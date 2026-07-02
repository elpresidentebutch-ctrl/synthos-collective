(function () {
  const API = window.SYNTHOS_API_URL || "http://127.0.0.1:8090";
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
    document.querySelectorAll(selector).forEach((node) => {
      node.textContent = value == null || value === "" ? "-" : String(value);
    });
  }

  function shortHash(value) {
    if (!value) return "-";
    return value.length > 20 ? `${value.slice(0, 18)}...` : value;
  }

  function heartbeatLabel(proof) {
    if (proof.heartbeat_fresh) return "fresh";
    if (!proof.heartbeat_age_ms) return "unknown";
    const hours = Math.floor(proof.heartbeat_age_ms / 3600000);
    if (hours < 48) return `${hours}h stale`;
    return `${Math.floor(hours / 24)}d stale`;
  }

  function renderValidators(validators) {
    document.querySelectorAll(selectors.validators).forEach((target) => {
      target.innerHTML = validators.map((validator) => `
        <div class="synthos-validator-row">
          <strong>${validator.name}</strong>
          <span>${validator.reachable ? "online" : "offline"}</span>
          <span>height ${validator.status?.height ?? "-"}</span>
          <span>${heartbeatLabel(validator)}</span>
        </div>
      `).join("");
    });
  }

  async function loadStatus() {
    try {
      const response = await fetch(`${API}/api/network/status`, { cache: "no-store" });
      if (!response.ok) throw new Error(`status ${response.status}`);
      const data = await response.json();
      const verdict = data.ok
        ? "Live testnet healthy"
        : data.majority_reachable && data.converged_tip && data.converged_state_root
          ? "Converged, heartbeat repair needed"
          : "Network attention needed";

      text(selectors.verdict, verdict);
      text(selectors.reachable, `${data.reachable}/${data.total}`);
      text(selectors.height, data.highest_height);
      text(selectors.tip, shortHash(data.tip));
      text(selectors.root, shortHash(data.state_root));
      text(selectors.proposer, data.next_proposer);
      text(selectors.fresh, `${data.fresh_heartbeats}/${data.total}`);
      renderValidators(data.validators || []);
    } catch (error) {
      text(selectors.verdict, "Backend unavailable");
      text(selectors.reachable, "-");
    }
  }

  document.querySelectorAll(selectors.install).forEach((link) => {
    link.href = `${API}/api/node/windows-installer.ps1`;
    link.setAttribute("download", "install-synthos-node.ps1");
  });

  loadStatus();
  setInterval(loadStatus, 60000);
})();
