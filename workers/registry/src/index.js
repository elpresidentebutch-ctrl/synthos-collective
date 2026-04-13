/**
 * Synthos Peer Registry — Cloudflare Worker
 *
 * Dynamic peer discovery service for decentralized Synthos validators.
 * Validators register themselves here; all validators poll this to
 * discover peers. Works with validators on ANY cloud — not just Cloudflare.
 *
 * Endpoints:
 *   GET  /health              - Health check
 *   GET  /peers               - List all registered validators
 *   GET  /peers/active        - List only active validators (seen in last 5 min)
 *   POST /register            - Register or heartbeat a validator
 *   POST /deregister          - Remove a validator
 *   GET  /stats               - Registry statistics
 */

const ACTIVE_THRESHOLD_MS = 5 * 60 * 1000; // 5 minutes
const PEER_PREFIX = "peer:";

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const path = url.pathname;
    const method = request.method;

    // CORS headers for all responses
    const corsHeaders = {
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Methods": "GET, POST, OPTIONS",
      "Access-Control-Allow-Headers": "Content-Type, X-Registry-Secret",
    };

    if (method === "OPTIONS") {
      return new Response(null, { status: 204, headers: corsHeaders });
    }

    try {
      switch (path) {
        case "/health":
          return json({ ok: true, service: "synthos-peer-registry" }, 200, corsHeaders);

        case "/peers":
          return await handleListPeers(env, false, corsHeaders);

        case "/peers/active":
          return await handleListPeers(env, true, corsHeaders);

        case "/register":
          if (method !== "POST") return error("method not allowed", 405, corsHeaders);
          return await handleRegister(request, env, corsHeaders);

        case "/deregister":
          if (method !== "POST") return error("method not allowed", 405, corsHeaders);
          return await handleDeregister(request, env, corsHeaders);

        case "/stats":
          return await handleStats(env, corsHeaders);

        default:
          return json({
            service: "synthos-peer-registry",
            endpoints: ["/health", "/peers", "/peers/active", "/register", "/deregister", "/stats"],
          }, 200, corsHeaders);
      }
    } catch (e) {
      return error(e.message, 500, corsHeaders);
    }
  },
};

// ─── Auth ──────────────────────────────────────────────────────────────────

function checkSecret(request, env) {
  const secret = env.REGISTRY_SECRET;
  if (!secret) return true; // No secret configured = open registry (dev mode)
  const provided = request.headers.get("X-Registry-Secret") || "";
  if (provided.length === 0) return false;
  // Constant-time comparison
  if (provided.length !== secret.length) return false;
  let mismatch = 0;
  for (let i = 0; i < secret.length; i++) {
    mismatch |= secret.charCodeAt(i) ^ provided.charCodeAt(i);
  }
  return mismatch === 0;
}

// ─── Handlers ──────────────────────────────────────────────────────────────

/**
 * POST /register
 * Body: { "name": "my-validator", "url": "https://...", "cloud": "cloudflare|aws|fly|railway|..." }
 *
 * Validators call this on startup and then every heartbeat interval.
 */
async function handleRegister(request, env, cors) {
  if (!checkSecret(request, env)) {
    return error("unauthorized", 401, cors);
  }

  const body = await request.json();
  const { name, url, cloud } = body;

  if (!name || !url) {
    return error("name and url are required", 400, cors);
  }

  // Validate URL format
  try {
    new URL(url);
  } catch {
    return error("invalid url format", 400, cors);
  }

  // Normalize: strip trailing slash
  const normalizedUrl = url.replace(/\/+$/, "");

  const peer = {
    name,
    url: normalizedUrl,
    cloud: cloud || "unknown",
    registered_at: new Date().toISOString(),
    last_seen: new Date().toISOString(),
  };

  // Check if already exists — preserve registration time
  const existing = await env.PEERS.get(PEER_PREFIX + name, "json");
  if (existing) {
    peer.registered_at = existing.registered_at;
  }

  await env.PEERS.put(PEER_PREFIX + name, JSON.stringify(peer));

  return json({ ok: true, action: existing ? "heartbeat" : "registered", peer }, 200, cors);
}

/**
 * POST /deregister
 * Body: { "name": "my-validator" }
 */
async function handleDeregister(request, env, cors) {
  if (!checkSecret(request, env)) {
    return error("unauthorized", 401, cors);
  }

  const body = await request.json();
  const { name } = body;

  if (!name) {
    return error("name is required", 400, cors);
  }

  await env.PEERS.delete(PEER_PREFIX + name);
  return json({ ok: true, action: "deregistered", name }, 200, cors);
}

/**
 * GET /peers or GET /peers/active
 * Returns the current peer list. If activeOnly, filters to peers seen in last 5 min.
 */
async function handleListPeers(env, activeOnly, cors) {
  const list = await env.PEERS.list({ prefix: PEER_PREFIX });
  const now = Date.now();
  const peers = [];

  for (const key of list.keys) {
    const peer = await env.PEERS.get(key.name, "json");
    if (!peer) continue;
    if (activeOnly) {
      const lastSeen = new Date(peer.last_seen).getTime();
      if (now - lastSeen > ACTIVE_THRESHOLD_MS) continue;
    }
    peers.push(peer);
  }

  // Sort by name for deterministic validator ordering
  peers.sort((a, b) => a.name.localeCompare(b.name));

  return json({
    count: peers.length,
    peers,
    // Provide the ordered validator names for consensus
    validator_order: peers.map((p) => p.name),
    // Provide just the URLs for easy peer list construction
    urls: peers.map((p) => p.url),
  }, 200, cors);
}

/**
 * GET /stats
 */
async function handleStats(env, cors) {
  const list = await env.PEERS.list({ prefix: PEER_PREFIX });
  const now = Date.now();
  let total = 0;
  let active = 0;
  const byClouds = {};

  for (const key of list.keys) {
    const peer = await env.PEERS.get(key.name, "json");
    if (!peer) continue;
    total++;
    const lastSeen = new Date(peer.last_seen).getTime();
    if (now - lastSeen <= ACTIVE_THRESHOLD_MS) active++;
    byClouds[peer.cloud] = (byClouds[peer.cloud] || 0) + 1;
  }

  return json({
    total_validators: total,
    active_validators: active,
    inactive_validators: total - active,
    by_cloud: byClouds,
    active_threshold_ms: ACTIVE_THRESHOLD_MS,
  }, 200, cors);
}

// ─── Helpers ───────────────────────────────────────────────────────────────

function json(data, status = 200, extraHeaders = {}) {
  return new Response(JSON.stringify(data, null, 2), {
    status,
    headers: { "Content-Type": "application/json", ...extraHeaders },
  });
}

function error(msg, status = 400, extraHeaders = {}) {
  return new Response(JSON.stringify({ error: msg }, null, 2), {
    status,
    headers: { "Content-Type": "application/json", ...extraHeaders },
  });
}
