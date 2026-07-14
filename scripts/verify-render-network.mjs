#!/usr/bin/env node

const endpoints = process.argv.slice(2);
const urls = endpoints.length
  ? endpoints
  : [11, 12, 13, 14, 15].map((n) => `https://synthos-validator-${n}.onrender.com`);

async function json(url) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 20_000);
  try {
    const response = await fetch(url, { signal: controller.signal, headers: { "user-agent": "synthos-render-verify" } });
    const text = await response.text();
    if (!response.ok) throw new Error(`${response.status} ${response.statusText}: ${text.slice(0, 200)}`);
    return JSON.parse(text);
  } finally {
    clearTimeout(timer);
  }
}

const rows = [];
for (const url of urls) {
  try {
    const status = await json(`${url.replace(/\/$/, "")}/status`);
    rows.push({
      url,
      ok: true,
      height: status.height,
      tip: status.tip,
      state_root: status.state_root,
      peers: Array.isArray(status.peers) ? status.peers.length : 0,
    });
  } catch (error) {
    rows.push({ url, ok: false, error: error.message });
  }
}

console.table(rows);

const live = rows.filter((row) => row.ok);
const allLive = live.length === urls.length;
const first = live[0] && `${live[0].height}:${live[0].tip}:${live[0].state_root}`;
const converged = allLive && live.every((row) => `${row.height}:${row.tip}:${row.state_root}` === first);

if (!allLive || !converged) {
  console.error(`Render network verification failed: allLive=${allLive} converged=${converged}`);
  process.exit(1);
}

console.log("Render network verification passed.");
