/**
 * SYNTHOS Block Explorer — Cloudflare Worker
 *
 * Reads chain state from all validator nodes and serves it to dashboards.
 * Dashboards call this worker's /api/* endpoints — they never hit validators directly.
 *
 * GET /              — Explorer UI (HTML)
 * GET /api/status    — Aggregated chain status (height, tip, validators)
 * GET /api/blocks    — Recent blocks across all validators (?limit=N)
 * GET /api/stats     — Network stats (TPS, block rate, proposers)
 * GET /api/mempool   — Aggregated mempool size
 * GET /health        — Health check
 */

const VALIDATORS = [
  "https://synthos-validator-11.jamesishamwilliams.workers.dev",
  "https://synthos-validator-12.jamesishamwilliams.workers.dev",
  "https://synthos-validator-13.jamesishamwilliams.workers.dev",
  "https://synthos-validator-14.jamesishamwilliams.workers.dev",
  "https://synthos-validator-15.jamesishamwilliams.workers.dev",
];

const PEER_REGISTRY = "https://synthos-peer-registry.jamesishamwilliams.workers.dev";

const TIMEOUT = 5000;

function cors() {
  return {
    "Access-Control-Allow-Origin": "*",
    "Access-Control-Allow-Methods": "GET, OPTIONS",
    "Access-Control-Allow-Headers": "Content-Type",
  };
}

function json(data, status = 200) {
  return new Response(JSON.stringify(data, null, 2), {
    status,
    headers: { "Content-Type": "application/json", ...cors() },
  });
}

// Fetch from a single validator
async function fetchValidator(url, path) {
  try {
    const hostname = url.split("//")[1];
    const resp = await fetch(`${url}${path}`, {
      headers: {
        "Host": hostname,
        "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
        "Accept": "application/json"
      }
    });
    if (!resp.ok) {
      console.log(`[EXPLORER] Fetch failed for ${url}${path}: ${resp.status}`);
      return null;
    }
    return await resp.json();
  } catch (e) {
    console.log(`[EXPLORER] Fetch error for ${url}${path}: ${e.message}`);
    return null;
  }
}

// Fetch from all validators in parallel, filter nulls
async function fetchAll(path) {
  const results = await Promise.all(VALIDATORS.map((v) => fetchValidator(v, path)));
  return results
    .map((data, i) => ({ url: VALIDATORS[i], data }))
    .filter((r) => r.data !== null);
}

export default {
  async fetch(request) {
    const url = new URL(request.url);
    const path = url.pathname;

    if (request.method === "OPTIONS") {
      return new Response(null, { status: 204, headers: cors() });
    }

    if (path === "/health") {
      let testFetch = "not attempted";
      let valFetch = "not attempted";
      try {
        const t = await fetch("https://www.google.com", { method: "HEAD" });
        testFetch = `google.com status: ${t.status}`;
      } catch (e) {
        testFetch = `google.com error: ${e.message}`;
      }
      
      try {
        const v11 = VALIDATORS[0] + "/status";
        const t = await fetch(v11, { method: "GET" });
        valFetch = `validator-11 status: ${t.status}`;
      } catch (e) {
        valFetch = `validator-11 error: ${e.message} stack: ${e.stack}`;
      }

      return json({ 
        ok: true, 
        service: "synthos-block-explorer", 
        validators: VALIDATORS.length,
        test_connectivity: testFetch,
        validator_connectivity: valFetch
      });
    }

    if (path === "/api/status") {
      return handleStatus();
    }

    if (path === "/api/blocks") {
      const limit = parseInt(url.searchParams.get("limit") || "30", 10);
      return handleBlocks(limit);
    }

    if (path === "/api/stats") {
      return handleStats();
    }

    if (path === "/api/mempool") {
      return handleMempool();
    }

    if (path === "/" || path === "/index.html") {
      return new Response(explorerHTML(), {
        headers: { "Content-Type": "text/html;charset=UTF-8" },
      });
    }

    return json({ error: "not found", endpoints: ["/", "/api/status", "/api/blocks", "/api/stats", "/api/mempool", "/health"] }, 404);
  },

  // Background cron — could warm cache in future
  async scheduled() {
    console.log("[CRON] Block explorer heartbeat");
  },
};

// ── API Handlers ──────────────────────────────────────────────────────────────

async function handleStatus() {
  const [statuses, registry] = await Promise.all([
    fetchAll("/status"),
    fetchValidator(PEER_REGISTRY, "/peers/active"),
  ]);

  if (statuses.length === 0) {
    return json({ error: "no validators reachable" }, 503);
  }

  // Best chain tip = highest height
  const best = statuses.reduce((a, b) =>
    (b.data.height || 0) > (a.data.height || 0) ? b : a
  );

  const heights = statuses.map((s) => s.data.height || 0);
  const synced = heights.filter((h) => h >= (best.data.height || 0) - 2).length;

  return json({
    highest_height: best.data.height,
    tip_hash: best.data.tip,
    state_root: best.data.state_root,
    best_validator: best.data.validator,
    next_proposer: best.data.next_proposer,
    validators_reachable: statuses.length,
    validators_synced: synced,
    registry_total: registry?.total || statuses.length,
    mempool_size: best.data.mempool_size || 0,
    chain_id: best.data.chain_id,
    fetched_at: new Date().toISOString(),
  });
}

async function handleBlocks(limit) {
  // Ask validators for their latest blocks
  const bestStatusResp = await handleStatus();
  const bestStatus = await bestStatusResp.json();

  if (bestStatus.error) return json({ blocks: [], count: 0 });

  const height = bestStatus.highest_height || 0;
  const from = Math.max(0, height - limit + 1);
  const validatorUrl = VALIDATORS.find((v) =>
    v.includes(bestStatus.best_validator)
  ) || VALIDATORS[0];

  const data = await fetchValidator(validatorUrl, `/blocks?from=${from}`);
  const blocks = (data?.blocks || []).slice(-limit).reverse().map((b) => ({
    height: b.header?.height,
    hash: b.hash,
    proposer: b.header?.proposer_id,
    tx_count: (b.tx || []).length,
    state_root: b.header?.state_root,
    finalized: b.finalized,
  }));

  return json({ blocks, count: blocks.length, from_height: from });
}

async function handleStats() {
  const statuses = await fetchAll("/status");
  if (statuses.length === 0) return json({ error: "no validators reachable" }, 503);

  const heights = statuses.map((s) => s.data.height || 0);
  const maxHeight = Math.max(...heights);
  const minHeight = Math.min(...heights);
  const proposers = [...new Set(statuses.map((s) => s.data.next_proposer).filter(Boolean))];

  return json({
    highest_height: maxHeight,
    lowest_height: minHeight,
    height_spread: maxHeight - minHeight,
    active_validators: statuses.length,
    known_proposers: proposers,
    total_validators_configured: VALIDATORS.length,
    fetched_at: new Date().toISOString(),
  });
}

async function handleMempool() {
  const mempools = await fetchAll("/mempool");
  const total = mempools.reduce((sum, r) => sum + (r.data.size || 0), 0);
  return json({
    total_pending: total,
    per_validator: mempools.map((r) => ({
      validator: r.url.split("//")[1].split(".")[0],
      size: r.data.size || 0,
    })),
  });
}

// ── Explorer HTML (inlined) ───────────────────────────────────────────────────

function explorerHTML() {
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>Synthos Block Explorer</title>
<meta name="description" content="Live Synthos blockchain explorer — real-time block feed, validator status, and network stats.">
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;600;700&family=JetBrains+Mono:wght@400;600&display=swap" rel="stylesheet">
<style>
:root{--bg:#030712;--card:rgba(17,24,39,.78);--accent:#8b5cf6;--green:#22c55e;--sky:#38bdf8;--red:#f87171;--text:#f3f4f6;--muted:#6b7280;--border:rgba(255,255,255,.08);--border-v:rgba(139,92,246,.22);}
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:'Inter',sans-serif;background:var(--bg);color:var(--text);padding:32px 40px;min-height:100vh;}
body::before{content:'';position:fixed;inset:0;background-image:linear-gradient(rgba(139,92,246,.04) 1px,transparent 1px),linear-gradient(90deg,rgba(139,92,246,.04) 1px,transparent 1px);background-size:48px 48px;pointer-events:none;z-index:0;}
.wrap{position:relative;z-index:1;max-width:1160px;margin:0 auto;}
header{display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:12px;padding-bottom:24px;border-bottom:1px solid var(--border);margin-bottom:32px;}
h1{font-size:1.5rem;font-weight:700;background:linear-gradient(135deg,#fff,#94a3b8);-webkit-background-clip:text;-webkit-text-fill-color:transparent;}
.sub{font-size:.78rem;color:var(--muted);margin-top:3px;}
.pill{display:flex;align-items:center;gap:6px;padding:5px 14px;border-radius:99px;font-size:.73rem;font-weight:700;border:1px solid;}
.pill-green{background:rgba(34,197,94,.08);color:var(--green);border-color:rgba(34,197,94,.2);}
.pill-sky{background:rgba(56,189,248,.08);color:var(--sky);border-color:rgba(56,189,248,.2);}
.dot{width:7px;height:7px;border-radius:50%;background:currentColor;animation:blink 2s infinite;}
@keyframes blink{0%,100%{opacity:1}50%{opacity:.4}}

.kpis{display:grid;grid-template-columns:repeat(5,1fr);gap:16px;margin-bottom:28px;}
@media(max-width:900px){.kpis{grid-template-columns:repeat(3,1fr)}}
@media(max-width:580px){.kpis{grid-template-columns:repeat(2,1fr)}}
.kpi{background:var(--card);backdrop-filter:blur(12px);border:1px solid var(--border);border-radius:14px;padding:18px 20px;transition:border-color .25s;}
.kpi:hover{border-color:var(--border-v);}
.kpi-label{font-size:.7rem;font-weight:700;text-transform:uppercase;letter-spacing:.07em;color:var(--muted);margin-bottom:8px;}
.kpi-val{font-size:1.75rem;font-weight:700;font-family:'JetBrains Mono',monospace;line-height:1;}
.kpi-val.flash{animation:kflash .35s ease;}
@keyframes kflash{0%{color:var(--green)}100%{color:var(--text)}}
.kpi-sub{font-size:.7rem;color:var(--muted);margin-top:4px;}

.grid{display:grid;grid-template-columns:1fr 300px;gap:20px;}
@media(max-width:840px){.grid{grid-template-columns:1fr}}
.card{background:var(--card);backdrop-filter:blur(12px);border:1px solid var(--border);border-radius:16px;padding:22px 24px;}
.card-title{font-size:.72rem;font-weight:700;text-transform:uppercase;letter-spacing:.08em;color:var(--muted);margin-bottom:18px;display:flex;justify-content:space-between;align-items:center;}
.card-title span{font-size:.68rem;color:var(--muted);font-weight:400;text-transform:none;letter-spacing:0;}

table{width:100%;border-collapse:collapse;}
th{text-align:left;font-size:.68rem;text-transform:uppercase;letter-spacing:.07em;color:var(--muted);padding:10px 8px;border-bottom:1px solid var(--border);font-weight:600;}
td{padding:13px 8px;border-bottom:1px solid var(--border);font-size:.82rem;vertical-align:middle;}
tbody tr{transition:background .15s;}
tbody tr:hover{background:rgba(255,255,255,.02);}
tbody tr.new{animation:slide .3s ease;}
@keyframes slide{from{opacity:0;transform:translateY(-6px)}to{opacity:1;transform:translateY(0)}}

.mono{font-family:'JetBrains Mono',monospace;color:var(--accent);font-size:.76rem;}
.tag{display:inline-block;font-size:.65rem;font-weight:700;padding:2px 7px;border-radius:4px;text-transform:uppercase;letter-spacing:.04em;}
.tg-block{background:rgba(34,197,94,.1);border:1px solid rgba(34,197,94,.22);color:var(--green);}
.tg-miss{background:rgba(248,113,113,.1);border:1px solid rgba(248,113,113,.22);color:var(--red);}

.vlist{display:flex;flex-direction:column;gap:10px;}
.vitem{display:flex;justify-content:space-between;align-items:center;background:rgba(255,255,255,.03);padding:10px 14px;border-radius:9px;border:1px solid var(--border);}
.vname{font-size:.76rem;font-family:'JetBrains Mono',monospace;color:var(--accent);}
.vstatus{font-size:.64rem;font-weight:700;padding:2px 8px;border-radius:99px;}
.vs-ok{background:rgba(34,197,94,.1);color:var(--green);border:1px solid rgba(34,197,94,.2);}
.vs-off{background:rgba(248,113,113,.1);color:var(--red);border:1px solid rgba(248,113,113,.2);}

.footer{font-size:.7rem;color:var(--muted);text-align:right;margin-top:14px;}
.status-bar{font-size:.7rem;color:var(--muted);margin-top:10px;padding-top:8px;border-top:1px solid var(--border);display:flex;justify-content:space-between;}
</style>
</head>
<body>
<div class="wrap">
  <header>
    <div>
      <h1>Synthos Block Explorer</h1>
      <div class="sub">Live chain state — powered by the Block Explorer API</div>
    </div>
    <div style="display:flex;gap:10px;align-items:center;">
      <div class="pill pill-sky" id="api-status"><span class="dot"></span> Connecting...</div>
      <div class="pill pill-green"><span class="dot"></span> Network Active</div>
    </div>
  </header>

  <div class="kpis">
    <div class="kpi"><div class="kpi-label">Block Height</div><div class="kpi-val" id="k-height">—</div><div class="kpi-sub" id="k-height-sub">—</div></div>
    <div class="kpi"><div class="kpi-label">Validators Online</div><div class="kpi-val" id="k-validators">—</div><div class="kpi-sub">reachable</div></div>
    <div class="kpi"><div class="kpi-label">Mempool</div><div class="kpi-val" id="k-mempool">—</div><div class="kpi-sub">pending txs</div></div>
    <div class="kpi"><div class="kpi-label">Next Proposer</div><div class="kpi-val" id="k-proposer" style="font-size:1rem;padding-top:6px">—</div><div class="kpi-sub">&nbsp;</div></div>
    <div class="kpi"><div class="kpi-label">Height Spread</div><div class="kpi-val" id="k-spread">—</div><div class="kpi-sub">sync drift</div></div>
  </div>

  <div class="grid">
    <div class="card">
      <div class="card-title">Latest Blocks <span id="block-count"></span></div>
      <table>
        <thead><tr><th>Height</th><th>Hash</th><th>Proposer</th><th>TXs</th><th>State Root</th></tr></thead>
        <tbody id="block-feed"></tbody>
      </table>
      <div class="footer" id="refresh-info">Refreshing every 4s...</div>
    </div>

    <div style="display:flex;flex-direction:column;gap:20px;">
      <div class="card">
        <div class="card-title">Validator Nodes</div>
        <div class="vlist" id="validator-list"></div>
      </div>
      <div class="card">
        <div class="card-title">Chain Info</div>
        <div style="font-size:.82rem;display:flex;flex-direction:column;gap:10px;">
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--muted)">Chain ID</span><span class="mono" id="i-chain">—</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--muted)">Tip Hash</span><span class="mono" id="i-tip" style="font-size:.68rem">—</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--muted)">State Root</span><span class="mono" id="i-root" style="font-size:.68rem">—</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--muted)">Last Update</span><span id="i-updated" style="color:var(--muted);font-size:.76rem">—</span></div>
        </div>
      </div>
    </div>
  </div>

  <div class="status-bar">
    <span id="sb-left">Waiting for data...</span>
    <span id="sb-right"></span>
  </div>
</div>

<script>
  // The explorer calls its own /api/* endpoints (same worker origin)
  const API = '';   // empty = same origin
  const seenHashes = new Set();
  let cycle = 0;

  function set(id, v) { const el = document.getElementById(id); if (el) el.textContent = v; }
  function flash(id) {
    const el = document.getElementById(id);
    if (!el) return;
    el.classList.remove('flash');
    void el.offsetWidth;
    el.classList.add('flash');
  }
  function timeAgo(iso) {
    if (!iso) return '—';
    const d = Math.floor((Date.now() - new Date(iso)) / 1000);
    if (d < 5)  return 'just now';
    if (d < 60) return d + 's ago';
    if (d < 3600) return Math.floor(d/60) + 'm ago';
    return Math.floor(d/3600) + 'h ago';
  }

  async function poll() {
    cycle++;
    try {
      const [statusRes, blocksRes, statsRes, mempoolRes] = await Promise.all([
        fetch(API + '/api/status'),
        fetch(API + '/api/blocks?limit=30'),
        fetch(API + '/api/stats'),
        fetch(API + '/api/mempool'),
      ]);

      const status  = statusRes.ok  ? await statusRes.json()  : null;
      const blkData = blocksRes.ok  ? await blocksRes.json()  : null;
      const stats   = statsRes.ok   ? await statsRes.json()   : null;
      const mempool = mempoolRes.ok ? await mempoolRes.json() : null;

      // KPIs
      if (status) {
        set('k-height', (status.highest_height || 0).toLocaleString());
        set('k-height-sub', '↑ via explorer API');
        flash('k-height');
        set('k-validators', status.validators_reachable + ' / ' + status.total_validators_configured);
        set('k-mempool', (status.mempool_size || 0).toLocaleString());
        set('k-proposer', status.next_proposer || '—');
        set('i-chain', status.chain_id || '—');
        set('i-tip', (status.tip_hash || '—').slice(0, 18) + '…');
        set('i-root', (status.state_root || '—').slice(0, 18) + '…');
        set('i-updated', timeAgo(status.fetched_at));
      }

      if (stats) {
        set('k-spread', stats.height_spread ?? '—');
      }

      // Validator list
      if (stats) {
        const vlist = document.getElementById('validator-list');
        const names = ['synthos-validator-11','synthos-validator-12','synthos-validator-13','synthos-validator-14','synthos-validator-15'];
        const reachable = status?.validators_reachable || 0;
        vlist.innerHTML = names.map((n, i) => \`
          <div class="vitem">
            <div class="vname">\${n}</div>
            <div class="vstatus \${i < reachable ? 'vs-ok' : 'vs-off'}">\${i < reachable ? 'Active' : 'Offline'}</div>
          </div>
        \`).join('');
      }

      // Block feed
      if (blkData?.blocks) {
        const feed = document.getElementById('block-feed');
        set('block-count', '(' + blkData.count + ' blocks)');
        for (const b of blkData.blocks) {
          if (!b.hash || seenHashes.has(b.hash)) continue;
          seenHashes.add(b.hash);
          const row = document.createElement('tr');
          row.className = 'new';
          row.innerHTML = \`
            <td class="mono">#\${(b.height||0).toLocaleString()}</td>
            <td class="mono">\${(b.hash||'').slice(0,14)}…</td>
            <td style="font-size:.75rem;color:var(--muted)">\${b.proposer||'—'}</td>
            <td style="color:var(--green);font-weight:600">\${b.tx_count||0}</td>
            <td class="mono" style="font-size:.68rem">\${(b.state_root||'').slice(0,12)}…</td>
          \`;
          feed.insertBefore(row, feed.firstChild);
          if (feed.children.length > 50) feed.removeChild(feed.lastChild);
        }
      }

      document.getElementById('api-status').innerHTML = '<span class="dot"></span> API Connected';
      document.getElementById('api-status').className = 'pill pill-sky';
      set('sb-left', 'Explorer API: healthy');
      set('sb-right', 'Cycle #' + cycle + ' — ' + new Date().toLocaleTimeString());
      document.getElementById('refresh-info').textContent = 'Last refresh: ' + new Date().toLocaleTimeString();

    } catch (e) {
      document.getElementById('api-status').innerHTML = '<span class="dot" style="background:var(--red)"></span> API Error';
      set('sb-left', 'Error: ' + e.message);
      console.error(e);
    }
  }

  poll();
  setInterval(poll, 4000);
</script>
</body>
</html>`;
}
