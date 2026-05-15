/**
 * SYNTHOS Block Relay — Cloudflare Worker
 *
 * Validators push every finalized block here. Dashboards poll for real-time
 * chain data without hitting individual validator nodes directly.
 *
 * Endpoints:
 *   GET  /health          — Health check
 *   GET  /latest          — Latest N blocks (default: 50)
 *   GET  /stats           — Aggregate network stats
 *   GET  /blocks/:height  — Single block by height
 *   POST /push-block      — Validators push finalized blocks here
 *   POST /reset           — Clear all stored blocks (dev use)
 *
 * Security:
 *   POST /push-block requires header:  X-Relay-Secret: <RELAY_SECRET>
 *   All GET routes are public (CORS *).
 */

export default {
  async fetch(request, env) {
    const url = new URL(request.url);

    // Fast health check — no DO needed
    if (url.pathname === "/health") {
      return json({ ok: true, service: "synthos-block-relay" });
    }

    const id = env.RELAY.idFromName("synthos-relay-v1");
    const stub = env.RELAY.get(id);
    return stub.fetch(request);
  },
};

// ─── Durable Object ──────────────────────────────────────────────────────────

export class BlockRelayDO {
  constructor(state, env) {
    this.state = state;
    this.env = env;
    this.relaySecret = env.RELAY_SECRET || "";
    /** @type {{ height: number, hash: string, proposer: string, tx_count: number, state_root: string, received_at: string }[]} */
    this.blocks = null; // lazy-loaded
    this.stats = null;  // lazy-loaded
    this.MAX_BLOCKS = 200; // rolling window kept in DO storage
  }

  async ensureLoaded() {
    if (this.blocks !== null) return;
    this.blocks = (await this.state.storage.get("blocks")) || [];
    this.stats = (await this.state.storage.get("stats")) || {
      total_blocks_received: 0,
      highest_height: 0,
      known_proposers: [],
      last_push_at: null,
    };
  }

  async persist() {
    await this.state.storage.put("blocks", this.blocks);
    await this.state.storage.put("stats", this.stats);
  }

  async fetch(request) {
    const url = new URL(request.url);
    const path = url.pathname;

    await this.ensureLoaded();

    // CORS pre-flight
    if (request.method === "OPTIONS") {
      return new Response(null, { status: 204, headers: cors() });
    }

    try {
      if (path === "/latest") {
        return this.handleLatest(url);
      }
      if (path === "/stats") {
        return this.handleStats();
      }
      if (path.startsWith("/blocks/")) {
        const height = parseInt(path.replace("/blocks/", ""), 10);
        return this.handleBlockByHeight(height);
      }
      if (path === "/push-block" && request.method === "POST") {
        return this.handlePushBlock(request);
      }
      if (path === "/reset" && request.method === "POST") {
        return this.handleReset(request);
      }
      if (path === "/health") {
        return json({ ok: true, service: "synthos-block-relay", blocks: this.blocks.length });
      }

      return json({
        service: "synthos-block-relay",
        endpoints: ["/health", "/latest", "/stats", "/blocks/:height", "/push-block (POST)", "/reset (POST)"],
        total_blocks: this.blocks.length,
      });
    } catch (e) {
      return errorResp(`internal error: ${e.message}`, 500);
    }
  }

  // ─── GET /latest?limit=N ───────────────────────────────────────────────

  handleLatest(url) {
    const limit = Math.min(parseInt(url.searchParams.get("limit") || "50", 10), 200);
    const blocks = this.blocks.slice(-limit).reverse(); // newest first
    return json({
      count: blocks.length,
      highest_height: this.stats.highest_height,
      blocks,
    });
  }

  // ─── GET /stats ────────────────────────────────────────────────────────

  handleStats() {
    // Compute rolling TPS over the last 60 seconds of blocks
    const now = Date.now();
    const window = 60_000;
    const recent = this.blocks.filter(
      (b) => now - new Date(b.received_at).getTime() < window
    );
    const total_tx = recent.reduce((s, b) => s + (b.tx_count || 0), 0);
    const tps = recent.length > 1 ? (total_tx / 60).toFixed(2) : "0.00";

    return json({
      highest_height: this.stats.highest_height,
      total_blocks_received: this.stats.total_blocks_received,
      known_proposers: this.stats.known_proposers,
      last_push_at: this.stats.last_push_at,
      rolling_tps: parseFloat(tps),
      blocks_last_60s: recent.length,
    });
  }

  // ─── GET /blocks/:height ───────────────────────────────────────────────

  handleBlockByHeight(height) {
    if (isNaN(height)) return errorResp("invalid height", 400);
    const block = this.blocks.find((b) => b.height === height);
    if (!block) return errorResp(`block at height ${height} not found`, 404);
    return json(block);
  }

  // ─── POST /push-block ──────────────────────────────────────────────────

  async handlePushBlock(request) {
    // Auth: optional shared secret
    if (this.relaySecret) {
      const provided = request.headers.get("X-Relay-Secret") || "";
      if (provided !== this.relaySecret) {
        return errorResp("unauthorized", 401);
      }
    }

    let body;
    try {
      body = await request.json();
    } catch {
      return errorResp("invalid JSON", 400);
    }

    const { block, validator } = body;
    if (!block || block.header === undefined) {
      return errorResp("missing block or block.header", 400);
    }

    const height = block.header.height;
    const hash = block.hash || "";
    const proposer = block.header.proposer_id || validator || "unknown";
    const tx_count = Array.isArray(block.tx) ? block.tx.length : 0;
    const state_root = block.header.state_root || "";

    // Dedup: skip if we already have this block
    const exists = this.blocks.some((b) => b.hash === hash && b.height === height);
    if (exists) {
      return json({ ok: true, action: "duplicate", height });
    }

    const record = {
      height,
      hash,
      proposer,
      tx_count,
      state_root,
      received_at: new Date().toISOString(),
    };

    this.blocks.push(record);

    // Keep rolling window
    if (this.blocks.length > this.MAX_BLOCKS) {
      this.blocks = this.blocks.slice(-this.MAX_BLOCKS);
    }

    // Update stats
    this.stats.total_blocks_received++;
    if (height > this.stats.highest_height) {
      this.stats.highest_height = height;
    }
    if (!this.stats.known_proposers.includes(proposer)) {
      this.stats.known_proposers.push(proposer);
    }
    this.stats.last_push_at = new Date().toISOString();

    await this.persist();

    console.log(`[RELAY] Block #${height} from ${proposer} | hash=${hash} | txs=${tx_count}`);

    return json({ ok: true, action: "accepted", height, hash });
  }

  // ─── POST /reset ───────────────────────────────────────────────────────

  async handleReset(request) {
    if (this.relaySecret) {
      const provided = request.headers.get("X-Relay-Secret") || "";
      if (provided !== this.relaySecret) return errorResp("unauthorized", 401);
    }
    this.blocks = [];
    this.stats = {
      total_blocks_received: 0,
      highest_height: 0,
      known_proposers: [],
      last_push_at: null,
    };
    await this.persist();
    return json({ ok: true, action: "reset" });
  }
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function cors() {
  return {
    "Access-Control-Allow-Origin": "*",
    "Access-Control-Allow-Methods": "GET, POST, OPTIONS",
    "Access-Control-Allow-Headers": "Content-Type, X-Relay-Secret",
  };
}

function json(data, status = 200) {
  return new Response(JSON.stringify(data, null, 2), {
    status,
    headers: { "Content-Type": "application/json", ...cors() },
  });
}

function errorResp(msg, status = 400) {
  return new Response(JSON.stringify({ error: msg }, null, 2), {
    status,
    headers: { "Content-Type": "application/json", ...cors() },
  });
}
