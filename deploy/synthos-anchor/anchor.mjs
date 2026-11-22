// SYNTHOS -> Base block-hash anchor relayer.
//
//   node anchor.mjs new-key            make a relayer wallet (writes $KEY_FILE, prints address only)
//   node anchor.mjs balance            show the relayer's ETH balance on Base
//   node anchor.mjs deploy             deploy SynthosAnchor to Base (relayer = this wallet)
//   node anchor.mjs anchor             post the latest SYNTHOS block hash once
//   node anchor.mjs status             show what's anchored on Base
//   node anchor.mjs check              confirm this wallet is the contract's relayer
//
// Settings (environment, or /etc/synthos-anchor/env):
//   BASE_RPC     default https://mainnet.base.org   (Base Sepolia: https://sepolia.base.org)
//   COMET_RPC    default http://127.0.0.1:26657     (a SYNTHOS CometBFT node)
//   KEY_FILE     default /etc/synthos-anchor/relayer.key
//   ANCHOR_ADDR  contract address, written to /etc/synthos-anchor/contract after deploy
//
// The private key is only read from KEY_FILE and never printed or committed.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { ethers } from "ethers";

const here = path.dirname(fileURLToPath(import.meta.url));
const CONF_DIR = "/etc/synthos-anchor";
const BASE_RPC = process.env.BASE_RPC || "https://mainnet.base.org";
const COMET_RPC = (process.env.COMET_RPC || "http://127.0.0.1:26657").replace(/\/$/, "");
const KEY_FILE = process.env.KEY_FILE || path.join(CONF_DIR, "relayer.key");
const ADDR_FILE = path.join(CONF_DIR, "contract");

// Prefer local artifact in same folder, fallback to contracts/src/synthos/
let artifactPath = path.join(here, "SynthosAnchor.json");
if (!fs.existsSync(artifactPath)) {
  artifactPath = path.join(here, "../../contracts/src/synthos/SynthosAnchor.json");
}
const artifact = JSON.parse(fs.readFileSync(artifactPath, "utf8"));

function die(msg) { console.error(new Date().toISOString() + " [ERROR] " + msg); process.exit(1); }
function log(msg) { console.log(new Date().toISOString() + " [INFO] " + msg); }

function wallet(provider) {
  if (!fs.existsSync(KEY_FILE)) die(`no relayer key at ${KEY_FILE}; run: node anchor.mjs new-key`);
  return new ethers.Wallet(fs.readFileSync(KEY_FILE, "utf8").trim(), provider);
}
function contractAddr() {
  const a = process.env.ANCHOR_ADDR || (fs.existsSync(ADDR_FILE) ? fs.readFileSync(ADDR_FILE, "utf8").trim() : "");
  if (!ethers.isAddress(a)) die("no contract address yet; run: node anchor.mjs deploy");
  return a;
}
async function comet(p) {
  const r = await fetch(COMET_RPC + p, { signal: AbortSignal.timeout(15000) });
  if (!r.ok) throw new Error(`${p}: HTTP ${r.status}`);
  const j = await r.json();
  if (j.error) throw new Error(`${p}: ${JSON.stringify(j.error)}`);
  return j.result;
}
const hex32 = (h) => "0x" + (h || "").toLowerCase().padStart(64, "0");

async function main() {
  const cmd = process.argv[2];
  const provider = new ethers.JsonRpcProvider(BASE_RPC);

  if (cmd === "new-key") {
    if (fs.existsSync(KEY_FILE)) die(`${KEY_FILE} already exists; not overwriting`);
    fs.mkdirSync(path.dirname(KEY_FILE), { recursive: true, mode: 0o700 });
    const w = ethers.Wallet.createRandom();
    fs.writeFileSync(KEY_FILE, w.privateKey + "\n", { mode: 0o600 });
    console.log(`Relayer wallet created. Key saved to ${KEY_FILE} (root-only).`);
    console.log(`Relayer address: ${w.address}`);
    console.log(`Send about $2 of ETH ON BASE to that address, then run: node anchor.mjs deploy`);
    return;
  }

  if (cmd === "balance") {
    const w = wallet(provider);
    const net = await provider.getNetwork();
    console.log(`network chainId ${net.chainId}  relayer ${w.address}  balance ${ethers.formatEther(await provider.getBalance(w.address))} ETH`);
    return;
  }

  if (cmd === "deploy") {
    if (fs.existsSync(ADDR_FILE)) die(`already deployed: ${fs.readFileSync(ADDR_FILE, "utf8").trim()}`);
    const w = wallet(provider);
    const net = await provider.getNetwork();
    const bal = await provider.getBalance(w.address);
    if (bal === 0n) die(`relayer ${w.address} has 0 ETH on chain ${net.chainId}; fund it first`);
    log(`deploying SynthosAnchor to chain ${net.chainId} from ${w.address} (balance ${ethers.formatEther(bal)} ETH)`);
    const f = new ethers.ContractFactory(artifact.abi, artifact.bytecode, w);
    const c = await f.deploy(w.address);
    log(`tx ${c.deploymentTransaction().hash}, waiting...`);
    await c.waitForDeployment();
    const addr = await c.getAddress();
    fs.mkdirSync(CONF_DIR, { recursive: true });
    fs.writeFileSync(ADDR_FILE, addr + "\n");
    log(`deployed at ${addr}`);
    const scan = net.chainId === 8453n ? "https://basescan.org" : "https://sepolia.basescan.org";
    console.log(`View: ${scan}/address/${addr}`);
    return;
  }

  if (cmd === "anchor") {
    const w = wallet(provider);
    const c = new ethers.Contract(contractAddr(), artifact.abi, w);
    const st = await comet("/status");
    if (st.node_info.network !== "synthos-mainnet-1") die(`CometBFT node is on ${st.node_info.network}, expected synthos-mainnet-1`);
    if (st.sync_info.catching_up) die("CometBFT node is still catching up; not anchoring");
    const h = BigInt(st.sync_info.latest_block_height);
    const last = await c.latestHeight();
    if (h <= last) {
      die(`nothing new to anchor (Base contract already has height ${last}, local SYNTHOS at ${h})`);
    }

    const bal = await provider.getBalance(w.address);
    if (bal === 0n) die(`relayer wallet ${w.address} has 0 ETH on Base; cannot submit anchor`);

    const b = await comet(`/block?height=${h}`);
    const blockHash = hex32(b.block_id.hash);
    const appHash = hex32(b.block.header.app_hash);
    const t = BigInt(Math.floor(Date.parse(b.block.header.time) / 1000));
    log(`anchoring height ${h} hash ${blockHash}`);
    const tx = await c.anchor(h, blockHash, appHash, t);
    const rc = await tx.wait();
    if (!rc || rc.status !== 1) {
      die(`transaction reverted or unconfirmed on Base: ${tx.hash} (status ${rc ? rc.status : "null"})`);
    }
    log(`anchored in Base block ${rc.blockNumber}, tx ${tx.hash}, gas ${rc.gasUsed}`);
    return;
  }

  if (cmd === "check") {
    const w = wallet(provider);
    const c = new ethers.Contract(contractAddr(), artifact.abi, provider);
    const r = await c.relayer();
    const bal = await provider.getBalance(w.address);
    console.log(`contract ${await c.getAddress()}  relayer on contract ${r}`);
    console.log(`this server's wallet ${w.address}  balance ${ethers.formatEther(bal)} ETH`);
    if (r.toLowerCase() !== w.address.toLowerCase()) die("this server is not the relayer yet; the owner must call setRelayer(" + w.address + ")");
    if (bal === 0n) die("relayer wallet has 0 ETH on Base; fund it first");
    console.log("OK: this server can anchor.");
    return;
  }

  if (cmd === "status") {
    const c = new ethers.Contract(contractAddr(), artifact.abi, provider);
    const [h, a] = await c.latest();
    console.log(JSON.stringify({ contract: await c.getAddress(), relayer: await c.relayer(), owner: await c.owner(),
      anchors: (await c.anchorCount()).toString(), latestHeight: h.toString(), blockHash: a.blockHash,
      appHash: a.appHash, blockTime: new Date(Number(a.blockTime) * 1000).toISOString() }, null, 2));
    return;
  }

  console.log("usage: node anchor.mjs new-key | balance | deploy | anchor | status | check");
}

main().catch((e) => die(e.shortMessage || e.message || String(e)));
