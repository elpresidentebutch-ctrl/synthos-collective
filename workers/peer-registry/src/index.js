/**
 * Synthos Peer Registry + WebRTC Signaling — Cloudflare Worker
 *
 * Two functions in one:
 * 1. HTTP peer registry — validators register/heartbeat, clients discover peers
 * 2. WebSocket signaling — relays WebRTC offers/answers/ICE between mobile validators
 *
 * HTTP Endpoints:
 *   POST /register        — Register/heartbeat a validator
 *   GET  /peers           — List all known peers
 *   GET  /peers/active    — List peers seen in the last 5 minutes
 *   GET  /mailbox?name=x  — Poll outbound-only messages for a silent node
 *   POST /mailbox         — Queue a message for a silent node
 *   GET  /health          — Health check
 *   DELETE /peers/:name   — Deregister a peer (requires secret)
 *
 * WebSocket:
 *   GET /signal?id=myPeerId  — Upgrade to WebSocket for WebRTC signaling
 */

const STALE_THRESHOLD_MS = 5 * 60 * 1000; // 5 minutes
const EARLY_OPERATOR_REWARD = 500; // SYN for approved early verified operators
const MAX_REWARDED_OPERATORS = 200;

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const path = url.pathname;

    // CORS
    if (request.method === "OPTIONS") {
      return new Response(null, {
        headers: {
          "Access-Control-Allow-Origin": "*",
          "Access-Control-Allow-Methods": "GET, POST, DELETE, OPTIONS",
          "Access-Control-Allow-Headers": "Content-Type, X-Registry-Secret",
        },
      });
    }

    try {
      if (path === "/health") {
        return json({ ok: true, service: "synthos-peer-registry", signaling: true });
      }

      // WebSocket signaling — route to Durable Object
      if (path === "/signal") {
        const id = env.SIGNALING.idFromName("synthos-signal-room");
        const stub = env.SIGNALING.get(id);
        return stub.fetch(request);
      }

      if (path === "/register" && request.method === "POST") {
        return await handleRegister(request, env);
      }

      if (path === "/mailbox" && request.method === "GET") {
        return await handleMailboxPoll(request, env);
      }

      if (path === "/mailbox" && request.method === "POST") {
        return await handleMailboxPost(request, env);
      }

      if (path === "/peers" && request.method === "GET") {
        return await handleListPeers(env, false);
      }

      if (path === "/register-identity" && request.method === "POST") {
        return await handleRegisterIdentity(request, env);
      }

      if (path === "/peers/active" && request.method === "GET") {
        return await handleListPeers(env, true);
      }

      if (path.startsWith("/peers/") && request.method === "DELETE") {
        return await handleDeregister(request, env, path);
      }

      return json({
        service: "synthos-peer-registry",
        endpoints: ["/health", "/register", "/register-identity", "/peers", "/peers/active", "/mailbox?name=YOUR_NODE", "/signal?id=YOUR_PEER_ID"],
      });
    } catch (e) {
      return json({ error: e.message }, 500);
    }
  },
};

// ─── HTTP Handlers ──────────────────────────────────────────────────────────

async function handleRegister(request, env) {
  const body = await request.json();

  if (!body.name) {
    return json({ error: "name required" }, 400);
  }

  const rawUrl = String(body.url || "").slice(0, 256);
  if (rawUrl !== "") {
    try {
      new URL(rawUrl);
    } catch {
      return json({ error: "invalid url" }, 400);
    }
  }

  const name = String(body.name).slice(0, 64).replace(/[^a-zA-Z0-9_-]/g, "");
  const peerUrl = rawUrl;
  const cloud = String(body.cloud || "unknown").slice(0, 32);

  const entry = {
    name,
    url: peerUrl,
    cloud,
    mode: String(body.mode || (peerUrl === "" ? "outbound_only" : "reachable")).slice(0, 64),
    inbound_ports: Number(body.inbound_ports || 0),
    hardware_commitment: String(body.hardware_commitment || "").slice(0, 128),
    registered_at: Date.now(),
    last_seen: Date.now(),
  };

  await env.PEERS.put(`peer:${name}`, JSON.stringify(entry), {
    expirationTtl: 3600,
  });

  // Early verified operator reward. Heartbeat rewards are paid monthly in arrears.
  let reward = null;
  const rewardKey = `reward:${name}`;
  const alreadyRewarded = await env.PEERS.get(rewardKey);

  if (!alreadyRewarded) {
    // Count total unique rewarded operators.
    const counterRaw = await env.PEERS.get("meta:rewarded_count");
    const rewardedCount = counterRaw ? parseInt(counterRaw, 10) : 0;

    if (rewardedCount < MAX_REWARDED_OPERATORS) {
      const newCount = rewardedCount + 1;
      // Mark as rewarded (permanent — no TTL)
      await env.PEERS.put(rewardKey, JSON.stringify({
        rewarded_at: Date.now(),
        amount: EARLY_OPERATOR_REWARD,
        position: newCount,
      }));
      await env.PEERS.put("meta:rewarded_count", String(newCount));

      // Submit reward TX to a live validator
      reward = { amount: EARLY_OPERATOR_REWARD, position: newCount, total: MAX_REWARDED_OPERATORS };
      try {
        const txBody = {
          from: "agent-0",
          to: name,
          amount: EARLY_OPERATOR_REWARD,
          memo: `early-operator-reward #${newCount}`,
        };
        // Try to submit to the first available validator
        const validators = [
          "https://synthos-validator-11.jamesishamwilliams.workers.dev",
          "https://synthos-validator-12.jamesishamwilliams.workers.dev",
        ];
        for (const v of validators) {
          try {
            const resp = await fetch(`${v}/submitTx`, {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify(txBody),
            });
            if (resp.ok) break;
          } catch { /* try next */ }
        }
      } catch { /* reward TX is best-effort */ }
    }
  }

  return json({ ok: true, peer: name, message: "registered", reward });
}

async function handleMailboxPoll(request, env) {
  const url = new URL(request.url);
  const name = sanitizeName(url.searchParams.get("name"));
  if (!name) return json({ error: "name required" }, 400);

  const key = `mailbox:${name}`;
  const raw = await env.PEERS.get(key);
  if (!raw) return json([]);

  let messages = [];
  try {
    messages = JSON.parse(raw);
  } catch {
    messages = [];
  }

  await env.PEERS.delete(key);
  return json(messages);
}

async function handleMailboxPost(request, env) {
  const secret = request.headers.get("X-Registry-Secret");
  const envSecret = env.REGISTRY_SECRET;
  if (envSecret && secret !== envSecret) {
    return json({ error: "unauthorized" }, 401);
  }

  const body = await request.json();
  const to = sanitizeName(body.to || body.name);
  if (!to) return json({ error: "to required" }, 400);

  const message = {
    id: String(body.id || crypto.randomUUID()).slice(0, 128),
    type: String(body.type || "message").slice(0, 64),
    from: String(body.from || "relay").slice(0, 64),
    payload: body.payload ?? body.message ?? null,
    created_at: Date.now(),
  };

  const key = `mailbox:${to}`;
  const raw = await env.PEERS.get(key);
  let messages = [];
  if (raw) {
    try {
      messages = JSON.parse(raw);
    } catch {
      messages = [];
    }
  }
  messages.push(message);
  messages = messages.slice(-100);

  await env.PEERS.put(key, JSON.stringify(messages), { expirationTtl: 3600 });
  return json({ ok: true, queued: true, to, id: message.id, depth: messages.length });
}

async function handleRegisterIdentity(request, env) {
  const body = await request.json();
  if (!body.publicKey) {
    return json({ error: "publicKey required" }, 400);
  }

  const pubKey = String(body.publicKey).slice(0, 128);
  const envelope = String(body.envelopeRoot || "").slice(0, 128);

  const entry = {
    publicKey: pubKey,
    envelopeRoot: envelope,
    registered_at: Date.now(),
  };

  await env.PEERS.put(`identity:${pubKey}`, JSON.stringify(entry));
  return json({ ok: true, message: "Sovereign Identity Persistent Storage Complete" });
}

async function handleListPeers(env, activeOnly) {
  const list = await env.PEERS.list({ prefix: "peer:" });
  const peers = [];
  const now = Date.now();

  for (const key of list.keys) {
    const raw = await env.PEERS.get(key.name);
    if (!raw) continue;

    try {
      const peer = JSON.parse(raw);
      const stale = now - peer.last_seen > STALE_THRESHOLD_MS;
      if (activeOnly && stale) continue;
      peers.push({ ...peer, stale });
    } catch {
      continue;
    }
  }

  peers.sort((a, b) => a.name.localeCompare(b.name));

  return json({
    peers,
    urls: peers.map((p) => p.url),
    validator_order: peers.map((p) => p.name),
    total: peers.length,
    active_only: activeOnly,
  });
}

async function handleDeregister(request, env, path) {
  const secret = request.headers.get("X-Registry-Secret");
  const envSecret = env.REGISTRY_SECRET;

  if (envSecret && secret !== envSecret) {
    return json({ error: "unauthorized" }, 401);
  }

  const name = path.replace("/peers/", "");
  await env.PEERS.delete(`peer:${name}`);
  return json({ ok: true, deleted: name });
}

// ─── WebRTC Signaling Durable Object ────────────────────────────────────────

export class SignalingDO {
  constructor(state) {
    this.state = state;
    // Map of peerId -> WebSocket
    this.connections = new Map();
  }

  async fetch(request) {
    const url = new URL(request.url);
    const peerId = url.searchParams.get("id");

    if (!peerId) {
      return json({ error: "missing ?id= parameter" }, 400);
    }

    // Sanitize peer ID
    const safePeerId = String(peerId).slice(0, 64).replace(/[^a-zA-Z0-9_-]/g, "");

    const upgradeHeader = request.headers.get("Upgrade");
    if (!upgradeHeader || upgradeHeader.toLowerCase() !== "websocket") {
      // Non-WebSocket: return list of connected signaling peers
      const connectedPeers = [...this.connections.keys()];
      return json({ connected_peers: connectedPeers, total: connectedPeers.length });
    }

    // WebSocket upgrade
    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);

    this.state.acceptWebSocket(server, [safePeerId]);

    // Close any existing connection for this peer (reconnect)
    if (this.connections.has(safePeerId)) {
      try { this.connections.get(safePeerId).close(1000, "reconnect"); } catch {}
    }
    this.connections.set(safePeerId, server);

    // Notify all existing peers about the new peer
    const peerList = [...this.connections.keys()];
    this.broadcast(safePeerId, JSON.stringify({
      type: "peer-joined",
      peerId: safePeerId,
      peers: peerList,
    }));

    // Send the new peer the current peer list
    try {
      server.send(JSON.stringify({
        type: "peer-list",
        peers: peerList.filter(p => p !== safePeerId),
        yourId: safePeerId,
      }));
    } catch {}

    return new Response(null, { status: 101, webSocket: client });
  }

  async webSocketMessage(ws, message) {
    let data;
    try {
      data = JSON.parse(message);
    } catch {
      return;
    }

    // Validate message structure
    if (!data.type || !data.to) return;

    const fromId = this.getPeerId(ws);
    if (!fromId) return;

    // Route signaling messages: offer, answer, ice-candidate
    const target = this.connections.get(data.to);
    if (target) {
      try {
        target.send(JSON.stringify({
          ...data,
          from: fromId,
        }));
      } catch {}
    }
  }

  async webSocketClose(ws) {
    const peerId = this.getPeerId(ws);
    if (peerId) {
      this.connections.delete(peerId);
      this.broadcast(peerId, JSON.stringify({
        type: "peer-left",
        peerId,
      }));
    }
  }

  async webSocketError(ws) {
    const peerId = this.getPeerId(ws);
    if (peerId) {
      this.connections.delete(peerId);
    }
  }

  getPeerId(ws) {
    for (const [id, socket] of this.connections) {
      if (socket === ws) return id;
    }
    // Try from tags
    const tags = this.state.getTags(ws);
    return tags?.[0] || null;
  }

  broadcast(excludePeerId, message) {
    for (const [id, socket] of this.connections) {
      if (id === excludePeerId) continue;
      try { socket.send(message); } catch {}
    }
  }
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function json(data, status = 200) {
  return new Response(JSON.stringify(data, null, 2), {
    status,
    headers: {
      "Content-Type": "application/json",
      "Access-Control-Allow-Origin": "*",
    },
  });
}

function sanitizeName(value) {
  return String(value || "").slice(0, 64).replace(/[^a-zA-Z0-9_-]/g, "");
}
