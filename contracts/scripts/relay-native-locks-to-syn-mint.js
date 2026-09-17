#!/usr/bin/env node
"use strict";

// Watches the native SYNTHOS chain for bridge_lock_native events that lock
// real SYN into a configured native escrow address for release as wrapped
// SYN on EVM, and calls approveMint on SYNTHOSSynBridgeMinter to mint the
// matching amount. This is the native -> EVM half of the SYN bridge; the
// EVM -> native half (burn -> release) is cmd/bridgerelayer's Go
// `watch-evm` mode.
//
// This intentionally does not use Hardhat's runtime -- it talks to a real
// native RPC endpoint and a real EVM RPC endpoint (which may or may not be
// a local Hardhat node), so it runs as a plain Node process:
//   node scripts/relay-native-locks-to-syn-mint.js
//
// Required environment variables:
//   SYNTHOS_BRIDGE_NATIVE_LOCK_ADDRESS   native address a lock must pay
//                                        into to be treated as real
//                                        backing (see isMintableNativeLock)
//   SYNTHOS_BRIDGE_EVM_RPC_URL           EVM JSON-RPC endpoint
//   SYNTHOS_BRIDGE_EVM_SYN_MINTER        SYNTHOSSynBridgeMinter address
//   SYNTHOS_BRIDGE_EVM_RELAYER_PRIVATE_KEY  EVM private key of an address
//                                        authorized as a relayer on the
//                                        minter contract
//
// Optional:
//   SYNTHOS_NATIVE_RPC_URL               default http://127.0.0.1:8080
//   SYNTHOS_BRIDGE_POLL_INTERVAL_MS      default 15000
//   SYNTHOS_BRIDGE_ONCE                  "true" to run one pass and exit
//   SYNTHOS_BRIDGE_NATIVE_LOCK_STATE_FILE default .synthos/native-lock-mint-state.json

const fs = require("fs");
const path = require("path");
const { ethers } = require("ethers");
const {
  computeSourceEventId,
  wrappedAmountFromNativeUnits,
  isMintableNativeLock,
  revertReason,
} = require("./lib/native-lock-relay");

const MINTER_ABI = [
  "function approveMint(bytes32 sourceEventId, address recipient, uint256 amount) external returns (bytes32 messageId)",
  "function paused() view returns (bool)",
];

function requiredEnv(key) {
  const value = process.env[key];
  if (!value) {
    throw new Error(`${key} is required`);
  }
  return value;
}

function env(key, fallback) {
  const value = process.env[key];
  return value === undefined || value === "" ? fallback : value;
}

function envInt(key, fallback) {
  const value = process.env[key];
  if (value === undefined || value === "") return fallback;
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function loadProcessed(stateFile) {
  try {
    const raw = fs.readFileSync(stateFile, "utf8");
    const ids = JSON.parse(raw);
    return new Set(Array.isArray(ids) ? ids : []);
  } catch {
    return new Set();
  }
}

function markProcessed(processed, stateFile, id) {
  processed.add(id);
  fs.mkdirSync(path.dirname(stateFile), { recursive: true });
  fs.writeFileSync(stateFile, JSON.stringify(Array.from(processed)), "utf8");
}

async function fetchNativeLockEvents(nativeRpcUrl) {
  const url = `${nativeRpcUrl.replace(/\/+$/, "")}/bridge/events?type=bridge_lock_native&limit=500`;
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`GET ${url} failed: ${res.status} ${await res.text()}`);
  }
  const body = await res.json();
  return body.events || [];
}

/**
 * Handles a single native lock event: validates it's real, on-topic
 * backing for this bridge, simulates the mint first to avoid burning gas
 * on a doomed transaction, then submits it for real. Returns a short
 * status string for logging/testing rather than throwing on expected
 * outcomes (not mintable, already handled by another relayer).
 */
async function handleEvent(event, { lockAddress, evmChainId, minter, processed, stateFile }) {
  if (!isMintableNativeLock(event, { lockAddress, evmChainId })) {
    return "skipped:not-mintable";
  }

  const sourceEventId = computeSourceEventId(event.id);
  const amount = wrappedAmountFromNativeUnits(event.amount);
  const recipient = event.destination_recipient;

  try {
    await minter.approveMint.staticCall(sourceEventId, recipient, amount);
  } catch (err) {
    const reason = revertReason(err);
    if (reason.includes("already processed") || reason.includes("already approved")) {
      console.log(`native lock ${event.id}: ${reason}, marking done`);
      markProcessed(processed, stateFile, event.id);
      return `done:${reason}`;
    }
    console.error(`native lock ${event.id}: simulated approveMint failed: ${reason}`);
    return `retry:${reason}`;
  }

  const tx = await minter.approveMint(sourceEventId, recipient, amount);
  const receipt = await tx.wait();
  console.log(
    `native lock ${event.id}: approveMint submitted, tx=${receipt.hash} amount=${event.amount} SYN -> ${recipient}`
  );
  markProcessed(processed, stateFile, event.id);
  return "minted";
}

async function pollOnce({ nativeRpcUrl, lockAddress, evmChainId, minter, processed, stateFile }) {
  const events = await fetchNativeLockEvents(nativeRpcUrl);
  const results = [];
  // The native RPC returns newest-first (matching cmd/bridgerelayer's own
  // convention); walk oldest-to-newest so logs and state updates land in
  // chronological order.
  for (let i = events.length - 1; i >= 0; i--) {
    const event = events[i];
    if (processed.has(event.id)) continue;
    results.push(await handleEvent(event, { lockAddress, evmChainId, minter, processed, stateFile }));
  }
  return results;
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function main() {
  const nativeRpcUrl = env("SYNTHOS_NATIVE_RPC_URL", "http://127.0.0.1:8080");
  const lockAddress = requiredEnv("SYNTHOS_BRIDGE_NATIVE_LOCK_ADDRESS");
  const evmRpcUrl = requiredEnv("SYNTHOS_BRIDGE_EVM_RPC_URL");
  const minterAddress = requiredEnv("SYNTHOS_BRIDGE_EVM_SYN_MINTER");
  const relayerPrivateKey = requiredEnv("SYNTHOS_BRIDGE_EVM_RELAYER_PRIVATE_KEY");
  const pollMs = envInt("SYNTHOS_BRIDGE_POLL_INTERVAL_MS", 15000);
  const once = env("SYNTHOS_BRIDGE_ONCE", "false") === "true";
  const stateFile = path.resolve(
    env("SYNTHOS_BRIDGE_NATIVE_LOCK_STATE_FILE", path.join(".synthos", "native-lock-mint-state.json"))
  );

  const provider = new ethers.JsonRpcProvider(evmRpcUrl);
  const wallet = new ethers.Wallet(relayerPrivateKey, provider);
  const minter = new ethers.Contract(minterAddress, MINTER_ABI, wallet);
  const network = await provider.getNetwork();
  const evmChainId = network.chainId.toString();

  console.log("native lock -> SYN mint relayer starting");
  console.log(`  native rpc:          ${nativeRpcUrl}`);
  console.log(`  native lock address: ${lockAddress}`);
  console.log(`  evm chain id:        ${evmChainId}`);
  console.log(`  syn bridge minter:   ${minterAddress}`);
  console.log(`  relayer address:     ${wallet.address}`);
  if (await minter.paused()) {
    console.log("  WARNING: SYNTHOSSynBridgeMinter is currently paused; mints will fail until it's unpaused.");
  }

  const processed = loadProcessed(stateFile);

  for (;;) {
    try {
      await pollOnce({ nativeRpcUrl, lockAddress, evmChainId, minter, processed, stateFile });
    } catch (err) {
      console.error("poll error:", err.message || err);
    }
    if (once) break;
    await sleep(pollMs);
  }
}

if (require.main === module) {
  main().catch((err) => {
    console.error(err);
    process.exit(1);
  });
}

module.exports = { main, pollOnce, handleEvent, fetchNativeLockEvents };
