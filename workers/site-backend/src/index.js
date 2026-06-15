const DEFAULT_VALIDATORS = [
  { name: "synthos-validator-11", url: "https://synthos-validator-11.jamesishamwilliams.workers.dev", role: "validator", binding: "VALIDATOR_11" },
  { name: "synthos-validator-12", url: "https://synthos-validator-12.jamesishamwilliams.workers.dev", role: "validator", binding: "VALIDATOR_12" },
  { name: "synthos-validator-13", url: "https://synthos-validator-13.jamesishamwilliams.workers.dev", role: "validator", binding: "VALIDATOR_13" },
  { name: "synthos-validator-14", url: "https://synthos-validator-14.jamesishamwilliams.workers.dev", role: "validator", binding: "VALIDATOR_14" },
  { name: "synthos-validator-15", url: "https://synthos-validator-15.jamesishamwilliams.workers.dev", role: "validator", binding: "VALIDATOR_15" },
];

const DEFAULT_REGISTRY = "https://synthos-peer-registry.jamesishamwilliams.workers.dev";
const HEARTBEAT_MAX_AGE_MS = 60 * 60 * 1000;
const FETCH_TIMEOUT_MS = 5000;

function corsHeaders(origin = "*") {
  return {
    "Access-Control-Allow-Origin": origin || "*",
    "Access-Control-Allow-Methods": "GET, POST, OPTIONS",
    "Access-Control-Allow-Headers": "Content-Type, Authorization",
    "Access-Control-Max-Age": "86400",
  };
}

function json(data, status = 200, origin = "*") {
  return new Response(JSON.stringify(data, null, 2), {
    status,
    headers: {
      "Content-Type": "application/json; charset=utf-8",
      "Cache-Control": "no-store",
      ...corsHeaders(origin),
    },
  });
}

function text(body, status = 200, contentType = "text/plain; charset=utf-8", origin = "*") {
  return new Response(body, {
    status,
    headers: {
      "Content-Type": contentType,
      "Cache-Control": "no-store",
      ...corsHeaders(origin),
    },
  });
}

function validatorsFromEnv(env) {
  if (!env.VALIDATORS_JSON) return DEFAULT_VALIDATORS;
  try {
    const parsed = JSON.parse(env.VALIDATORS_JSON);
    if (Array.isArray(parsed) && parsed.length > 0) return parsed;
  } catch (_) {
    // Fall back to defaults.
  }
  return DEFAULT_VALIDATORS;
}

async function fetchJSON(url, timeoutMs = FETCH_TIMEOUT_MS, serviceBinding = null) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort("timeout"), timeoutMs);
  try {
    const request = new Request(url, {
      signal: controller.signal,
      headers: {
        Accept: "application/json",
        "User-Agent": "synthos-site-backend/1.0",
      },
    });
    const response = serviceBinding ? await serviceBinding.fetch(request) : await fetch(request);
    const body = await response.text();
    if (!response.ok) {
      throw new Error(`status=${response.status} body=${body.slice(0, 180)}`);
    }
    return body ? JSON.parse(body) : null;
  } finally {
    clearTimeout(timeout);
  }
}

async function checkValidator(validator, env) {
  const started = Date.now();
  const proof = {
    name: validator.name,
    url: validator.url,
    role: validator.role || "validator",
    reachable: false,
    converged: false,
    heartbeat_fresh: false,
    latency_ms: 0,
  };

  try {
    const base = validator.url.replace(/\/+$/, "");
    const serviceBinding = validator.binding ? env[validator.binding] : null;
    const [health, status, heartbeat, peers, blocks] = await Promise.all([
      fetchJSON(`${base}/health`, FETCH_TIMEOUT_MS, serviceBinding),
      fetchJSON(`${base}/status`, FETCH_TIMEOUT_MS, serviceBinding),
      fetchJSON(`${base}/heartbeat`, FETCH_TIMEOUT_MS, serviceBinding).catch(() => null),
      fetchJSON(`${base}/peers`, FETCH_TIMEOUT_MS, serviceBinding).catch(() => null),
      fetchJSON(`${base}/blocks?from=0`, FETCH_TIMEOUT_MS, serviceBinding).catch(() => null),
    ]);

    proof.reachable = true;
    proof.health = health;
    proof.status = status;
    proof.heartbeat = heartbeat;
    proof.peers = peers;
    proof.block_count = blocks?.count || 0;

    if (heartbeat?.last_check) {
      const age = Date.now() - Date.parse(heartbeat.last_check);
      if (Number.isFinite(age)) {
        proof.heartbeat_age_ms = age;
        proof.heartbeat_fresh = age <= HEARTBEAT_MAX_AGE_MS;
      }
    }
  } catch (error) {
    proof.error = error?.message || String(error);
  }

  proof.latency_ms = Date.now() - started;
  return proof;
}

async function networkStatus(env) {
  const validators = validatorsFromEnv(env);
  const proofs = await Promise.all(validators.map((validator) => checkValidator(validator, env)));
  const reachable = proofs.filter((proof) => proof.reachable && proof.status);
  const first = reachable[0]?.status || null;
  const highest = reachable.reduce((height, proof) => Math.max(height, proof.status?.height || 0), 0);
  const majorityRequirement = Math.ceil((validators.length * 2) / 3);

  let convergedHeight = reachable.length > 0;
  let convergedTip = reachable.length > 0;
  let convergedStateRoot = reachable.length > 0;

  for (const proof of proofs) {
    if (!proof.reachable || !proof.status || !first) continue;
    proof.converged = proof.status.height === first.height &&
      proof.status.tip === first.tip &&
      proof.status.state_root === first.state_root;
    if (proof.status.height !== first.height) convergedHeight = false;
    if (proof.status.tip !== first.tip) convergedTip = false;
    if (proof.status.state_root !== first.state_root) convergedStateRoot = false;
  }

  const freshHeartbeats = proofs.filter((proof) => proof.heartbeat_fresh).length;
  const majorityReachable = reachable.length >= majorityRequirement;

  return {
    ok: reachable.length === validators.length &&
      freshHeartbeats === validators.length &&
      convergedHeight &&
      convergedTip &&
      convergedStateRoot,
    mode: "b12-site-backend",
    chain_id: first?.chain_id || env.CHAIN_ID || "synthos-l1-devnet",
    reachable: reachable.length,
    total: validators.length,
    highest_height: highest,
    tip: first?.tip || null,
    state_root: first?.state_root || null,
    next_proposer: first?.next_proposer || null,
    converged_height: convergedHeight,
    converged_tip: convergedTip,
    converged_state_root: convergedStateRoot,
    majority_requirement: majorityRequirement,
    majority_reachable: majorityReachable,
    fresh_heartbeats: freshHeartbeats,
    fetched_at: new Date().toISOString(),
    validators: proofs,
  };
}

async function bestValidator(env) {
  const status = await networkStatus(env);
  const reachable = status.validators.filter((validator) => validator.reachable && validator.status);
  if (reachable.length === 0) return null;
  return reachable.reduce((best, next) =>
    (next.status.height || 0) > (best.status.height || 0) ? next : best
  );
}

async function proxyBestValidator(env, path) {
  const best = await bestValidator(env);
  if (!best) return json({ ok: false, error: "no validators reachable" }, 503);
  const data = await fetchJSON(`${best.url.replace(/\/+$/, "")}${path}`);
  return json({ ok: true, source: best.name, data });
}

function installerScript(env) {
  const relayUrls = env.RELAY_URLS || DEFAULT_REGISTRY;
  return `$ErrorActionPreference = "Stop"

Write-Host "SYNTHOS Background Node"
Write-Host "Download or clone the SYNTHOS Collective repository, then run this from the repository root."

$env:SYNTHOS_RELAY_URLS = "${relayUrls}"
$script = Join-Path (Get-Location) "scripts\\install_background_node.ps1"
if (!(Test-Path $script)) {
  throw "scripts\\install_background_node.ps1 was not found. Run this from the SYNTHOS Collective repository root."
}
& $script -RelayUrls "${relayUrls}"
`;
}

function b12Snippet() {
  return `(function () {
  const API = window.SYNTHOS_API_URL || "https://synthos-site-backend.jamesishamwilliams.workers.dev";
  const selectors = {
    verdict: "[data-synthos='network-verdict']",
    reachable: "[data-synthos='validators-reachable']",
    height: "[data-synthos='chain-height']",
    tip: "[data-synthos='tip']",
    root: "[data-synthos='state-root']",
    proposer: "[data-synthos='next-proposer']",
    fresh: "[data-synthos='fresh-heartbeats']",
    validators: "[data-synthos='validator-list']",
    install: "[data-synthos='windows-installer']",
  };
  function text(selector, value) {
    document.querySelectorAll(selector).forEach((node) => { node.textContent = value == null || value === "" ? "-" : String(value); });
  }
  function shortHash(value) { return !value ? "-" : value.length > 20 ? value.slice(0, 18) + "..." : value; }
  function heartbeatLabel(proof) {
    if (proof.heartbeat_fresh) return "fresh";
    if (!proof.heartbeat_age_ms) return "unknown";
    const hours = Math.floor(proof.heartbeat_age_ms / 3600000);
    return hours < 48 ? hours + "h stale" : Math.floor(hours / 24) + "d stale";
  }
  function renderValidators(validators) {
    document.querySelectorAll(selectors.validators).forEach((target) => {
      target.innerHTML = validators.map((validator) => "<div class='synthos-validator-row'><strong>" + validator.name + "</strong><span>" + (validator.reachable ? "online" : "offline") + "</span><span>height " + (validator.status?.height ?? "-") + "</span><span>" + heartbeatLabel(validator) + "</span></div>").join("");
    });
  }
  async function loadStatus() {
    try {
      const response = await fetch(API + "/api/network/status", { cache: "no-store" });
      if (!response.ok) throw new Error("status " + response.status);
      const data = await response.json();
      const verdict = data.ok ? "Live testnet healthy" : data.majority_reachable && data.converged_tip && data.converged_state_root ? "Converged, heartbeat repair needed" : "Network attention needed";
      text(selectors.verdict, verdict);
      text(selectors.reachable, data.reachable + "/" + data.total);
      text(selectors.height, data.highest_height);
      text(selectors.tip, shortHash(data.tip));
      text(selectors.root, shortHash(data.state_root));
      text(selectors.proposer, data.next_proposer);
      text(selectors.fresh, data.fresh_heartbeats + "/" + data.total);
      renderValidators(data.validators || []);
    } catch (_) {
      text(selectors.verdict, "Backend unavailable");
      text(selectors.reachable, "-");
    }
  }
  document.querySelectorAll(selectors.install).forEach((link) => {
    link.href = API + "/api/node/windows-installer.ps1";
    link.setAttribute("download", "install-synthos-node.ps1");
  });
  loadStatus();
  setInterval(loadStatus, 60000);
})();`;
}

async function handleContact(request, env) {
  let body;
  try {
    body = await request.json();
  } catch (_) {
    return json({ ok: false, error: "invalid json" }, 400);
  }

  const payload = {
    ok: true,
    received_at: new Date().toISOString(),
    name: String(body.name || "").slice(0, 160),
    email: String(body.email || "").slice(0, 220),
    message: String(body.message || "").slice(0, 4000),
    source: "synthos-b12-site",
  };

  if (env.CONTACT_WEBHOOK_URL) {
    await fetch(env.CONTACT_WEBHOOK_URL, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
  }

  return json({ ok: true, received_at: payload.received_at });
}

function siteConfig(env) {
  return {
    ok: true,
    project: "SYNTHOS Collective",
    chain_id: env.CHAIN_ID || "synthos-l1-devnet",
    relay_url: env.RELAY_URLS || DEFAULT_REGISTRY,
    endpoints: [
      "/api/health",
      "/api/network/status",
      "/api/validators",
      "/api/validators/:name",
      "/api/blocks",
      "/api/dex/pools",
      "/api/node/windows-installer.ps1",
      "/api/contact",
      "/snippet.js",
    ],
  };
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const origin = request.headers.get("Origin") || "*";

    if (request.method === "OPTIONS") {
      return new Response(null, { status: 204, headers: corsHeaders(origin) });
    }

    try {
      if (url.pathname === "/" || url.pathname === "/api" || url.pathname === "/api/health") {
        return json({
          ok: true,
          service: "synthos-site-backend",
          version: "1.0.0",
          fetched_at: new Date().toISOString(),
          config: siteConfig(env),
        }, 200, origin);
      }

      if (url.pathname === "/api/config") {
        return json(siteConfig(env), 200, origin);
      }

      if (url.pathname === "/api/network/status") {
        return json(await networkStatus(env), 200, origin);
      }

      if (url.pathname === "/api/validators") {
        const status = await networkStatus(env);
        return json({ ok: true, validators: status.validators, fetched_at: status.fetched_at }, 200, origin);
      }

      if (url.pathname.startsWith("/api/validators/")) {
        const name = decodeURIComponent(url.pathname.replace("/api/validators/", ""));
        const status = await networkStatus(env);
        const validator = status.validators.find((item) => item.name === name);
        if (!validator) return json({ ok: false, error: "validator not found" }, 404, origin);
        return json({ ok: true, validator, fetched_at: status.fetched_at }, 200, origin);
      }

      if (url.pathname === "/api/blocks") {
        const from = Number.parseInt(url.searchParams.get("from") || "0", 10);
        return proxyBestValidator(env, `/blocks?from=${Number.isFinite(from) && from >= 0 ? from : 0}`);
      }

      if (url.pathname === "/api/dex/pools") {
        return proxyBestValidator(env, "/dex/pools");
      }

      if (url.pathname === "/api/node/windows-installer.ps1") {
        return text(installerScript(env), 200, "text/plain; charset=utf-8", origin);
      }

      if (url.pathname === "/snippet.js") {
        return text(b12Snippet(), 200, "text/javascript; charset=utf-8", origin);
      }

      if (url.pathname === "/api/contact" && request.method === "POST") {
        return handleContact(request, env);
      }

      return json({ ok: false, error: "not found", config: siteConfig(env) }, 404, origin);
    } catch (error) {
      return json({ ok: false, error: error?.message || String(error) }, 500, origin);
    }
  },
};
