const publicRpcUrl = window.SYNTHOS_RPC_URL || "https://rpc.ishamwilliamsblockchains.com";
const validators = window.SYNTHOS_VALIDATORS || [
  { name: "synthos-public-rpc", url: publicRpcUrl },
];

const heartbeatMaxAgeMs = 60 * 60 * 1000;

const el = (id) => document.getElementById(id);

async function fetchJSON(url, timeoutMs = 5000) {
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

async function checkValidator(validator) {
  const started = performance.now();
  const proof = {
    ...validator,
    reachable: false,
    converged: false,
    fresh: false,
    latency: 0,
  };
  try {
    const [health, status, heartbeat, peers, blocks, mempool] = await Promise.all([
      fetchJSON(`${validator.url}/health`),
      fetchJSON(`${validator.url}/status`),
      fetchJSON(`${validator.url}/heartbeat`).catch(() => null),
      fetchJSON(`${validator.url}/peers`).catch(() => null),
      fetchJSON(`${validator.url}/blocks?from=0`).catch(() => null),
      fetchJSON(`${validator.url}/mempool`).catch(() => null),
    ]);
    proof.reachable = true;
    proof.health = health;
    proof.status = status;
    proof.heartbeat = heartbeat;
    proof.peers = peers;
    proof.blockCount = blocks?.count || 0;
    proof.mempoolSize = mempool?.size ?? mempool?.tx?.length ?? status?.mempool ?? 0;
    if (heartbeat?.last_check) {
      const age = Date.now() - Date.parse(heartbeat.last_check);
      proof.heartbeatAge = Number.isFinite(age) ? age : null;
      proof.fresh = Number.isFinite(age) && age <= heartbeatMaxAgeMs;
    }
  } catch (error) {
    proof.error = error.name === "AbortError" ? "timeout" : error.message;
  }
  proof.latency = Math.round(performance.now() - started);
  return proof;
}

function shortHash(value) {
  if (!value || value === "-") return "-";
  return value.length > 20 ? `${value.slice(0, 18)}...` : value;
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

function ageLabel(ms) {
  if (!Number.isFinite(ms)) return "-";
  const minutes = Math.floor(ms / 60000);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function nodeClass(proof) {
  if (!proof.reachable) return "down";
  if (!proof.fresh) return "warn";
  return "ok";
}

function renderTopology(proofs) {
  el("topology").innerHTML = proofs.map((proof) => `
    <div class="node-dot ${nodeClass(proof)}">
      <strong>${html(proof.name)}</strong>
      <span>${proof.reachable ? `h${html(proof.status.height)} | ${html(proof.latency)}ms` : html(proof.error)}</span>
      <span>${proof.heartbeat ? (proof.fresh ? "fresh heartbeat" : "heartbeat stale") : "RPC status source"}</span>
    </div>
  `).join("");
}

function renderValidators(proofs) {
  el("validatorList").innerHTML = proofs.map((proof) => `
    <article class="validator-card">
      <strong>${html(proof.name)}</strong>
      <dl>
        <div><dt>Status</dt><dd>${proof.reachable ? "online" : "offline"}</dd></div>
        <div><dt>Height</dt><dd>${html(proof.status?.height ?? "-")}</dd></div>
        <div><dt>Tip</dt><dd>${html(shortHash(proof.status?.tip))}</dd></div>
        <div><dt>Mempool</dt><dd>${html(proof.mempoolSize ?? "-")}</dd></div>
      </dl>
    </article>
  `).join("");
}

function renderSummary(proofs) {
  const reachable = proofs.filter((proof) => proof.reachable);
  const fresh = proofs.filter((proof) => proof.fresh);
  const first = reachable[0]?.status;
  const converged = reachable.length > 0 && reachable.every((proof) =>
    proof.status.height === first.height &&
    proof.status.tip === first.tip &&
    proof.status.state_root === first.state_root
  );
  const required = Math.ceil((validators.length * 2) / 3);
  const majority = reachable.length >= required;
  const allLive = reachable.length === validators.length && converged;

  el("networkVerdict").textContent = allLive
    ? "Live RPC healthy"
    : majority && converged
      ? "RPC reachable"
      : "Network attention needed";
  el("reachableCount").textContent = `${reachable.length}/${validators.length}`;
  el("chainHeight").textContent = first?.height ?? "-";
  el("freshCount").textContent = `${fresh.length}/${validators.length}`;
  el("majorityState").textContent = majority ? "yes" : "no";
  el("tipHash").textContent = shortHash(first?.tip);
  el("stateRoot").textContent = shortHash(first?.state_root);
  el("nextProposer").textContent = first?.next_proposer || "-";
  el("updatedAt").textContent = `Updated ${new Date().toLocaleString()}`;

  renderTopology(proofs.map((proof) => ({
    ...proof,
    converged: proof.reachable && first &&
      proof.status.height === first.height &&
      proof.status.tip === first.tip &&
      proof.status.state_root === first.state_root,
  })));
  renderValidators(proofs);
}

async function refreshStatus() {
  el("networkVerdict").textContent = "Checking network";
  const proofs = await Promise.all(validators.map(checkValidator));
  renderSummary(proofs);
}

el("refreshButton").addEventListener("click", refreshStatus);
refreshStatus();
setInterval(refreshStatus, 60000);
