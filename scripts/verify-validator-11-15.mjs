import http from "node:http";
import https from "node:https";

const endpoints = [11, 12, 13, 14, 15].map((id) => ({
  id: `validator-${id}`,
  url: `http://127.0.0.1:81${id}`,
}));

const requiredCapabilities = [
  "immune_node",
  "economist",
  "governor",
  "communicator",
  "simulator",
  "enforcer",
  "citizen",
];

const timeoutMs = Number(process.env.SYNTHOS_VERIFY_TIMEOUT_MS || 150_000);
const pollMs = Number(process.env.SYNTHOS_VERIFY_POLL_MS || 5_000);
const startedAt = Date.now();

async function getJSON(url) {
  const parsed = new URL(url);
  const client = parsed.protocol === "https:" ? https : http;
  return await new Promise((resolve, reject) => {
    const request = client.get(parsed, { timeout: 5_000 }, (response) => {
      let body = "";
      response.setEncoding("utf8");
      response.on("data", (chunk) => {
        body += chunk;
        if (body.length > 2_000_000) {
          request.destroy(new Error(`${url} response too large`));
        }
      });
      response.on("end", () => {
        if (response.statusCode < 200 || response.statusCode >= 300) {
          reject(new Error(`${url} returned ${response.statusCode}: ${body.slice(0, 200)}`));
          return;
        }
        try {
          resolve(JSON.parse(body));
        } catch (error) {
          reject(error);
        }
      });
    });
    request.on("timeout", () => request.destroy(new Error(`${url} timed out`)));
    request.on("error", reject);
  });
}

function hasAllCapabilities(status) {
  const actual = new Set(status.capabilities || status.agent?.capabilities || []);
  return requiredCapabilities.every((capability) => actual.has(capability));
}

function summarize(statuses) {
  return statuses.map(({ id, status }) => ({
    id,
    height: status.height,
    tip: status.tip,
    state_root: status.state_root,
    chain_id: status.chain_id,
    peers: Array.isArray(status.peers) ? status.peers.length : 0,
    reachable_peers: status.live?.reachable_peers ?? 0,
    configured_peers: status.live?.configured_peers ?? 0,
    immune_capable: status.immune_capable === true,
    capabilities_ok: hasAllCapabilities(status),
  }));
}

function converged(statuses) {
  if (statuses.length !== endpoints.length) return false;
  const first = statuses[0].status;
  return statuses.every(({ status }) =>
    status.chain_id === first.chain_id &&
    status.height === first.height &&
    status.tip === first.tip &&
    status.state_root === first.state_root &&
    status.height >= 1 &&
    Array.isArray(status.peers) &&
    status.peers.length === endpoints.length - 1 &&
    hasAllCapabilities(status)
  );
}

let lastSummary = [];
while (Date.now() - startedAt < timeoutMs) {
  const statuses = [];
  for (const endpoint of endpoints) {
    try {
      await getJSON(`${endpoint.url}/health`);
      statuses.push({
        ...endpoint,
        status: await getJSON(`${endpoint.url}/status`),
      });
    } catch (error) {
      statuses.push({
        ...endpoint,
        status: {
          error: error.message,
          height: null,
          tip: null,
          state_root: null,
          peers: [],
          live: {},
        },
      });
    }
  }

  lastSummary = summarize(statuses);
  console.table(lastSummary);

  if (converged(statuses)) {
    console.log("SYNTHOS validators 11-15 are healthy, capability-complete, and converged.");
    process.exit(0);
  }

  await new Promise((resolve) => setTimeout(resolve, pollMs));
}

console.error("SYNTHOS validators 11-15 did not converge before timeout.");
console.error(JSON.stringify(lastSummary, null, 2));
process.exit(1);
