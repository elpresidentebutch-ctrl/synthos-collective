/**
 * Synthos Validator Node — Deno Deploy
 *
 * Port of the Cloudflare Worker validator for Deno Deploy.
 * Uses Deno KV for persistent state (built into Deno Deploy, no config needed).
 * Push-from-proposer sync model — zero background gossip polling.
 *
 * Deploy:
 *   1. Push to GitHub
 *   2. Link repo to https://dash.deno.com
 *   3. Set entry point: deploy/deno/validator.ts
 *   4. Set env vars: WORKER_NAME, SELF_URL
 *
 * Or deploy via CLI:
 *   deno install -A jsr:@deno/deployctl
 *   deployctl deploy --project=synthos-validator deploy/deno/validator.ts
 */

// @ts-nocheck — Deno Deploy runtime; Deno.* APIs are not available locally

export {}; // make this a module so top-level await works

const FALLBACK_PEERS = [
  "https://synthos-validator-11.jamesishamwilliams.workers.dev",
  "https://synthos-validator-12.jamesishamwilliams.workers.dev",
  "https://synthos-validator-13.jamesishamwilliams.workers.dev",
  "https://synthos-validator-14.jamesishamwilliams.workers.dev",
  "https://synthos-validator-15.jamesishamwilliams.workers.dev",
];

const SELF_NAME = Deno.env.get("WORKER_NAME") || "deno-validator-1";
const SELF_URL = Deno.env.get("SELF_URL") || "";
const HEARTBEAT_MS = 6000;
const SKIP_TIMEOUT_MS = 12000; // 2× block time — skip proposer if no block arrives

// ─── State ──────────────────────────────────────────────────────────────────

let chain: any = null;
let initialized = false;
const kv = await Deno.openKv();

const GENESIS_ALLOC: Record<string, number> = {
  "agent-0": 100_000_000_000,
  "0x4ab2003c0391f25013f15539c9123e770d3c5a67": 10000,
};

function genesisChain() {
  const accounts: Record<string, { balance: number; nonce: number }> = {};
  for (const [addr, bal] of Object.entries(GENESIS_ALLOC)) {
    accounts[addr] = { balance: bal, nonce: 0 };
  }
  const stateRoot = computeStateRoot(accounts);
  const genesis = {
    header: { height: 0, parent_hash: "0x0", timestamp: "1970-01-01T00:00:00.000Z", proposer_id: "genesis", state_root: stateRoot },
    tx: [] as any[],
    hash: "",
    validator_votes: {},
    finalized: true,
  };
  return {
    chain_id: "synthos-l1-devnet",
    accounts,
    blocks: [genesis],
    mempool: {} as Record<string, any>,
    gossip: { synced_blocks: 0, tx_gossiped: 0, sync_errors: 0, last_sync: null as string | null },
    heartbeat: { checks: 0, last_check: null as string | null, blocks_auto_proposed: 0 },
  };
}

async function initialize() {
  if (initialized) return;
  try {
    const stored = await kv.get(["chain"]);
    if (stored.value && (stored.value as any).blocks && (stored.value as any).accounts) {
      chain = stored.value;
      if (!chain.gossip) chain.gossip = { synced_blocks: 0, tx_gossiped: 0, sync_errors: 0, last_sync: null };
      if (!chain.heartbeat) chain.heartbeat = { checks: 0, last_check: null, blocks_auto_proposed: 0 };
      if (!chain.mempool) chain.mempool = {};
    } else {
      chain = genesisChain();
      await persist();
    }
  } catch {
    chain = genesisChain();
    await persist();
  }
  initialized = true;
}

async function persist() {
  await kv.set(["chain"], chain);
}

function tip() {
  return chain.blocks[chain.blocks.length - 1];
}

// ─── Crypto ─────────────────────────────────────────────────────────────────

function computeStateRoot(accounts: Record<string, { balance: number; nonce: number }>) {
  const sorted = Object.keys(accounts).sort();
  const items = sorted.map((addr) => ({ addr, balance: accounts[addr].balance, nonce: accounts[addr].nonce }));
  const data = JSON.stringify(items);
  let hash = 0;
  for (let i = 0; i < data.length; i++) {
    const chr = data.charCodeAt(i);
    hash = ((hash << 5) - hash + chr) | 0;
  }
  return "0x" + Math.abs(hash).toString(16).padStart(16, "0");
}

async function computeBlockHash(block: any) {
  const payload = JSON.stringify({ header: block.header, tx: block.tx });
  const buf = new TextEncoder().encode(payload);
  const hashBuf = await crypto.subtle.digest("SHA-256", buf);
  const arr = new Uint8Array(hashBuf).slice(0, 16);
  return "0x" + Array.from(arr, (b) => b.toString(16).padStart(2, "0")).join("");
}

async function computeTxId(tx: any) {
  const payload = JSON.stringify({ from: tx.from, to: tx.to, amount: tx.amount, fee: tx.fee || 0, nonce: tx.nonce || 0 });
  const buf = new TextEncoder().encode(payload);
  const hashBuf = await crypto.subtle.digest("SHA-256", buf);
  const arr = new Uint8Array(hashBuf).slice(0, 16);
  return "0x" + Array.from(arr, (b) => b.toString(16).padStart(2, "0")).join("");
}

// ─── Consensus ──────────────────────────────────────────────────────────────

const peers = FALLBACK_PEERS.filter((url) => !url.includes(SELF_NAME));
const validatorOrder = FALLBACK_PEERS.map((u) => u.split("//")[1].split(".")[0]).sort();
// Add self to validator order if not already present
if (!validatorOrder.includes(SELF_NAME)) {
  validatorOrder.push(SELF_NAME);
  validatorOrder.sort();
}

function getDesignatedProposer(height: number) {
  return validatorOrder[height % validatorOrder.length];
}

function isMyTurn() {
  return getDesignatedProposer(tip().header.height + 1) === SELF_NAME;
}

// ─── Push/Pull Sync (no polling) ────────────────────────────────────────────

let lastBlockReceivedAt = Date.now();

// Push block to the NEXT proposer in rotation (1 fetch, not n)
async function pushToNextProposer(block: any) {
  const nextHeight = block.header.height + 1;
  const nextProposerName = getDesignatedProposer(nextHeight);
  const nextPeerUrl = peers.find((url) => url.includes(nextProposerName));
  if (!nextPeerUrl) return;

  try {
    await fetch(`${nextPeerUrl}/gossip/block`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ block }),
      signal: AbortSignal.timeout(5000),
    });
    console.log(`[PUSH] Block #${block.header.height} → ${nextProposerName}`);
  } catch (e: any) {
    console.log(`[PUSH] Failed to reach ${nextProposerName}: ${e.message}`);
  }
}

// Lazy catch-up: pull missing blocks from ONE peer (not all)
async function lazyCatchUp() {
  for (const peer of peers) {
    try {
      const statusResp = await fetch(`${peer}/status`, { signal: AbortSignal.timeout(5000) });
      if (!statusResp.ok) continue;
      const status = await statusResp.json();
      const myHeight = tip().header.height;
      if (status.height <= myHeight) continue;

      const blocksResp = await fetch(`${peer}/blocks?from=${myHeight + 1}`, { signal: AbortSignal.timeout(10000) });
      if (!blocksResp.ok) continue;
      const data = await blocksResp.json();

      let synced = 0;
      for (const block of data.blocks) {
        if (block.header.height <= tip().header.height) continue;
        if (block.header.parent_hash !== tip().hash) break;
        const tmpAccounts = JSON.parse(JSON.stringify(chain.accounts));
        let valid = true;
        for (const tx of block.tx) {
          const sender = tmpAccounts[tx.from];
          if (!sender) { valid = false; break; }
          const total = (tx.amount || 0) + (tx.fee || 0);
          if (sender.balance < total) { valid = false; break; }
          sender.balance -= total;
          sender.nonce += 1;
          if (!tmpAccounts[tx.to]) tmpAccounts[tx.to] = { balance: 0, nonce: 0 };
          tmpAccounts[tx.to].balance += tx.amount;
        }
        if (!valid) break;
        const expectedRoot = computeStateRoot(tmpAccounts);
        if (block.header.state_root !== expectedRoot) break;
        chain.accounts = tmpAccounts;
        chain.blocks.push(block);
        for (const tx of block.tx) delete chain.mempool[tx.id];
        synced++;
      }
      if (synced > 0) {
        lastBlockReceivedAt = Date.now();
        chain.gossip.synced_blocks += synced;
        chain.gossip.last_sync = new Date().toISOString();
        console.log(`[CATCHUP] Pulled ${synced} blocks from ${peer.split("//")[1].split(".")[0]}`);
        return;
      }
    } catch {
      continue;
    }
  }
}

// Forward a TX to the current designated proposer
async function forwardTxToProposer(tx: any) {
  const nextHeight = tip().header.height + 1;
  const proposerName = getDesignatedProposer(nextHeight);
  const proposerUrl = peers.find((url) => url.includes(proposerName));
  if (!proposerUrl) return;
  try {
    await fetch(`${proposerUrl}/gossip/tx-batch`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ transactions: [tx] }),
      signal: AbortSignal.timeout(5000),
    });
  } catch { /* non-critical */ }
}

async function autoPropose() {
  const mempoolKeys = Object.keys(chain.mempool);
  if (mempoolKeys.length === 0) return null;

  const tmpAccounts = JSON.parse(JSON.stringify(chain.accounts));
  const includedTxs: any[] = [];
  const candidates = mempoolKeys.map((k) => chain.mempool[k]);
  candidates.sort((a: any, b: any) => (b.fee || 0) - (a.fee || 0));

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

  const parent = tip();
  const stateRoot = computeStateRoot(tmpAccounts);
  const block = {
    header: { height: parent.header.height + 1, parent_hash: parent.hash, timestamp: "", proposer_id: SELF_NAME, state_root: stateRoot },
    tx: includedTxs, hash: "", validator_votes: {}, finalized: true,
  };
  block.hash = await computeBlockHash(block);
  chain.accounts = tmpAccounts;
  for (const tx of includedTxs) delete chain.mempool[tx.id];
  chain.blocks.push(block);
  await persist();
  return block;
}

// ─── Heartbeat (background loop) ───────────────────────────────────────────

// ─── Heartbeat (minimal — propose + skip timeout, no polling) ──────────────

async function heartbeat() {
  await initialize();
  const now = Date.now();
  chain.heartbeat.checks++;
  chain.heartbeat.last_check = new Date().toISOString();

  // Auto-propose if it's our turn and mempool has TXs
  if (Object.keys(chain.mempool).length > 0 && isMyTurn()) {
    const block = await autoPropose();
    if (block) {
      await pushToNextProposer(block);
      lastBlockReceivedAt = now;
    }
  }

  // Skip-proposer timeout: if no block in 2× block time, pull and try
  const timeSinceLastBlock = now - lastBlockReceivedAt;
  if (timeSinceLastBlock > SKIP_TIMEOUT_MS && !isMyTurn()) {
    console.log(`[SKIP] No block in ${timeSinceLastBlock}ms — attempting catch-up`);
    await lazyCatchUp();
    if (Object.keys(chain.mempool).length > 0 && isMyTurn()) {
      const block = await autoPropose();
      if (block) {
        await pushToNextProposer(block);
        lastBlockReceivedAt = now;
      }
    }
  }

  await persist();
}

// Start heartbeat loop
setInterval(heartbeat, HEARTBEAT_MS);

// ─── HTTP Handler ───────────────────────────────────────────────────────────

function json(data: any, status = 200) {
  return new Response(JSON.stringify(data, null, 2), {
    status,
    headers: { "Content-Type": "application/json", "Access-Control-Allow-Origin": "*" },
  });
}

function errorResp(msg: string, status = 400) {
  return new Response(JSON.stringify({ error: msg }), {
    status,
    headers: { "Content-Type": "application/json", "Access-Control-Allow-Origin": "*" },
  });
}

Deno.serve({ port: 8080 }, async (request: Request) => {
  await initialize();
  const url = new URL(request.url);
  const path = url.pathname;

  try {
    switch (path) {
      case "/health":
        return json({ ok: true, service: "synthos-validator", runtime: "deno", worker: false });

      case "/status": {
        const t = tip();
        return json({
          chain_id: chain.chain_id, height: t.header.height, tip: t.hash, state_root: t.header.state_root,
          mempool_size: Object.keys(chain.mempool).length, total_accounts: Object.keys(chain.accounts).length,
          validator: SELF_NAME, next_proposer: getDesignatedProposer(t.header.height + 1), is_my_turn: isMyTurn(),
          gossip_enabled: true,
        });
      }

      case "/account": {
        const addr = url.searchParams.get("address");
        if (!addr) return errorResp("missing address");
        const acct = chain.accounts[addr] || { balance: 0, nonce: 0 };
        return json({ address: addr, balance: acct.balance, nonce: acct.nonce });
      }

      case "/balance": {
        const addr = url.searchParams.get("address");
        if (!addr) return errorResp("missing address");
        const acct = chain.accounts[addr] || { balance: 0, nonce: 0 };
        return json({ address: addr, balance: acct.balance });
      }

      case "/mempool":
        return json({ size: Object.keys(chain.mempool).length, tx: chain.mempool });

      case "/blocks": {
        const from = parseInt(url.searchParams.get("from") || "0", 10);
        const blocks = chain.blocks.slice(from);
        return json({ blocks, count: blocks.length });
      }

      case "/submitTx": {
        if (request.method !== "POST") return errorResp("method not allowed", 405);
        const tx = await request.json();
        if (!tx.from || !tx.to || !tx.amount || tx.amount <= 0) return errorResp("invalid transaction");
        const sender = chain.accounts[tx.from];
        if (!sender) return errorResp(`unknown sender: ${tx.from}`);
        const total = (tx.amount || 0) + (tx.fee || 0);
        if (sender.balance < total) return errorResp(`insufficient funds: have ${sender.balance}, need ${total}`);
        if (tx.nonce !== undefined && tx.nonce !== sender.nonce) return errorResp(`bad nonce: expected ${sender.nonce}, got ${tx.nonce}`);
        if (!tx.id) tx.id = await computeTxId(tx);
        chain.mempool[tx.id] = tx;
        await persist();
        // Forward to current proposer if it's not us
        if (!isMyTurn()) await forwardTxToProposer(tx);
        return json({ ok: true, tx_id: tx.id, mempool_size: Object.keys(chain.mempool).length });
      }

      case "/gossip/tx-batch": {
        if (request.method !== "POST") return errorResp("method not allowed", 405);
        const { transactions } = await request.json();
        if (!Array.isArray(transactions)) return errorResp("invalid payload");
        let added = 0;
        for (const tx of transactions) {
          if (!tx.id || chain.mempool[tx.id]) continue;
          if (!tx.from || !tx.to || !tx.amount || tx.amount <= 0) continue;
          const sender = chain.accounts[tx.from];
          if (!sender) continue;
          if (tx.nonce !== undefined && tx.nonce < sender.nonce) continue;
          chain.mempool[tx.id] = tx;
          added++;
        }
        if (added > 0) await persist();
        return json({ ok: true, added, mempool_size: Object.keys(chain.mempool).length });
      }

      case "/gossip/block": {
        if (request.method !== "POST") return errorResp("method not allowed", 405);
        const { block } = await request.json();
        if (!block) return errorResp("missing block");
        if (block.header.height <= tip().header.height) return json({ ok: true, applied: false, my_height: tip().header.height });
        if (block.header.parent_hash !== tip().hash) return json({ ok: true, applied: false, reason: "parent mismatch" });
        const tmpAccounts = JSON.parse(JSON.stringify(chain.accounts));
        let valid = true;
        for (const tx of block.tx) {
          const s = tmpAccounts[tx.from];
          if (!s) { valid = false; break; }
          s.balance -= (tx.amount || 0) + (tx.fee || 0);
          s.nonce += 1;
          if (!tmpAccounts[tx.to]) tmpAccounts[tx.to] = { balance: 0, nonce: 0 };
          tmpAccounts[tx.to].balance += tx.amount;
        }
        if (valid && computeStateRoot(tmpAccounts) === block.header.state_root) {
          chain.accounts = tmpAccounts;
          chain.blocks.push(block);
          for (const tx of block.tx) delete chain.mempool[tx.id];
          await persist();
          lastBlockReceivedAt = Date.now();
          // Relay: push to the next proposer so the chain keeps moving
          await pushToNextProposer(block);
          return json({ ok: true, applied: true, height: block.header.height });
        }
        return json({ ok: true, applied: false, reason: "validation failed" });
      }

      case "/reset": {
        if (request.method !== "POST") return errorResp("method not allowed", 405);
        chain = genesisChain();
        await persist();
        return json({ ok: true, action: "reset", validator: SELF_NAME, height: 0 });
      }

      case "/peers":
        return json({ self: SELF_NAME, peers, total_validators: validatorOrder.length, validator_order: validatorOrder });

      default:
        return json({
          service: "synthos-validator", chain_id: chain.chain_id, validator: SELF_NAME, runtime: "deno",
          endpoints: ["/health", "/status", "/account", "/balance", "/mempool", "/blocks", "/submitTx", "/gossip/tx-batch", "/gossip/block", "/reset", "/peers"],
        });
    }
  } catch (e: any) {
    return errorResp(e.message, 500);
  }
});
