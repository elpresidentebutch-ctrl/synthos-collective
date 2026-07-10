#!/usr/bin/env node
/**
 * SYNTHOS Multi-Capability Agent
 *
 * One base assistant can spawn deterministic replicas. Each replica has the
 * same root identity, a different numerical hash, and a focused capability.
 *
 * Usage:
 *   node agents/synthos-agent-swarm.js --list --replicas 12
 *   node agents/synthos-agent-swarm.js "what is the current height?"
 *   node agents/synthos-agent-swarm.js "is this tx suspicious? <tx id>"
 *
 * Env:
 *   SYNTHOS_RPC       default https://rpc.ishamwilliamsblockchains.com
 *   SYNTHOS_AGENT_ID  default synthos-chain-assistant
 *   SYNTHOS_REPLICAS  default 8
 *   OLLAMA_URL        default http://localhost:11434
 *   OLLAMA_MODEL      default llama3.2:1b
 *   SYNTHOS_USE_OLLAMA default false
 */

const crypto = require("crypto");

const RPC = (process.env.SYNTHOS_RPC || "https://rpc.ishamwilliamsblockchains.com").replace(/\/+$/, "");
const BASE_AGENT_ID = process.env.SYNTHOS_AGENT_ID || "synthos-chain-assistant";
const DEFAULT_REPLICAS = Number.parseInt(process.env.SYNTHOS_REPLICAS || "8", 10);
const OLLAMA = process.env.OLLAMA_URL || "http://localhost:11434";
const MODEL = process.env.OLLAMA_MODEL || "llama3.2:1b";
const USE_OLLAMA = /^(1|true|yes)$/i.test(process.env.SYNTHOS_USE_OLLAMA || "");

const capabilities = [
  {
    id: "chain-assistant",
    title: "Plain-English Chain Assistant",
    purpose: "Explains balances, block height, mempool, blocks, accounts, and transaction status.",
  },
  {
    id: "onboarding-support",
    title: "Onboarding and Support",
    purpose: "Answers how to run a node, what SYNTHOS is, how buying SYN should work, and what is safe to claim.",
  },
  {
    id: "transaction-risk",
    title: "Transaction Risk Explainer",
    purpose: "Looks at visible chain activity and explains possible risk signals without pretending to have magic scoring.",
  },
  {
    id: "governance-summarizer",
    title: "Governance Summarizer",
    purpose: "Summarizes proposals or governance text in plain English and names likely effects.",
  },
  {
    id: "mempool-watcher",
    title: "Mempool Watcher",
    purpose: "Reports pending transaction pressure and whether submitted activity appears to be waiting for blocks.",
  },
  {
    id: "block-production-monitor",
    title: "Block Production Monitor",
    purpose: "Checks whether the chain is advancing and whether block proposal appears available.",
  },
  {
    id: "node-operator-guide",
    title: "Node Operator Guide",
    purpose: "Helps validators understand node setup, RPC health, and operational next steps.",
  },
  {
    id: "token-sale-reconciler",
    title: "Token Sale Reconciler",
    purpose: "Helps compare payment claims against visible SYN delivery state and flags missing confirmations.",
  },
];

function numericalHash(input) {
  const digest = crypto.createHash("sha256").update(input).digest();
  const value = digest.readBigUInt64BE(0) % 1_000_000_000_000n;
  return value.toString().padStart(12, "0");
}

function spawnReplicas(count = DEFAULT_REPLICAS) {
  const safeCount = Number.isFinite(count) && count > 0 ? Math.min(count, 256) : DEFAULT_REPLICAS;
  return Array.from({ length: safeCount }, (_, i) => {
    const capability = capabilities[i % capabilities.length];
    const hash = numericalHash(`${BASE_AGENT_ID}:${i + 1}:${capability.id}`);
    return {
      agent_id: `${BASE_AGENT_ID}-${hash}`,
      parent_agent_id: BASE_AGENT_ID,
      replica: i + 1,
      numerical_hash: hash,
      capability: capability.id,
      title: capability.title,
      purpose: capability.purpose,
    };
  });
}

async function getJSON(path) {
  try {
    const response = await fetch(`${RPC}${path}`, { cache: "no-store" });
    if (!response.ok) return { error: `HTTP ${response.status}` };
    return await response.json();
  } catch (error) {
    return { error: error.message };
  }
}

function findAddress(text) {
  const match = text.match(/0x[a-fA-F0-9]{40}/);
  return match ? match[0] : "";
}

function findTxID(text) {
  const match = text.match(/0x[a-fA-F0-9]{16,64}|[a-fA-F0-9]{32,64}/);
  return match ? match[0] : "";
}

async function gatherContext(question) {
  const status = await getJSON("/status");
  const mempool = await getJSON("/mempool");
  const dexPools = await getJSON("/dex/pools");

  const height = Number(status.height || 0);
  const from = Math.max(0, height - 5);
  const blocks = await getJSON(`/blocks?from=${from}`);

  const address = findAddress(question);
  const account = address ? await getJSON(`/account?address=${encodeURIComponent(address)}`) : null;

  return {
    rpc: RPC,
    status,
    mempool,
    recent_blocks: blocks,
    dex_pools: dexPools,
    address,
    account,
    tx_id: findTxID(question),
  };
}

function allTransactions(ctx) {
  const blocks = Array.isArray(ctx.recent_blocks?.blocks) ? ctx.recent_blocks.blocks : [];
  const finalized = blocks.flatMap((block) => (block.tx || []).map((tx) => ({ ...tx, block_height: block.header?.height, finalized: block.finalized })));
  const pendingMap = ctx.mempool?.tx || {};
  const pending = Array.isArray(pendingMap)
    ? pendingMap
    : Object.values(pendingMap).map((tx) => ({ ...tx, pending: true }));
  return { finalized, pending };
}

function chooseReplica(question, replicas) {
  const q = question.toLowerCase();
  const wanted = [
    [/risk|suspicious|safe|scam|score/, "transaction-risk"],
    [/governance|proposal|vote/, "governance-summarizer"],
    [/mempool|pending|waiting|stuck/, "mempool-watcher"],
    [/block|height|propose|finalize|frozen|production/, "block-production-monitor"],
    [/run a node|validator|operator|health|rpc/, "node-operator-guide"],
    [/buy|sale|token sale|payment|delivered|reconcile/, "token-sale-reconciler"],
    [/what is|how do i|help|onboard|explain synthos/, "onboarding-support"],
  ];
  const match = wanted.find(([pattern]) => pattern.test(q));
  if (match) return replicas.find((replica) => replica.capability === match[1]) || replicas[0];
  return replicas.find((replica) => replica.capability === "chain-assistant") || replicas[0];
}

function deterministicAnswer(replica, question, ctx) {
  const q = question.toLowerCase();
  const status = ctx.status || {};
  const { finalized, pending } = allTransactions(ctx);
  const mempoolSize = pending.length || ctx.mempool?.size || 0;

  if (ctx.address && /balance|account|wallet/.test(q)) {
    if (ctx.account?.error) return `I could not read that account from ${ctx.rpc}: ${ctx.account.error}.`;
    return `${ctx.address} has ${Number(ctx.account?.balance || 0).toLocaleString()} SYN and nonce ${ctx.account?.nonce ?? 0}.`;
  }

  if (/payment|transaction|tx|went through|confirm/.test(q)) {
    const txID = ctx.tx_id;
    const confirmed = txID ? finalized.find((tx) => String(tx.id || "").toLowerCase() === txID.toLowerCase()) : null;
    const waiting = txID ? pending.find((tx) => String(tx.id || "").toLowerCase() === txID.toLowerCase()) : null;
    if (confirmed) return `That transaction is finalized in block height ${confirmed.block_height}. It moved ${confirmed.amount} ${confirmed.asset_id || "SYN"} from ${confirmed.from} to ${confirmed.to}.`;
    if (waiting) return `That transaction is still pending in the mempool. It has not been finalized into a block yet.`;
    return `I do not see that transaction in the recent finalized blocks or current mempool exposed by this RPC. Current height is ${status.height ?? "unknown"} and mempool size is ${mempoolSize}.`;
  }

  if (replica.capability === "transaction-risk" || /risk|suspicious|safe|scam|score/.test(q)) {
    const signals = [];
    if (Number(status.height || 0) === 0 && mempoolSize > 0) signals.push("transactions are pending while height is still 0, which means settlement is not happening yet");
    if (mempoolSize > 50) signals.push(`the mempool has ${mempoolSize} pending transactions`);
    if (!signals.length) signals.push("I do not see an obvious risk signal from the exposed status, recent blocks, and mempool");
    return `Risk explanation: ${signals.join("; ")}. This is an explanation from visible chain data, not a guarantee or magic fraud score.`;
  }

  if (replica.capability === "governance-summarizer" || /governance|proposal|vote/.test(q)) {
    return "I can summarize governance text or proposals, but this RPC does not currently expose a proposal endpoint. Paste the proposal text and I will explain what it changes, who it affects, and what risks voters should notice.";
  }

  if (replica.capability === "onboarding-support" || /what is|how do i|run a node|buy|support|onboard/.test(q)) {
    return "SYNTHOS is the chain and SYN is its native token. The practical first steps are: keep the RPC block-producing, verify /health and /status, run validator nodes with synthosd, and only describe purchases as delivered after the buyer's SYN transfer is finalized in a block.";
  }

  if (replica.capability === "block-production-monitor" || /height|block|frozen|propose|finalize/.test(q)) {
    const frozen = Number(status.height || 0) === 0;
    return `Chain ${status.chain_id || "unknown"} is at height ${status.height ?? "unknown"}. ${frozen ? "Height 0 means no non-genesis blocks are finalized yet." : "Blocks have finalized beyond genesis."} Current mempool size is ${mempoolSize}.`;
  }

  return `Chain ${status.chain_id || "unknown"} is at height ${status.height ?? "unknown"} with ${mempoolSize} pending transaction(s). Ask for a balance, transaction status, risk explanation, onboarding help, or governance summary and I will route it to the right replica.`;
}

async function ollamaAnswer(replica, question, ctx) {
  const system = [
    "You are a SYNTHOS AI agent replica.",
    `Replica ID: ${replica.agent_id}`,
    `Numerical hash: ${replica.numerical_hash}`,
    `Capability: ${replica.title}`,
    replica.purpose,
    "Answer only from the provided live chain context or from clearly labeled operational guidance.",
    "Do not claim a transaction finalized unless the context shows it in a finalized block.",
    "Keep the answer plain-English and short.",
    "",
    "LIVE CONTEXT:",
    JSON.stringify(ctx, null, 2),
  ].join("\n");

  const response = await fetch(`${OLLAMA}/api/chat`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      model: MODEL,
      stream: false,
      messages: [
        { role: "system", content: system },
        { role: "user", content: question },
      ],
    }),
  });
  if (!response.ok) throw new Error(`Ollama HTTP ${response.status}: ${await response.text()}`);
  const data = await response.json();
  return data?.message?.content?.trim() || "";
}

function parseArgs(argv) {
  const args = { list: false, replicas: DEFAULT_REPLICAS, question: [] };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--list") args.list = true;
    else if (arg === "--replicas") args.replicas = Number.parseInt(argv[++i] || "", 10);
    else args.question.push(arg);
  }
  args.question = args.question.join(" ").trim();
  return args;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const replicas = spawnReplicas(args.replicas);

  if (args.list || !args.question) {
    console.log(`\nBase agent: ${BASE_AGENT_ID}`);
    console.log(`RPC: ${RPC}`);
    console.log(`Replicas: ${replicas.length}\n`);
    for (const replica of replicas) {
      console.log(`${replica.replica}. ${replica.agent_id}`);
      console.log(`   hash=${replica.numerical_hash} capability=${replica.title}`);
      console.log(`   ${replica.purpose}`);
    }
    if (!args.question) console.log('\nAsk a question: node agents/synthos-agent-swarm.js "what is my balance 0x...?"\n');
    return;
  }

  const replica = chooseReplica(args.question, replicas);
  const ctx = await gatherContext(args.question);
  let answer = "";
  if (USE_OLLAMA) {
    try {
      answer = await ollamaAnswer(replica, args.question, ctx);
    } catch (error) {
      answer = `${deterministicAnswer(replica, args.question, ctx)}\n\n(Ollama fallback note: ${error.message})`;
    }
  } else {
    answer = deterministicAnswer(replica, args.question, ctx);
  }

  console.log(`\n[${replica.title}] ${replica.agent_id}`);
  console.log(`hash=${replica.numerical_hash} capability=${replica.capability}`);
  console.log(`Q: ${args.question}\n`);
  console.log(`${answer}\n`);
}

main().catch((error) => {
  console.error(error.stack || error.message);
  process.exit(1);
});
