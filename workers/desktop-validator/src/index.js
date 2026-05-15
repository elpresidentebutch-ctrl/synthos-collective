/**
 * Synthos Mobile Validator — Cloudflare Worker
 *
 * Serves the mobile validator PWA as a static site.
 * GET /          → index.html
 * GET /sw.js     → Service Worker
 * GET /manifest.json → PWA manifest
 */

import HTML from "../index.html";
import SW from "../sw.js";
import MANIFEST from "../manifest.json";

function headers(contentType, cacheSeconds = 0) {
  const h = {
    "Content-Type": contentType,
    "Access-Control-Allow-Origin": "*",
    "X-Content-Type-Options": "nosniff",
  };
  if (cacheSeconds > 0) {
    h["Cache-Control"] = `public, max-age=${cacheSeconds}`;
  }
  return h;
}

export default {
  async fetch(request) {
    const url = new URL(request.url);
    const path = url.pathname;

    // Health check
    if (path === "/health") {
      return new Response(JSON.stringify({ ok: true, service: "synthos-mobile" }), {
        headers: headers("application/json"),
      });
    }

    // Service Worker
    if (path === "/sw.js") {
      return new Response(SW, {
        headers: headers("application/javascript", 3600),
      });
    }

    // PWA manifest
    if (path === "/manifest.json") {
      return new Response(MANIFEST, {
        headers: headers("application/manifest+json", 86400),
      });
    }

    // Everything else → serve the HTML app
    return new Response(HTML, {
      headers: headers("text/html; charset=utf-8"),
    });
  },
};
