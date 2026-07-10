#!/usr/bin/env node
/**
 * SYNTHOS Chain Assistant — acting arm.
 *
 * Lets the AI agent SUBMIT a real transaction on the SYNTHOS chain from its
 * OWN small-allowance wallet. Safety model:
 *   - The amount and recipient are extracted deterministically from your
 *     instruction (regex), NOT invented by the AI model.
 *   - The exact proposed transaction is shown to you.
 *   - Nothing is submitted until you type YES.
 *   - Signing/submission uses the chain's real Go tooling (cmd/fundvalidators),
 *     so the signature format is exactly what consensus expects.
 *
 * Usage:
 *   AGENT_PRIVATE_KEY=0x... node agent-act.js "send 50 SYN to 0xABC...(40 hex)"
 *
 * Env:
 *   AGENT_PRIVATE_KEY  (required) the agent wallet's ed25519 private key
 *   SYNTHOS_RPC        (default https://rpc.ishamwilliamsblockchains.com)
 *   SYNTHOS_TX_CHAIN_ID(default 20260702)
 *   AGENT_TX_FEE       (default 10 = chain MIN_FEE)
 */

const path = require("path");
const readline = require("readline");
const { spawn } = require("child_process");

const RPC = process.env.SYNTHOS_RPC || "https://rpc.ishamwilliamsblockchains.com";
const TX_CHAIN_ID = process.env.SYNTHOS_TX_CHAIN_ID || "20260702";
const FEE = process.env.AGENT_TX_FEE || "10";
const KEY = process.env.AGENT_PRIVATE_KEY || "";
const REPO_ROOT = path.resolve(__dirname, "..");

function fail(msg) {
  console.error("\n" + msg + "\n");
  process.exit(1);
}

const instruction = process.argv.slice(2).join(" ").trim();
if (!instruction) {
  console.log('Usage: node agent-act.js "send <amount> SYN to 0x<40 hex address>"');
  process.exit(0);
}
if (!KEY) fail("AGENT_PRIVATE_KEY is not set. Refusing to act without the agent's wallet key.");

// Deterministic extraction — the AI never decides the money values.
const addrMatch = instruction.match(/0x[a-fA-F0-9]{40}/);
const amountMatch = instruction.match(/(\d[\d,]*)\s*SYN/i) || instruction.match(/\b(\d[\d,]*)\b/);

if (!/\bsend\b/i.test(instruction)) {
  fail('Only "send" actions are supported right now. Example: send 50 SYN to 0x...');
}
if (!addrMatch) fail("No valid recipient address (0x + 40 hex characters) found in your instruction.");
if (!amountMatch) fail("No amount found in your instruction. Example: send 50 SYN to 0x...");

const toAddress = addrMatch[0];
const amount = amountMatch[1].replace(/,/g, "");

if (!/^\d+$/.test(amount) || BigInt(amount) <= 0n) fail(`Invalid amount: ${amount}`);

// Derive the agent's own address for display, from the RPC-agnostic tooling later;
// here we just show what we can confirm deterministically.
console.log("\n=== SYNTHOS Agent — Proposed Transaction ===");
console.log(`Network:   synthos-mainnet-1 (tx chain id ${TX_CHAIN_ID})`);
console.log(`RPC:       ${RPC}`);
console.log(`Send:      ${Number(amount).toLocaleString()} SYN`);
console.log(`To:        ${toAddress}`);
console.log(`Fee:       ${FEE} SYN`);
console.log("From:      the agent's own allowance wallet");
console.log("============================================");
console.log("This will submit a REAL transaction to the live chain and cannot be undone.");

const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
rl.question('\nType YES (all caps) to submit, anything else to cancel: ', (answer) => {
  rl.close();
  if (answer !== "YES") {
    console.log("\nCancelled. Nothing was submitted.\n");
    process.exit(0);
  }

  console.log("\nSubmitting via the chain's signing tool...\n");
  const args = [
    "run", "./cmd/fundvalidators",
    "--rpc", RPC,
    "--priv", KEY,
    "--addresses", toAddress,
    "--amount", amount,
    "--fee", FEE,
    "--chain-id", TX_CHAIN_ID,
    "--propose-block",
  ];
  const child = spawn("go", args, { cwd: REPO_ROOT, stdio: "inherit" });
  child.on("error", (e) => fail("Failed to run the Go signer: " + e.message));
  child.on("exit", (code) => {
    if (code === 0) {
      console.log("\nDone. The transaction was submitted and a block was proposed.");
      console.log(`Check it: ${RPC}/account?address=${toAddress}\n`);
    } else {
      console.log(`\nThe signer exited with code ${code}. Nothing may have been submitted — check the output above.\n`);
    }
  });
});
