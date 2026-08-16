const API = window.SYNTHOS_API_URL || window.location.origin;
const heartbeatMaxAgeMs = 2 * 60 * 1000;

const el = (id) => document.getElementById(id);

async function fetchJSON(url, timeoutMs = 8000) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetch(url, { signal: controller.signal, cache: "no-store" });
    if (!response.ok) throw new Error(`${response.status} ${response.statusText}`);
    return await response.json();
  } finally {
    clearTimeout(timeout);
  }
}

function html(value) {
  return String(value ?? "").replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    "\"": "&quot;",
    "'": "&#39;",
  })[char]);
}

function shortHash(value) {
  if (!value || value === "-") return "-";
  return String(value).length > 20 ? `${String(value).slice(0, 18)}...` : String(value);
}

function ageMsFromNode(node) {
  const raw = node.last_seen || node.lastSeen || 0;
  if (typeof raw === "number" && raw > 0) return Date.now() - raw;
  const date = Date.parse(node.last_heartbeat_at || node.lastHeartbeatAt || "");
  return Number.isFinite(date) ? Date.now() - date : Infinity;
}

function nodeClass(node) {
  if (node.stale) return "down";
  if (ageMsFromNode(node) > heartbeatMaxAgeMs) return "warn";
  return "ok";
}

function nodeHeight(node) {
  return node.height || node.Height || 0;
}

function renderTopology(nodes) {
  el("topology").innerHTML = nodes.map((node) => `
    <div class="node-dot ${nodeClass(node)}">
      <strong>${html(node.name || node.node_id || "node")}</strong>
      <span>${node.stale ? "stale" : `h${html(nodeHeight(node))}`}</span>
      <span>${node.real_signed_heartbeat ? "real signed heartbeat" : node.proof_status || node.status || "registered"}</span>
    </div>
  `).join("") || `<p class="muted">No nodes registered yet.</p>`;
}

function renderValidators(nodes) {
  el("validatorList").innerHTML = nodes.map((node) => `
    <article class="validator-card">
      <strong>${html(node.name || node.node_id || "node")}</strong>
      <dl>
        <div><dt>Status</dt><dd>${html(node.proof_status || node.status || (node.stale ? "stale" : "running"))}</dd></div>
        <div><dt>Height</dt><dd>${html(nodeHeight(node))}</dd></div>
        <div><dt>Heartbeats</dt><dd>${html(node.valid_heartbeats || 0)}</dd></div>
        <div><dt>Signature</dt><dd>${node.real_signed_heartbeat ? "Ed25519" : "waiting"}</dd></div>
      </dl>
    </article>
  `).join("") || `<p class="muted">Install a validator service to appear here.</p>`;
}

function renderSummary(status) {
  const nodes = Array.isArray(status.validators) ? status.validators : [];
  const active = nodes.filter((node) => !node.stale);
  const fresh = active.filter((node) => ageMsFromNode(node) <= heartbeatMaxAgeMs);
  const required = Math.ceil((Math.max(status.registered_total || nodes.length, 1) * 2) / 3);
  const majority = Boolean(status.majority_reachable) || active.length >= required;

  el("networkVerdict").textContent = status.ok
    ? active.length > 0 ? "SYNTHOS network live" : "Backend live, waiting for nodes"
    : "Network attention needed";
  el("reachableCount").textContent = `${status.reachable ?? active.length}/${status.registered_total ?? nodes.length}`;
  el("chainHeight").textContent = status.highest_height ?? 0;
  el("freshCount").textContent = `${status.fresh_heartbeats ?? fresh.length}/${status.total ?? active.length}`;
  el("majorityState").textContent = majority ? "yes" : "no";
  el("tipHash").textContent = shortHash(status.tip || nodes[0]?.tip || "-");
  el("stateRoot").textContent = shortHash(status.state_root || nodes[0]?.state_root || "-");
  el("nextProposer").textContent = status.next_proposer || "-";
  el("updatedAt").textContent = `Updated ${new Date().toLocaleString()}`;

  renderTopology(nodes);
  renderValidators(nodes);
}

async function refreshStatus() {
  el("networkVerdict").textContent = "Checking network";
  try {
    const status = await fetchJSON(`${API}/api/network/status`);
    renderSummary(status);
  } catch (error) {
    el("networkVerdict").textContent = "Backend unavailable";
    el("reachableCount").textContent = "0/0";
    el("chainHeight").textContent = "-";
    el("freshCount").textContent = "0/0";
    el("majorityState").textContent = "no";
    el("topology").innerHTML = `<p class="muted">${html(error.message)}</p>`;
    el("validatorList").innerHTML = "";
  }
}

el("refreshButton")?.addEventListener("click", refreshStatus);
refreshStatus();
setInterval(refreshStatus, 15000);
