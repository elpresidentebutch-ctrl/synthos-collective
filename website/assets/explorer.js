const DEFAULT_RPC_URL = window.SYNTHOS_RPC_URL || "https://rpc.ishamwilliamsblockchains.com";
const requestTimeoutMs = 9000;

const state = {
  blocks: [],
  mempool: [],
  selectedHash: "",
  status: null,
};

const els = {
  rpcUrl: document.getElementById("rpcUrl"),
  fromHeight: document.getElementById("fromHeight"),
  refresh: document.getElementById("explorerRefresh"),
  search: document.getElementById("explorerSearch"),
  chainId: document.getElementById("explorerChainId"),
  height: document.getElementById("explorerHeight"),
  mempoolCount: document.getElementById("explorerMempool"),
  finalized: document.getElementById("explorerFinalized"),
  blocksTitle: document.getElementById("blocksTitle"),
  blockList: document.getElementById("blockList"),
  detailTitle: document.getElementById("detailTitle"),
  detailHash: document.getElementById("detailHash"),
  detailParent: document.getElementById("detailParent"),
  detailTime: document.getElementById("detailTime"),
  detailProposer: document.getElementById("detailProposer"),
  detailState: document.getElementById("detailState"),
  txList: document.getElementById("txList"),
  mempoolList: document.getElementById("mempoolList"),
  updated: document.getElementById("explorerUpdated"),
};

els.rpcUrl.value = DEFAULT_RPC_URL;

function cleanRpcUrl() {
  return els.rpcUrl.value.trim().replace(/\/+$/, "");
}

async function fetchJSON(path) {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), requestTimeoutMs);
  try {
    const response = await fetch(`${cleanRpcUrl()}${path}`, {
      cache: "no-store",
      signal: controller.signal,
    });
    if (!response.ok) throw new Error(`${response.status} ${response.statusText}`);
    return await response.json();
  } finally {
    window.clearTimeout(timer);
  }
}

function short(value, size = 10) {
  if (!value) return "-";
  const text = String(value);
  if (text.length <= size * 2 + 3) return text;
  return `${text.slice(0, size)}...${text.slice(-size)}`;
}

function formatAmount(value) {
  if (value === undefined || value === null || value === "") return "-";
  return Number(value).toLocaleString(undefined, { maximumFractionDigits: 8 });
}

function formatTime(value) {
  if (!value) return "-";
  const time = new Date(value);
  return Number.isNaN(time.getTime()) ? String(value) : time.toLocaleString();
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

function txMatches(tx, term) {
  const haystack = [
    tx.id,
    tx.from,
    tx.to,
    tx.asset_id,
    tx.chain_id,
    tx.metadata,
  ].filter(Boolean).join(" ").toLowerCase();
  return haystack.includes(term);
}

function blockMatches(block, term) {
  if (!term) return true;
  const header = block.header || {};
  const blockText = [
    block.hash,
    header.parent_hash,
    header.height,
    header.proposer_id,
    header.state_root,
  ].filter(Boolean).join(" ").toLowerCase();
  return blockText.includes(term) || (block.tx || []).some((tx) => txMatches(tx, term));
}

function renderMetrics() {
  const status = state.status || {};
  const latest = state.blocks[0];
  els.chainId.textContent = status.chain_id || "-";
  els.height.textContent = status.height ?? "-";
  els.mempoolCount.textContent = state.mempool.length || status.mempool || 0;
  els.finalized.textContent = latest?.finalized ? "yes" : latest ? "pending" : "-";
}

function renderBlocks() {
  const term = els.search.value.trim().toLowerCase();
  const blocks = state.blocks.filter((block) => blockMatches(block, term));
  els.blocksTitle.textContent = blocks.length ? `${blocks.length} block${blocks.length === 1 ? "" : "s"}` : "No blocks found";
  els.blockList.innerHTML = blocks.map((block) => {
    const header = block.header || {};
    const txCount = (block.tx || []).length;
    const active = block.hash === state.selectedHash ? " active" : "";
    return `
      <button class="block-row${active}" type="button" data-hash="${html(block.hash || "")}">
        <span>
          <strong>Height ${html(header.height ?? "-")}</strong>
          <small>${html(short(block.hash, 12))}</small>
        </span>
        <span>
          <strong>${html(txCount)}</strong>
          <small>tx</small>
        </span>
        <span>
          <strong>${block.finalized ? "Finalized" : "Pending"}</strong>
          <small>${html(formatTime(header.timestamp))}</small>
        </span>
      </button>
    `;
  }).join("") || `<p class="empty-state">Explorer waiting for matching blocks.</p>`;
}

function renderTxList(container, txs) {
  container.innerHTML = txs.map((tx) => `
    <article class="tx-row">
      <div>
        <strong>${html(short(tx.id, 14))}</strong>
        <small>${html(tx.asset_id || "SYN")} transfer</small>
      </div>
      <dl>
        <div><dt>From</dt><dd>${html(short(tx.from, 12))}</dd></div>
        <div><dt>To</dt><dd>${html(short(tx.to, 12))}</dd></div>
        <div><dt>Amount</dt><dd>${html(formatAmount(tx.amount))}</dd></div>
        <div><dt>Fee</dt><dd>${html(formatAmount(tx.fee))}</dd></div>
        <div><dt>Nonce</dt><dd>${html(tx.nonce ?? "-")}</dd></div>
      </dl>
    </article>
  `).join("") || `<p class="empty-state">No transactions to show.</p>`;
}

function selectBlock(hash) {
  const selected = state.blocks.find((block) => block.hash === hash) || state.blocks[0];
  if (!selected) {
    state.selectedHash = "";
    els.detailTitle.textContent = "No block selected";
    els.detailHash.textContent = "-";
    els.detailParent.textContent = "-";
    els.detailTime.textContent = "-";
    els.detailProposer.textContent = "-";
    els.detailState.textContent = "-";
    renderTxList(els.txList, []);
    renderBlocks();
    return;
  }

  const header = selected.header || {};
  state.selectedHash = selected.hash;
  els.detailTitle.textContent = `Height ${header.height ?? "-"}`;
  els.detailHash.textContent = selected.hash || "-";
  els.detailParent.textContent = header.parent_hash || "-";
  els.detailTime.textContent = formatTime(header.timestamp);
  els.detailProposer.textContent = header.proposer_id || "-";
  els.detailState.textContent = header.state_root || "-";
  renderTxList(els.txList, selected.tx || []);
  renderBlocks();
}

function renderMempool() {
  const term = els.search.value.trim().toLowerCase();
  const txs = term ? state.mempool.filter((tx) => txMatches(tx, term)) : state.mempool;
  renderTxList(els.mempoolList, txs);
}

function normalizeBlocks(payload) {
  const blocks = Array.isArray(payload?.blocks) ? payload.blocks : [];
  return blocks.sort((a, b) => Number(b.header?.height || 0) - Number(a.header?.height || 0));
}

async function refreshExplorer() {
  els.refresh.disabled = true;
  els.refresh.textContent = "Refreshing";
  els.blocksTitle.textContent = "Checking RPC";
  const from = Math.max(0, Number.parseInt(els.fromHeight.value || "0", 10));

  try {
    const [status, mempool, blocks] = await Promise.all([
      fetchJSON("/status"),
      fetchJSON("/mempool").catch(() => ({ tx: [] })),
      fetchJSON(`/blocks?from=${from}`),
    ]);

    state.status = status;
    state.blocks = normalizeBlocks(blocks);
    state.mempool = Array.isArray(mempool?.tx) ? mempool.tx : [];
    renderMetrics();
    selectBlock(state.selectedHash);
    renderMempool();
    els.updated.textContent = `Updated ${new Date().toLocaleTimeString()}`;
  } catch (error) {
    els.blocksTitle.textContent = "RPC unavailable";
    els.blockList.innerHTML = `<p class="empty-state">Explorer waiting for RPC endpoint.</p>`;
    els.updated.textContent = error.message || "Unable to reach RPC";
    state.blocks = [];
    state.mempool = [];
    renderMetrics();
    selectBlock("");
    renderMempool();
  } finally {
    els.refresh.disabled = false;
    els.refresh.textContent = "Refresh";
  }
}

els.refresh.addEventListener("click", refreshExplorer);
els.search.addEventListener("input", () => {
  renderBlocks();
  renderMempool();
});
els.blockList.addEventListener("click", (event) => {
  const row = event.target.closest("[data-hash]");
  if (row) selectBlock(row.dataset.hash);
});

refreshExplorer();
