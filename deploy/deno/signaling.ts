/**
 * Synthos Signaling + Peer Registry — Deno Deploy
 *
 * Replaces the Cloudflare Worker peer-registry.
 * Two jobs:
 *   1. WebSocket signaling — phones exchange WebRTC offers/answers here
 *   2. HTTP peer registry — validators register, clients discover peers
 *
 * Deploy:
 *   1. Go to https://dash.deno.com → New Project
 *   2. Link your GitHub repo
 *   3. Set entry point: deploy/deno/signaling.ts
 *   4. Deploy
 *
 * Or CLI:
 *   deployctl deploy --project=synthos-signal deploy/deno/signaling.ts
 */

// ─── State ──────────────────────────────────────────────────────────────────

// Connected WebSocket peers: peerId → WebSocket
const signalingPeers = new Map<string, WebSocket>();

// Registered HTTP peers (validators): in-memory (peers re-register on connect)
const registeredPeers = new Map<string, any>();

const STALE_MS = 5 * 60 * 1000; // 5 minutes

// ─── HTTP Server ────────────────────────────────────────────────────────────

Deno.serve(async (req: Request): Promise<Response> => {
  const url = new URL(req.url);
  const path = url.pathname;

  // CORS preflight
  if (req.method === "OPTIONS") {
    return new Response(null, { headers: corsHeaders() });
  }

  // Health
  if (path === "/health") {
    return json({ ok: true, service: "synthos-signaling", runtime: "deno" });
  }

  // WebSocket signaling
  if (path === "/signal") {
    const peerId = url.searchParams.get("id");
    if (!peerId) return json({ error: "missing ?id= parameter" }, 400);

    const safePeerId = peerId.slice(0, 64).replace(/[^a-zA-Z0-9_-]/g, "");

    const upgrade = req.headers.get("upgrade") || "";
    if (upgrade.toLowerCase() !== "websocket") {
      // Non-WS request: return connected peer list
      const peers = [...signalingPeers.keys()];
      return json({ connected_peers: peers, total: peers.length });
    }

    const { socket, response } = Deno.upgradeWebSocket(req);
    handleSignalingSocket(socket, safePeerId);
    return response;
  }

  // Register validator
  if (path === "/register" && req.method === "POST") {
    return await handleRegister(req);
  }

  // List peers
  if (path === "/peers" && req.method === "GET") {
    return await handleListPeers(false);
  }
  if (path === "/peers/active" && req.method === "GET") {
    return await handleListPeers(true);
  }

  // Delete peer
  if (path.startsWith("/peers/") && req.method === "DELETE") {
    const name = path.replace("/peers/", "");
    registeredPeers.delete(name);
    return json({ ok: true, deleted: name });
  }

  return json({
    service: "synthos-signaling",
    endpoints: ["/health", "/signal?id=synthos-peer-1", "/register", "/peers", "/peers/active"],
  });
});

// ─── WebSocket Signaling ────────────────────────────────────────────────────

function handleSignalingSocket(ws: WebSocket, peerId: string) {
  // Close existing connection for this peer (reconnect)
  if (signalingPeers.has(peerId)) {
    try { signalingPeers.get(peerId)!.close(1000, "reconnect"); } catch { /* ok */ }
  }
  signalingPeers.set(peerId, ws);

  ws.onopen = () => {
    // Notify existing peers about the new peer
    const peerList = [...signalingPeers.keys()];
    broadcast(peerId, JSON.stringify({
      type: "peer-joined",
      peerId,
      peers: peerList,
    }));

    // Send the new peer the current peer list
    try {
      ws.send(JSON.stringify({
        type: "peer-list",
        peers: peerList.filter(p => p !== peerId),
        yourId: peerId,
      }));
    } catch { /* ok */ }
  };

  ws.onmessage = (event) => {
    let data: any;
    try { data = JSON.parse(String(event.data)); } catch { return; }

    // Route signaling messages: offer, answer, ice-candidate
    if (!data.type || !data.to) return;

    const target = signalingPeers.get(data.to);
    if (target && target.readyState === WebSocket.OPEN) {
      try {
        target.send(JSON.stringify({ ...data, from: peerId }));
      } catch { /* ok */ }
    }
  };

  ws.onclose = () => {
    signalingPeers.delete(peerId);
    broadcast(peerId, JSON.stringify({ type: "peer-left", peerId }));
  };

  ws.onerror = () => {
    signalingPeers.delete(peerId);
  };
}

function broadcast(excludeId: string, message: string) {
  for (const [id, socket] of signalingPeers) {
    if (id === excludeId) continue;
    if (socket.readyState === WebSocket.OPEN) {
      try { socket.send(message); } catch { /* ok */ }
    }
  }
}

// ─── Peer Registry ──────────────────────────────────────────────────────────

async function handleRegister(req: Request): Promise<Response> {
  let body: any;
  try { body = await req.json(); } catch { return json({ error: "invalid JSON" }, 400); }

  if (!body.name || !body.url) {
    return json({ error: "name and url required" }, 400);
  }

  try { new URL(body.url); } catch { return json({ error: "invalid url" }, 400); }

  const name = String(body.name).slice(0, 64).replace(/[^a-zA-Z0-9_-]/g, "");
  const peerUrl = String(body.url).slice(0, 256);
  const cloud = String(body.cloud || "unknown").slice(0, 32);

  const entry = {
    name,
    url: peerUrl,
    cloud,
    registered_at: Date.now(),
    last_seen: Date.now(),
  };

  registeredPeers.set(name, entry);
  // Auto-expire after 1 hour
  setTimeout(() => { if (registeredPeers.get(name)?.registered_at === entry.registered_at) registeredPeers.delete(name); }, 3600_000);

  return json({ ok: true, peer: name, message: "registered" });
}

async function handleListPeers(activeOnly: boolean): Promise<Response> {
  const peers: any[] = [];
  const now = Date.now();

  for (const peer of registeredPeers.values()) {
    const stale = now - peer.last_seen > STALE_MS;
    if (activeOnly && stale) continue;
    peers.push({ ...peer, stale });
  }

  peers.sort((a, b) => a.name.localeCompare(b.name));

  return json({
    peers,
    urls: peers.map(p => p.url),
    validator_order: peers.map(p => p.name),
    total: peers.length,
    active_only: activeOnly,
  });
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function json(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data, null, 2), {
    status,
    headers: {
      "Content-Type": "application/json",
      ...corsHeaders(),
    },
  });
}

function corsHeaders(): Record<string, string> {
  return {
    "Access-Control-Allow-Origin": "*",
    "Access-Control-Allow-Methods": "GET, POST, DELETE, OPTIONS",
    "Access-Control-Allow-Headers": "Content-Type, X-Registry-Secret",
  };
}
