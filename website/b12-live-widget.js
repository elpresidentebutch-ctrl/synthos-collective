(function () {
  "use strict";

  const API = window.SYNTHOS_API_URL || "https://synthos-site-backend.jamesishamwilliams.workers.dev";
  const ID = "synthos-live-network-widget";
  const REFRESH_MS = 15000;

  function escapeHtml(value) {
    return String(value ?? "").replace(/[&<>'"]/g, (char) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;",
    })[char]);
  }

  function shortHash(value) {
    if (!value) return "-";
    return value.length > 18 ? `${value.slice(0, 16)}…` : value;
  }

  function ensureWidget() {
    let root = document.getElementById(ID);
    if (root) return root;

    const style = document.createElement("style");
    style.textContent = `
      #${ID}{position:fixed;right:18px;bottom:18px;z-index:2147483000;width:min(390px,calc(100vw - 28px));color:#f6fbff;background:#081019;border:1px solid rgba(83,229,175,.45);border-radius:14px;box-shadow:0 20px 60px rgba(0,0,0,.45);font:14px/1.4 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;overflow:hidden}
      #${ID} *{box-sizing:border-box} #${ID} button{font:inherit}
      #${ID} .sw-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:13px 15px;background:linear-gradient(90deg,rgba(35,202,141,.16),rgba(32,147,211,.08));border-bottom:1px solid rgba(255,255,255,.1)}
      #${ID} .sw-title{display:flex;align-items:center;gap:9px;font-weight:800} #${ID} .sw-dot{width:10px;height:10px;border-radius:50%;background:#f2c45d;box-shadow:0 0 12px currentColor}
      #${ID}[data-state="ok"] .sw-dot{background:#35d59f} #${ID}[data-state="down"] .sw-dot{background:#ff6b6b}
      #${ID} .sw-close{border:0;background:transparent;color:#b8c7d6;cursor:pointer;padding:3px 6px;font-size:18px}
      #${ID} .sw-body{padding:14px 15px} #${ID} .sw-verdict{font-size:17px;font-weight:850;margin-bottom:11px}
      #${ID} .sw-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-bottom:11px} #${ID} .sw-metric{padding:9px;background:rgba(255,255,255,.055);border:1px solid rgba(255,255,255,.08);border-radius:9px}
      #${ID} .sw-metric strong{display:block;font-size:17px} #${ID} .sw-metric span{color:#94a9bc;font-size:11px;text-transform:uppercase;letter-spacing:.04em}
      #${ID} .sw-facts{display:grid;grid-template-columns:84px 1fr;gap:5px 10px;color:#a9bac9;font-size:12px} #${ID} .sw-facts b{color:#eef7ff;font-family:ui-monospace,SFMono-Regular,Consolas,monospace;overflow:hidden;text-overflow:ellipsis}
      #${ID} .sw-foot{display:flex;justify-content:space-between;gap:10px;margin-top:11px;color:#7890a4;font-size:11px} #${ID} .sw-foot a{color:#53e5af;text-decoration:none}
      @media(max-width:520px){#${ID}{right:14px;bottom:14px}.sw-grid{grid-template-columns:repeat(3,1fr)}}
    `;
    document.head.appendChild(style);

    root = document.createElement("aside");
    root.id = ID;
    root.setAttribute("role", "status");
    root.setAttribute("aria-live", "polite");
    root.dataset.state = "loading";
    root.innerHTML = `
      <div class="sw-head"><div class="sw-title"><span class="sw-dot"></span><span>SYNTHOS live testnet</span></div><button class="sw-close" aria-label="Close network status">×</button></div>
      <div class="sw-body"><div class="sw-verdict">Connecting to backend…</div><div class="sw-grid"></div><div class="sw-facts"></div><div class="sw-foot"><span class="sw-updated">Waiting for status</span><a href="/network-status">Network details</a></div></div>`;
    root.querySelector(".sw-close").addEventListener("click", () => {
      root.remove();
      sessionStorage.setItem("synthos-widget-closed", "1");
    });
    document.body.appendChild(root);
    return root;
  }

  function render(data) {
    const root = ensureWidget();
    const converged = Boolean(data.converged_tip && data.converged_state_root);
    const healthy = Boolean(data.ok);
    root.dataset.state = healthy ? "ok" : data.reachable > 0 ? "warn" : "down";
    root.querySelector(".sw-verdict").textContent = healthy
      ? "Network healthy and converged"
      : data.majority_reachable && converged
        ? "Network converged; heartbeat repair needed"
        : data.reachable > 0
          ? "Testnet requires attention"
          : "Validator backend unavailable";
    root.querySelector(".sw-grid").innerHTML = `
      <div class="sw-metric"><strong>${escapeHtml(data.reachable)}/${escapeHtml(data.total)}</strong><span>Reachable</span></div>
      <div class="sw-metric"><strong>${escapeHtml(data.highest_height ?? "-")}</strong><span>Height</span></div>
      <div class="sw-metric"><strong>${escapeHtml(data.fresh_heartbeats)}/${escapeHtml(data.total)}</strong><span>Fresh</span></div>`;
    root.querySelector(".sw-facts").innerHTML = `
      <span>Chain</span><b>${escapeHtml(data.chain_id || "-")}</b>
      <span>Tip</span><b title="${escapeHtml(data.tip || "")}">${escapeHtml(shortHash(data.tip))}</b>
      <span>State root</span><b title="${escapeHtml(data.state_root || "")}">${escapeHtml(shortHash(data.state_root))}</b>
      <span>Proposer</span><b>${escapeHtml(data.next_proposer || "-")}</b>`;
    root.querySelector(".sw-updated").textContent = `Updated ${new Date().toLocaleTimeString()}`;
  }

  function renderError() {
    const root = ensureWidget();
    root.dataset.state = "down";
    root.querySelector(".sw-verdict").textContent = "Live backend unavailable";
    root.querySelector(".sw-grid").innerHTML = "";
    root.querySelector(".sw-facts").innerHTML = `<span>Backend</span><b>${escapeHtml(API)}</b>`;
    root.querySelector(".sw-updated").textContent = `Retrying every ${REFRESH_MS / 1000}s`;
  }

  async function refresh() {
    if (!document.body || sessionStorage.getItem("synthos-widget-closed") === "1") return;
    try {
      const response = await fetch(`${API}/api/network/status`, { cache: "no-store", headers: { Accept: "application/json" } });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      render(await response.json());
    } catch (_) {
      renderError();
    }
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", refresh, { once: true });
  else refresh();
  setInterval(refresh, REFRESH_MS);
})();
