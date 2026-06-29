// Synthos Mobile Validator Service Worker
// Provides offline caching and best-effort background node heartbeats.
// Browser limits still apply: iOS Safari may suspend background execution.

const CACHE_NAME = "synthos-validator-v1";
const CONFIG_CACHE = "synthos-validator-config-v1";
const CONFIG_URL = "/__synthos_background_config__";
const ASSETS = ["/", "/index.html", "/manifest.json", "/runtime.js"];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(ASSETS))
  );
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k)))
    )
  );
  self.clients.claim();
});

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);

  // Only cache same-origin assets (not API calls to validators)
  if (url.origin !== self.location.origin) return;

  event.respondWith(
    caches.match(event.request).then((cached) => {
      return cached || fetch(event.request).then((resp) => {
        const clone = resp.clone();
        caches.open(CACHE_NAME).then((cache) => cache.put(event.request, clone));
        return resp;
      });
    })
  );
});

self.addEventListener("message", (event) => {
  const msg = event.data || {};
  if (msg.type !== "SYNTHOS_BACKGROUND_CONFIG") return;

  event.waitUntil(
    caches.open(CONFIG_CACHE).then((cache) =>
      cache.put(
        CONFIG_URL,
        new Response(JSON.stringify(msg.payload || {}), {
          headers: { "Content-Type": "application/json" },
        })
      )
    )
  );
});

self.addEventListener("sync", (event) => {
  if (event.tag === "synthos-node-heartbeat") {
    event.waitUntil(runBackgroundHeartbeat());
  }
});

self.addEventListener("periodicsync", (event) => {
  if (event.tag === "synthos-node-heartbeat") {
    event.waitUntil(runBackgroundHeartbeat());
  }
});

async function loadBackgroundConfig() {
  const cache = await caches.open(CONFIG_CACHE);
  const resp = await cache.match(CONFIG_URL);
  if (!resp) return null;
  return resp.json();
}

async function runBackgroundHeartbeat() {
  const cfg = await loadBackgroundConfig();
  const registryUrls = cfg?.registryUrls || (cfg?.registryUrl ? [cfg.registryUrl] : []);
  if (!cfg?.peerId || registryUrls.length === 0) return;

  await Promise.allSettled(registryUrls.map((registryUrl) =>
    fetch(`${registryUrl}/register`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: cfg.peerId,
        url: cfg.nodeUrl || self.location.origin,
        cloud: cfg.cloud || "mobile-background",
        mode: cfg.runtimeTier || "browser-sentinel",
        role: "immune-sentinel",
        consensus_eligible: false,
        inbound_ports: 0,
        background: "best-effort",
        heartbeat_at: new Date().toISOString(),
      }),
    })
  ));
}
