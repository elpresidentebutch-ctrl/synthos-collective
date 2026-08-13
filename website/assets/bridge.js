const APPROVE_SELECTOR = "0x095ea7b3";
const LOCK_SELECTOR = "0x12482d10";
const SYN_DECIMALS = BigInt(window.SYNTHOS_EVM_SYN_DECIMALS || "18");

const els = {
  locks: document.getElementById("bridgeLocks"),
  releases: document.getElementById("bridgeReleases"),
  processed: document.getElementById("bridgeProcessed"),
  rpc: document.getElementById("bridgeRPC"),
  eventsTitle: document.getElementById("eventsTitle"),
  events: document.getElementById("bridgeEvents"),
  updated: document.getElementById("bridgeUpdated"),
  wallet: document.getElementById("walletStatus"),
  output: document.getElementById("bridgeOutput"),
  connect: document.getElementById("connectWallet"),
  approve: document.getElementById("approveToken"),
  lock: document.getElementById("lockToken"),
  vault: document.getElementById("vaultAddress"),
  token: document.getElementById("tokenAddress"),
  destinationChain: document.getElementById("destinationChain"),
  recipient: document.getElementById("nativeRecipient"),
  amount: document.getElementById("bridgeAmount"),
  refresh: document.getElementById("refreshBridge"),
};

let account = "";

function html(value) {
  return String(value ?? "").replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    "\"": "&quot;",
    "'": "&#39;",
  })[char]);
}

function short(value, size = 10) {
  const text = String(value || "");
  if (!text) return "-";
  return text.length > size * 2 + 3 ? `${text.slice(0, size)}...${text.slice(-size)}` : text;
}

function assertAddress(value, label) {
  const text = value.trim();
  if (!/^0x[0-9a-fA-F]{40}$/.test(text)) throw new Error(`${label} must be a 20-byte 0x address`);
  return text;
}

function leftPad64(hex) {
  return hex.replace(/^0x/, "").padStart(64, "0");
}

function encodeUint(value) {
  const n = BigInt(value);
  if (n < 0n) throw new Error("negative values are not allowed");
  return n.toString(16).padStart(64, "0");
}

function utf8Hex(text) {
  return Array.from(new TextEncoder().encode(text)).map((b) => b.toString(16).padStart(2, "0")).join("");
}

function rightPadWord(hex) {
  const mod = hex.length % 64;
  return mod === 0 ? hex : hex + "0".repeat(64 - mod);
}

function parseTokenAmount(value) {
  const text = String(value || "").trim();
  if (!/^\d+(\.\d+)?$/.test(text)) throw new Error("amount must be a positive number");
  const [whole, fraction = ""] = text.split(".");
  const scale = 10n ** SYN_DECIMALS;
  const paddedFraction = (fraction + "0".repeat(Number(SYN_DECIMALS))).slice(0, Number(SYN_DECIMALS));
  const amount = BigInt(whole) * scale + BigInt(paddedFraction || "0");
  if (amount <= 0n) throw new Error("amount must be greater than zero");
  return amount;
}

function encodeApprove(spender, amount) {
  return APPROVE_SELECTOR + leftPad64(spender) + encodeUint(amount);
}

function encodeLock(asset, amount, destinationChainId, recipient) {
  const recipientHex = utf8Hex(recipient);
  return LOCK_SELECTOR
    + leftPad64(asset)
    + encodeUint(amount)
    + encodeUint(destinationChainId)
    + encodeUint(128)
    + encodeUint(recipientHex.length / 2)
    + rightPadWord(recipientHex);
}

async function ethereumRequest(args) {
  if (!window.ethereum) throw new Error("No EVM wallet found. Install MetaMask or another injected wallet.");
  return window.ethereum.request(args);
}

async function connectWallet() {
  const accounts = await ethereumRequest({ method: "eth_requestAccounts" });
  account = accounts[0] || "";
  els.wallet.textContent = account ? `Connected: ${short(account, 8)}` : "Wallet not connected.";
}

function formValues() {
  const vault = assertAddress(els.vault.value, "Bridge vault");
  const token = assertAddress(els.token.value, "Token");
  const destinationChain = BigInt(els.destinationChain.value.trim());
  const recipient = els.recipient.value.trim();
  if (!recipient) throw new Error("Native recipient is required");
  const amount = parseTokenAmount(els.amount.value);
  return { vault, token, destinationChain, recipient, amount };
}

async function sendTx(to, data) {
  if (!account) await connectWallet();
  const txHash = await ethereumRequest({
    method: "eth_sendTransaction",
    params: [{ from: account, to, data }],
  });
  els.output.textContent = `Transaction submitted:\n${txHash}`;
  return txHash;
}

async function approveToken() {
  const { vault, token, amount } = formValues();
  await sendTx(token, encodeApprove(vault, amount));
}

async function lockToken() {
  const { vault, token, amount, destinationChain, recipient } = formValues();
  await sendTx(vault, encodeLock(token, amount, destinationChain, recipient));
}

async function refreshBridge() {
  const [status, events] = await Promise.all([
    fetch("/api/bridge/status", { cache: "no-store" }).then((r) => r.json()),
    fetch("/api/bridge/events?limit=25", { cache: "no-store" }).then((r) => r.json()),
  ]);
  const bridge = status.bridge || {};
  els.locks.textContent = bridge.native_locks ?? 0;
  els.releases.textContent = bridge.native_releases ?? 0;
  els.processed.textContent = bridge.processed_messages ?? 0;
  els.rpc.textContent = status.rpc_attached === false ? "Not attached" : status.ok === false ? "Unavailable" : "Attached";
  const items = Array.isArray(events.events) ? events.events : [];
  els.eventsTitle.textContent = `${items.length} bridge event${items.length === 1 ? "" : "s"}`;
  els.events.innerHTML = items.map((event) => `
    <article class="block-row">
      <span><strong>${html(event.type || "-")}</strong><small>${html(short(event.id, 12))}</small></span>
      <span><strong>${html(event.amount ?? 0)}</strong><small>${html(event.asset_id || "syn")}</small></span>
      <span><strong>${html(event.destination_chain_id || event.source_chain_id || "-")}</strong><small>${html(short(event.address, 10))}</small></span>
    </article>
  `).join("") || `<p class="empty-state">No native bridge events yet.</p>`;
  els.updated.textContent = `Updated ${new Date().toLocaleTimeString()}`;
}

els.connect.addEventListener("click", () => connectWallet().catch((e) => { els.output.textContent = e.message; }));
els.approve.addEventListener("click", () => approveToken().catch((e) => { els.output.textContent = e.message; }));
els.lock.addEventListener("click", () => lockToken().catch((e) => { els.output.textContent = e.message; }));
els.refresh.addEventListener("click", () => refreshBridge().catch((e) => { els.output.textContent = e.message; }));

refreshBridge().catch((e) => {
  els.output.textContent = e.message;
});
