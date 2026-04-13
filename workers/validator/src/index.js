/**
 * SYNTHOS Validator Node — Cloudflare Worker
 *
 * L1 chain with push-from-proposer protocol (zero background gossip).
 * Uses Durable Objects for persistent state + round-robin PoA consensus.
 *
 * Sync Model (push/pull, no polling):
 *   - Proposer builds block → pushes to NEXT proposer only (1 fetch)
 *   - TXs are forwarded to the current proposer (not broadcast)
 *   - Validators pull missing blocks on-demand (lazy catch-up)
 *   - Skip-proposer timeout: if expected block doesn't arrive in 12s, next takes over
 *
 * Endpoints:
 *   GET  /health           - Health check
 *   GET  /status           - Chain status (height, tip, state root)
 *   GET  /account          - Account info
 *   GET  /balance          - Account balance
 *   GET  /mempool          - Pending transactions
 *   GET  /blocks?from=N    - Get blocks from height N
 *   GET  /heartbeat        - Heartbeat & gossip status
 *   GET  /peers            - Peer connectivity status
 *   POST /submitTx         - Submit a transaction (forwarded to proposer)
 *   POST /proposeBlock     - Manually propose a block
 *   POST /gossip/tx-batch  - Receive TXs from peer (internal)
 *   POST /gossip/block     - Receive block from peer (internal)
 *   POST /reset            - Reset chain to genesis
 */

// ─── Peer Network ────────────────────────────────────────────────────────────

// Hardcoded fallback peers (used when registry is unreachable)
const FALLBACK_PEERS = [
  "https://synthos-validator-11.jamesishamwilliams.workers.dev",
  "https://synthos-validator-12.jamesishamwilliams.workers.dev",
  "https://synthos-validator-13.jamesishamwilliams.workers.dev",
  "https://synthos-validator-14.jamesishamwilliams.workers.dev",
  "https://synthos-validator-15.jamesishamwilliams.workers.dev",
];

// Sorted validator names for deterministic round-robin proposer selection (fallback)
const FALLBACK_VALIDATOR_ORDER = FALLBACK_PEERS.map((url) => url.split("//")[1].split(".")[0]).sort();

// ─── Worker Entry Point ─────────────────────────────────────────────────────

export default {
  async fetch(request, env) {
    const url = new URL(request.url);

    if (url.pathname === "/health") {
      return json({ ok: true, service: "synthos-validator", worker: true });
    }

    // Serve registry URL for diagnostics
    if (url.pathname === "/registry") {
      return json({ registry_url: env.REGISTRY_URL || "none" });
    }

    const id = env.CHAIN.idFromName("synthos-l1-devnet");
    const stub = env.CHAIN.get(id);
    return stub.fetch(request);
  },

  async scheduled(event, env, ctx) {
    const id = env.CHAIN.idFromName("synthos-l1-devnet");
    const stub = env.CHAIN.get(id);
    const resp = await stub.fetch(new Request("https://internal/cron-propose"));
    const result = await resp.json();
    console.log(`[CRON] ${JSON.stringify(result)}`);
  },
};

// ─── Durable Object: ChainDO ────────────────────────────────────────────────

export class ChainDO {
  constructor(state, env) {
    this.state = state;
    this.env = env;
    this.chain = null;
    this.initialized = false;
    this.HEARTBEAT_MS = 6000; // 6s block time
    this.SKIP_TIMEOUT_MS = 12000; // 2× block time — skip proposer if no block arrives
    this.REGISTRY_REFRESH_MS = 60000; // Refresh peer list from registry every 60s
    // Self-identity: set via --var WORKER_NAME:synthos-validator-XX at deploy
    this.selfName = env.WORKER_NAME || "unknown";
    this.selfUrl = env.SELF_URL || "";
    this.registryUrl = env.REGISTRY_URL || "";
    this.registrySecret = env.REGISTRY_SECRET || "";
    // Dynamic peer list — starts with fallback, gets replaced by registry data
    this.peers = FALLBACK_PEERS.filter((url) => !url.includes(this.selfName));
    this.validatorOrder = [...FALLBACK_VALIDATOR_ORDER];
    this.lastRegistryRefresh = 0;
    // Track when we last received a block (for skip-proposer timeout)
    this.lastBlockReceivedAt = Date.now();
  }

  // ─── Initialization ─────────────────────────────────────────────────────

  async initialize() {
    if (this.initialized) return;

    try {
      const stored = await this.state.storage.get("chain");
      if (stored && stored.blocks && stored.accounts) {
        this.chain = stored;
        // Ensure gossip stats exist (upgrade from older version)
        if (!this.chain.gossip) {
          this.chain.gossip = { synced_blocks: 0, tx_gossiped: 0, sync_errors: 0, last_sync: null };
        }
        if (!this.chain.heartbeat) {
          this.chain.heartbeat = { checks: 0, last_check: null, blocks_auto_proposed: 0 };
        }
      } else {
        console.log("[INIT] No valid stored state, resetting to genesis");
        await this.resetToGenesis();
      }
    } catch (e) {
      console.log(`[INIT] Storage read failed (${e.message}), resetting to genesis`);
      await this.resetToGenesis();
    }
    this.initialized = true;
    await this.scheduleHeartbeat();
  }

  async resetToGenesis() {
    const alloc = {
      "agent-0": 100_000_000_000,
      "0x4ab2003c0391f25013f15539c9123e770d3c5a67": 10000,
    };
    const accounts = {};
    for (const [addr, bal] of Object.entries(alloc)) {
      accounts[addr] = { balance: bal, nonce: 0 };
    }

    const stateRoot = computeStateRoot(accounts);
    const genesisBlock = {
      header: {
        height: 0,
        parent_hash: "0x0",
        timestamp: "1970-01-01T00:00:00.000Z",
        proposer_id: "genesis",
        state_root: stateRoot,
      },
      tx: [],
      hash: "",
      validator_votes: {},
      finalized: true,
    };
    genesisBlock.hash = await computeBlockHash(genesisBlock);

    this.chain = {
      chain_id: "synthos-l1-devnet",
      accounts,
      blocks: [genesisBlock],
      mempool: {},
      heartbeat: { checks: 0, last_check: null, blocks_auto_proposed: 0 },
      gossip: { synced_blocks: 0, tx_gossiped: 0, sync_errors: 0, last_sync: null },
    };
    await this.persist();
  }

  async scheduleHeartbeat() {
    const current = await this.state.storage.getAlarm();
    if (!current) {
      await this.state.storage.setAlarm(Date.now() + this.HEARTBEAT_MS);
    }
  }

  // ─── Consensus: Round-Robin Proof-of-Authority ──────────────────────────

  getDesignatedProposer(height) {
    return this.validatorOrder[height % this.validatorOrder.length];
  }

  isMyTurn() {
    const nextHeight = this.tip().header.height + 1;
    return this.getDesignatedProposer(nextHeight) === this.selfName;
  }

  // ─── Heartbeat (minimal — no polling, no broadcast) ──────────────────────

  async alarm() {
    await this.initialize();

    const now = Date.now();
    this.chain.heartbeat.checks++;
    this.chain.heartbeat.last_check = new Date().toISOString();

    // Periodically refresh peer list from registry + register self
    await this.refreshFromRegistry();

    // Auto-propose if it's our turn and mempool has TXs
    const mempoolSize = Object.keys(this.chain.mempool).length;
    if (mempoolSize > 0 && this.isMyTurn()) {
      console.log(`[PROPOSE] ${this.selfName} proposing block at height ${this.tip().header.height + 1} (${mempoolSize} TXs)`);
      const block = await this.autoPropose();
      if (block) {
        // Push block to NEXT proposer only (not all peers)
        await this.pushToNextProposer(block);
        this.lastBlockReceivedAt = now;
      }
    }

    // Skip-proposer timeout: if we haven't received a block in 2× block time
    // and we're NOT the current proposer, the expected proposer may be down.
    // Pull from any peer to catch up, then try proposing if it's now our turn.
    const timeSinceLastBlock = now - this.lastBlockReceivedAt;
    if (timeSinceLastBlock > this.SKIP_TIMEOUT_MS && !this.isMyTurn()) {
      console.log(`[SKIP] No block in ${timeSinceLastBlock}ms — attempting catch-up`);
      await this.lazyCatchUp();
      // After catch-up, check if it's now our turn (proposer may have been skipped)
      if (Object.keys(this.chain.mempool).length > 0 && this.isMyTurn()) {
        const block = await this.autoPropose();
        if (block) {
          await this.pushToNextProposer(block);
          this.lastBlockReceivedAt = now;
        }
      }
    }

    await this.persist();
    await this.state.storage.setAlarm(Date.now() + this.HEARTBEAT_MS);
  }

  // ─── Dynamic Peer Registry ───────────────────────────────────────────────

  async refreshFromRegistry() {
    if (!this.registryUrl) return;

    const now = Date.now();
    if (now - this.lastRegistryRefresh < this.REGISTRY_REFRESH_MS) return;
    this.lastRegistryRefresh = now;

    try {
      // Register/heartbeat ourselves
      if (this.selfUrl) {
        await fetch(`${this.registryUrl}/register`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            ...(this.registrySecret ? { "X-Registry-Secret": this.registrySecret } : {}),
          },
          body: JSON.stringify({
            name: this.selfName,
            url: this.selfUrl,
            cloud: "cloudflare",
          }),
          signal: AbortSignal.timeout(5000),
        });
      }

      // Fetch active peers
      const resp = await fetch(`${this.registryUrl}/peers/active`, {
        signal: AbortSignal.timeout(5000),
      });
      if (!resp.ok) return;

      const data = await resp.json();
      if (data.peers && data.peers.length > 0) {
        // Update peers (exclude self)
        this.peers = data.urls.filter((url) => !url.includes(this.selfName));
        this.validatorOrder = data.validator_order;
        console.log(`[REGISTRY] Refreshed: ${data.peers.length} peers, order: ${this.validatorOrder.join(", ")}`);
      }
    } catch (e) {
      console.log(`[REGISTRY] Refresh failed (using cached peers): ${e.message}`);
    }
  }

  // Push block to the NEXT proposer in rotation (1 fetch, not n)
  async pushToNextProposer(block) {
    const nextHeight = block.header.height + 1;
    const nextProposerName = this.getDesignatedProposer(nextHeight);
    const nextPeerUrl = this.peers.find((url) => url.includes(nextProposerName));
    if (!nextPeerUrl) return;

    try {
      await fetch(`${nextPeerUrl}/gossip/block`, {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Gossip": "true" },
        body: JSON.stringify({ block }),
        signal: AbortSignal.timeout(5000),
      });
      console.log(`[PUSH] Block #${block.header.height} → ${nextProposerName}`);
    } catch (e) {
      console.log(`[PUSH] Failed to reach ${nextProposerName}: ${e.message}`);
    }
  }

  // Lazy catch-up: pull missing blocks from ONE peer (not all)
  async lazyCatchUp() {
    for (const peer of this.peers) {
      try {
        const statusResp = await fetch(`${peer}/status`, {
          headers: { "X-Gossip": "true" },
          signal: AbortSignal.timeout(5000),
        });
        if (!statusResp.ok) continue;
        const status = await statusResp.json();

        const myHeight = this.tip().header.height;
        if (status.height <= myHeight) continue;

        const blocksResp = await fetch(`${peer}/blocks?from=${myHeight + 1}`, {
          headers: { "X-Gossip": "true" },
          signal: AbortSignal.timeout(10000),
        });
        if (!blocksResp.ok) continue;
        const { blocks } = await blocksResp.json();

        let synced = 0;
        for (const block of blocks) {
          const applied = await this.applyPeerBlock(block);
          if (applied) {
            synced++;
            this.chain.gossip.synced_blocks++;
          } else {
            break;
          }
        }
        if (synced > 0) {
          this.lastBlockReceivedAt = Date.now();
          this.chain.gossip.last_sync = new Date().toISOString();
          console.log(`[CATCHUP] Pulled ${synced} blocks from ${peer.split("//")[1].split(".")[0]}`);
          return; // Done — caught up from one peer
        }
      } catch (_) {
        continue;
      }
    }
  }

  // Forward a TX to the current designated proposer
  async forwardTxToProposer(tx) {
    const nextHeight = this.tip().header.height + 1;
    const proposerName = this.getDesignatedProposer(nextHeight);
    const proposerUrl = this.peers.find((url) => url.includes(proposerName));
    if (!proposerUrl) return false;

    try {
      const resp = await fetch(`${proposerUrl}/gossip/tx-batch`, {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Gossip": "true" },
        body: JSON.stringify({ transactions: [tx] }),
        signal: AbortSignal.timeout(5000),
      });
      return resp.ok;
    } catch (_) {
      return false;
    }
  }

  // Validate and apply a block received from a peer
  async applyPeerBlock(block) {
    const myTip = this.tip();

    // Must extend our chain exactly
    if (block.header.height !== myTip.header.height + 1) return false;
    if (block.header.parent_hash !== myTip.hash) return false;

    // Replay transactions against our account state to verify
    const tmpAccounts = JSON.parse(JSON.stringify(this.chain.accounts));
    for (const tx of block.tx) {
      const sender = tmpAccounts[tx.from];
      if (!sender) return false;
      if (tx.nonce !== undefined && tx.nonce !== sender.nonce) return false;
      const total = (tx.amount || 0) + (tx.fee || 0);
      if (sender.balance < total) return false;
      sender.balance -= total;
      sender.nonce += 1;
      if (!tmpAccounts[tx.to]) tmpAccounts[tx.to] = { balance: 0, nonce: 0 };
      tmpAccounts[tx.to].balance += tx.amount;
    }

    // Verify state root matches what the proposer computed
    const expectedRoot = computeStateRoot(tmpAccounts);
    if (expectedRoot !== block.header.state_root) return false;

    // Timeless runtime (B3): reject non-genesis blocks with timestamps
    if (block.header.height > 0 && block.header.timestamp && block.header.timestamp !== "") return false;

    // Verify block hash integrity
    const expectedHash = await computeBlockHash(block);
    if (expectedHash !== block.hash) return false;

    // Accept: apply state, clear included TXs from mempool, append block
    this.chain.accounts = tmpAccounts;
    for (const tx of block.tx) {
      delete this.chain.mempool[tx.id];
    }
    this.chain.blocks.push(block);

    return true;
  }

  // Legacy broadcastBlock removed — replaced by pushToNextProposer (1 fetch vs n)

  // ─── Block Building ─────────────────────────────────────────────────────

  async autoPropose() {
    const tmpAccounts = JSON.parse(JSON.stringify(this.chain.accounts));
    const includedTxs = [];

    const candidates = Object.values(this.chain.mempool);
    candidates.sort((a, b) => {
      if ((b.fee || 0) !== (a.fee || 0)) return (b.fee || 0) - (a.fee || 0);
      if (a.from !== b.from) return a.from < b.from ? -1 : 1;
      return (a.nonce || 0) - (b.nonce || 0);
    });

    for (const tx of candidates) {
      if (includedTxs.length >= 1000) break;
      const sender = tmpAccounts[tx.from];
      if (!sender) continue;
      if (tx.nonce !== undefined && tx.nonce !== sender.nonce) continue;
      const total = (tx.amount || 0) + (tx.fee || 0);
      if (sender.balance < total) continue;
      sender.balance -= total;
      sender.nonce += 1;
      if (!tmpAccounts[tx.to]) tmpAccounts[tx.to] = { balance: 0, nonce: 0 };
      tmpAccounts[tx.to].balance += tx.amount;
      includedTxs.push(tx);
    }

    if (includedTxs.length === 0) return null;

    const parent = this.tip();
    const stateRoot = computeStateRoot(tmpAccounts);
    // Timeless runtime (B3): non-genesis blocks carry no wall-clock timestamp.
    const block = {
      header: {
        height: parent.header.height + 1,
        parent_hash: parent.hash,
        timestamp: "",
        proposer_id: this.selfName,
        state_root: stateRoot,
      },
      tx: includedTxs,
      hash: "",
      validator_votes: {},
      finalized: true,
    };
    block.hash = await computeBlockHash(block);

    this.chain.accounts = tmpAccounts;
    for (const tx of includedTxs) delete this.chain.mempool[tx.id];
    this.chain.blocks.push(block);
    this.chain.heartbeat.blocks_auto_proposed++;

    console.log(`[BLOCK] #${block.header.height} by ${this.selfName} with ${includedTxs.length} TXs — hash ${block.hash}`);
    return block;
  }

  // ─── Persistence ────────────────────────────────────────────────────────

  async persist() {
    await this.state.storage.put("chain", this.chain);
  }

  // ─── Request Router ─────────────────────────────────────────────────────

  async fetch(request) {
    try {
      await this.initialize();
    } catch (e) {
      return error(`initialization failed: ${e.message}`, 500);
    }

    const url = new URL(request.url);
    const path = url.pathname;

    try {
      switch (path) {
        case "/status":
          return this.handleStatus();
        case "/account":
          return this.handleAccount(url);
        case "/balance":
          return this.handleBalance(url);
        case "/mempool":
          return this.handleMempool();
        case "/blocks":
          return this.handleGetBlocks(url);
        case "/heartbeat":
          return this.handleHeartbeatStatus();
        case "/peers":
          return this.handlePeers();
        case "/submitTx":
          if (request.method !== "POST") return error("method not allowed", 405);
          return await this.handleSubmitTx(request);
        case "/proposeBlock":
          if (request.method !== "POST") return error("method not allowed", 405);
          return await this.handleProposeBlock();
        case "/cron-propose":
          return await this.handleCronPropose();
        case "/gossip/tx-batch":
          if (request.method !== "POST") return error("method not allowed", 405);
          return await this.handleGossipTxBatch(request);
        case "/gossip/block":
          if (request.method !== "POST") return error("method not allowed", 405);
          return await this.handleGossipBlock(request);
        case "/reset":
          if (request.method !== "POST") return error("method not allowed", 405);
          return await this.handleReset();
        case "/health":
          return json({ ok: true, service: "synthos-validator", worker: true });
        default:
          return json({
            service: "synthos-validator",
            chain_id: this.chain.chain_id,
            height: this.tip().header.height,
            validator: this.selfName,
            gossip: true,
            endpoints: [
              "/health", "/status", "/account", "/balance", "/mempool",
              "/blocks", "/heartbeat", "/peers", "/submitTx", "/proposeBlock",
              "/gossip/tx-batch", "/gossip/block", "/reset",
            ],
          });
      }
    } catch (e) {
      return error(e.message, 500);
    }
  }

  // ─── Handlers ───────────────────────────────────────────────────────────

  tip() {
    return this.chain.blocks[this.chain.blocks.length - 1];
  }

  handleStatus() {
    const tip = this.tip();
    const nextHeight = tip.header.height + 1;
    return json({
      chain_id: this.chain.chain_id,
      height: tip.header.height,
      tip: tip.hash,
      state_root: tip.header.state_root,
      mempool_size: Object.keys(this.chain.mempool).length,
      total_accounts: Object.keys(this.chain.accounts).length,
      validator: this.selfName,
      next_proposer: this.getDesignatedProposer(nextHeight),
      is_my_turn: this.isMyTurn(),
      gossip_enabled: false,
      sync_model: "push-pull",
    });
  }

  handleAccount(url) {
    const addr = url.searchParams.get("address");
    if (!addr) return error("missing address", 400);
    const acct = this.chain.accounts[addr] || { balance: 0, nonce: 0 };
    return json({ address: addr, balance: acct.balance, nonce: acct.nonce });
  }

  handleBalance(url) {
    const addr = url.searchParams.get("address");
    if (!addr) return error("missing address", 400);
    const acct = this.chain.accounts[addr] || { balance: 0, nonce: 0 };
    return json({ address: addr, balance: acct.balance });
  }

  handleMempool() {
    return json({
      size: Object.keys(this.chain.mempool).length,
      tx: this.chain.mempool,
    });
  }

  handleGetBlocks(url) {
    const from = parseInt(url.searchParams.get("from") || "0", 10);
    const blocks = this.chain.blocks.slice(from);
    return json({ blocks, count: blocks.length });
  }

  handleHeartbeatStatus() {
    const hb = this.chain.heartbeat;
    const gs = this.chain.gossip;
    return json({
      heartbeat_interval_ms: this.HEARTBEAT_MS,
      total_checks: hb.checks,
      last_check: hb.last_check,
      blocks_auto_proposed: hb.blocks_auto_proposed,
      mempool_size: Object.keys(this.chain.mempool).length,
      gossip: {
        synced_blocks: gs.synced_blocks,
        tx_gossiped: gs.tx_gossiped,
        sync_errors: gs.sync_errors,
        last_sync: gs.last_sync,
      },
      validator: this.selfName,
      is_my_turn: this.isMyTurn(),
    });
  }

  handlePeers() {
    return json({
      self: this.selfName,
      peers: this.peers,
      total_validators: this.validatorOrder.length,
      validator_order: this.validatorOrder,
      registry_url: this.registryUrl || "none",
      current_proposer: this.getDesignatedProposer(this.tip().header.height + 1),
    });
  }

  async handleCronPropose() {
    const mempoolSize = Object.keys(this.chain.mempool).length;
    if (mempoolSize === 0) {
      return json({ ok: true, action: "no-op", reason: "mempool empty" });
    }
    if (!this.isMyTurn()) {
      return json({ ok: true, action: "no-op", reason: "not my turn", next_proposer: this.getDesignatedProposer(this.tip().header.height + 1) });
    }
    const block = await this.autoPropose();
    if (block) await this.pushToNextProposer(block);
    await this.persist();
    const tip = this.tip();
    return json({ ok: true, action: "block-proposed", height: tip.header.height, hash: tip.hash });
  }

  async handleSubmitTx(request) {
    const tx = await request.json();

    if (!tx.from || !tx.to || !tx.amount || tx.amount <= 0) {
      return error("invalid transaction: from, to, and amount > 0 required", 400);
    }

    const sender = this.chain.accounts[tx.from];
    if (!sender) {
      return error(`unknown sender: ${tx.from}`, 400);
    }

    const total = (tx.amount || 0) + (tx.fee || 0);
    if (sender.balance < total) {
      return error(`insufficient funds: have ${sender.balance}, need ${total}`, 400);
    }

    if (tx.nonce !== undefined && tx.nonce !== sender.nonce) {
      return error(`bad nonce: expected ${sender.nonce}, got ${tx.nonce}`, 400);
    }

    if (!tx.id) {
      tx.id = await computeTxId(tx);
    }

    // Always store locally
    this.chain.mempool[tx.id] = tx;
    await this.persist();

    // If we're not the current proposer, forward to them
    if (!this.isMyTurn()) {
      await this.forwardTxToProposer(tx);
    }

    return json({ ok: true, tx_id: tx.id, mempool_size: Object.keys(this.chain.mempool).length });
  }

  async handleProposeBlock() {
    // Enforce round-robin PoA: only the designated proposer can build a block
    if (!this.isMyTurn()) {
      const next = this.getDesignatedProposer(this.tip().header.height + 1);
      return error(`not this validator's turn to propose — current proposer is ${next}`, 403);
    }

    const mempoolKeys = Object.keys(this.chain.mempool);
    if (mempoolKeys.length === 0) {
      return error("mempool empty, nothing to propose", 400);
    }

    const tmpAccounts = JSON.parse(JSON.stringify(this.chain.accounts));
    const includedTxs = [];

    const candidates = mempoolKeys.map((k) => this.chain.mempool[k]);
    candidates.sort((a, b) => {
      if ((b.fee || 0) !== (a.fee || 0)) return (b.fee || 0) - (a.fee || 0);
      if (a.from !== b.from) return a.from < b.from ? -1 : 1;
      return (a.nonce || 0) - (b.nonce || 0);
    });

    for (const tx of candidates) {
      if (includedTxs.length >= 1000) break;
      const sender = tmpAccounts[tx.from];
      if (!sender) continue;
      if (tx.nonce !== undefined && tx.nonce !== sender.nonce) continue;
      const total = (tx.amount || 0) + (tx.fee || 0);
      if (sender.balance < total) continue;
      sender.balance -= total;
      sender.nonce += 1;
      if (!tmpAccounts[tx.to]) tmpAccounts[tx.to] = { balance: 0, nonce: 0 };
      tmpAccounts[tx.to].balance += tx.amount;
      includedTxs.push(tx);
    }

    if (includedTxs.length === 0) {
      return error("no valid transactions to include", 400);
    }

    const parent = this.tip();
    const stateRoot = computeStateRoot(tmpAccounts);

    // Timeless runtime (B3): non-genesis blocks carry no wall-clock timestamp.
    const block = {
      header: {
        height: parent.header.height + 1,
        parent_hash: parent.hash,
        timestamp: "",
        proposer_id: this.selfName,
        state_root: stateRoot,
      },
      tx: includedTxs,
      hash: "",
      validator_votes: {},
      finalized: true,
    };
    block.hash = await computeBlockHash(block);

    this.chain.accounts = tmpAccounts;
    for (const tx of includedTxs) {
      delete this.chain.mempool[tx.id];
    }
    this.chain.blocks.push(block);
    await this.persist();

    // Push to next proposer only (not broadcast)
    await this.pushToNextProposer(block);

    return json({
      ok: true,
      block_hash: block.hash,
      height: block.header.height,
      tx_count: includedTxs.length,
      state_root: stateRoot,
      proposer: this.selfName,
    });
  }

  // ─── Gossip Handlers (called by peers) ──────────────────────────────────

  async handleGossipTxBatch(request) {
    const { transactions } = await request.json();
    if (!Array.isArray(transactions)) return error("invalid payload", 400);

    let added = 0;
    for (const tx of transactions) {
      if (!tx.id || this.chain.mempool[tx.id]) continue;
      if (!tx.from || !tx.to || !tx.amount || tx.amount <= 0) continue;
      const sender = this.chain.accounts[tx.from];
      if (!sender) continue;
      // Accept TXs with nonce >= current (they might become valid after earlier TXs)
      if (tx.nonce !== undefined && tx.nonce < sender.nonce) continue;
      this.chain.mempool[tx.id] = tx;
      added++;
    }
    if (added > 0) await this.persist();
    return json({ ok: true, added, mempool_size: Object.keys(this.chain.mempool).length });
  }

  async handleGossipBlock(request) {
    const { block } = await request.json();
    if (!block) return error("missing block", 400);

    const applied = await this.applyPeerBlock(block);
    if (applied) {
      this.lastBlockReceivedAt = Date.now();
      await this.persist();
      console.log(`[GOSSIP] Accepted block #${block.header.height} from ${block.header.proposer_id}`);

      // Relay: push to the next proposer so the chain keeps moving
      await this.pushToNextProposer(block);

      return json({ ok: true, applied: true, height: block.header.height });
    }
    return json({ ok: true, applied: false, my_height: this.tip().header.height });
  }

  async handleReset() {
    await this.resetToGenesis();
    this.initialized = true;
    return json({
      ok: true,
      action: "reset",
      validator: this.selfName,
      height: 0,
      state_root: this.tip().header.state_root,
    });
  }
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function json(data, status = 200) {
  return new Response(JSON.stringify(data, null, 2), {
    status,
    headers: { "Content-Type": "application/json", "Access-Control-Allow-Origin": "*" },
  });
}

function error(msg, status = 400) {
  return new Response(JSON.stringify({ error: msg }, null, 2), {
    status,
    headers: { "Content-Type": "application/json", "Access-Control-Allow-Origin": "*" },
  });
}

function computeStateRoot(accounts) {
  const sorted = Object.keys(accounts).sort();
  const items = sorted.map((addr) => ({
    addr,
    balance: accounts[addr].balance,
    nonce: accounts[addr].nonce,
  }));
  const data = JSON.stringify(items);
  let hash = 0;
  for (let i = 0; i < data.length; i++) {
    const chr = data.charCodeAt(i);
    hash = ((hash << 5) - hash + chr) | 0;
  }
  return "0x" + Math.abs(hash).toString(16).padStart(16, "0");
}

async function computeBlockHash(block) {
  const payload = JSON.stringify({ header: block.header, tx: block.tx });
  const buf = new TextEncoder().encode(payload);
  const hashBuf = await crypto.subtle.digest("SHA-256", buf);
  const arr = new Uint8Array(hashBuf).slice(0, 16);
  return "0x" + Array.from(arr, (b) => b.toString(16).padStart(2, "0")).join("");
}

async function computeTxId(tx) {
  const payload = JSON.stringify({
    from: tx.from,
    to: tx.to,
    amount: tx.amount,
    fee: tx.fee || 0,
    nonce: tx.nonce || 0,
  });
  const buf = new TextEncoder().encode(payload);
  const hashBuf = await crypto.subtle.digest("SHA-256", buf);
  const arr = new Uint8Array(hashBuf).slice(0, 16);
  return "0x" + Array.from(arr, (b) => b.toString(16).padStart(2, "0")).join("");
}
