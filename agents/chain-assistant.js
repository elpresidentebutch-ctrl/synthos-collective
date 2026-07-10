#!/usr/bin/env node
/**
 * SYNTHOS Chain Assistant — a real, offline AI agent.
 *
 * It pulls live data from the SYNTHOS chain RPC, hands that context to a
 * local AI model running on this machine via Ollama (no internet needed for
 * the model's thinking), and answers plain-English questions about the chain.
 *
 * Usage:
 *   node chain-assistant.js "what is the current block height?"
 *   node chain-assistant.js "what is the balance of 0x825f...?"
 *
 * Env overrides:
 *   SYNTHOS_RPC   (default https://rpc.ishamwilliamsblockchains.com)
 *   OLLAMA_URL    (default http://localhost:11434)
 *   OLLAMA_MODEL  (default llama3.2:1b)
 */

const RPC = process.env.SYNTHOS_RPC || "https://rpc.ishamwilliamsblockchains.com";
const OLLAMA = process.env.OLLAMA_URL || "http://localhost:11434";
const MODEL = process.env.OLLAMA_MODEL || "llama3.2:1b";

async function getJSON(url) {
  try {
    const r = await fetch(url, { cache: "no-store" });
    if (!r.ok) return { error: `HTTP ${r.status}` };
    return await r.json();
  } catch (e) {
    return { error: e.message };
  }
}

// Gather live chain context relevant to the question.
async function gatherContext(question) {
  const ctx = {};
  ctx.status = await getJSON(`${RPC}/status`);
  ctx.dexPools = await getJSON(`${RPC}/dex/pools`);

  // If the question contains an address, look it up.
  const addrMatch = question.match(/0x[a-fA-F0-9]{40}/);
  if (addrMatch) {
    ctx.account = await getJSON(`${RPC}/account?address=${addrMatch[0]}`);
    ctx.accountAddress = addrMatch[0];
  }
  return ctx;
}

async function ask(question) {
  const ctx = await gatherContext(question);

  const system = [
    "You are the SYNTHOS Chain Assistant, an AI agent that helps people understand the SYNTHOS blockchain.",
    "Answer ONLY from the live chain data provided below. If the data doesn't contain the answer, say so plainly — do not invent facts.",
    "Keep answers short, clear, and friendly for a non-technical person. SYN is the network's token.",
    "",
    "LIVE CHAIN DATA (JSON):",
    JSON.stringify(ctx, null, 2),
  ].join("\n");

  const body = {
    model: MODEL,
    stream: false,
    messages: [
      { role: "system", content: system },
      { role: "user", content: question },
    ],
  };

  let r;
  try {
    r = await fetch(`${OLLAMA}/api/chat`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  } catch (e) {
    console.error(
      `\nCouldn't reach the local AI model at ${OLLAMA}.\n` +
        `Make sure Ollama is running and the model is downloaded (ollama pull ${MODEL}).\n` +
        `Details: ${e.message}`
    );
    process.exit(1);
  }

  if (!r.ok) {
    console.error(`Ollama returned HTTP ${r.status}: ${await r.text()}`);
    process.exit(1);
  }

  const data = await r.json();
  const answer = data && data.message && data.message.content;
  console.log("\n" + (answer ? answer.trim() : "(no answer returned)") + "\n");
}

const question = process.argv.slice(2).join(" ").trim();
if (!question) {
  console.log('Usage: node chain-assistant.js "your question about the SYNTHOS chain"');
  process.exit(0);
}

console.log(`\n[SYNTHOS Chain Assistant] model=${MODEL} (offline) · rpc=${RPC}`);
console.log(`Q: ${question}`);
ask(question);
