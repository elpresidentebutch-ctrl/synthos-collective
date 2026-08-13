import http from "node:http";

const nodeURL = process.env.SYNTHOS_PUSH_NODE_URL || "http://127.0.0.1:8120";
const validatorURLs = [8111, 8112, 8113, 8114, 8115].map((port) => `http://127.0.0.1:${port}`);
const requiredCapabilities = [
  "immune_node",
  "economist",
  "governor",
  "communicator",
  "simulator",
  "enforcer",
  "citizen",
];
const timeoutMs = Number(process.env.SYNTHOS_PUSH_VERIFY_TIMEOUT_MS || 150_000);
const pollMs = Number(process.env.SYNTHOS_PUSH_VERIFY_POLL_MS || 5_000);
const startedAt = Date.now();

async function getJSON(url) {
  return await new Promise((resolve, reject) => {
    const request = http.get(new URL(url), { timeout: 5_000 }, (response) => {
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

async function validatorSummary() {
  const statuses = [];
  for (const url of validatorURLs) {
    statuses.push(await getJSON(`${url}/sync/status`));
  }
  statuses.sort((a, b) => Number(b.height) - Number(a.height));
  return statuses[0];
}

while (Date.now() - startedAt < timeoutMs) {
  try {
    await getJSON(`${nodeURL}/health`);
    const [status, capabilities, aen, highestValidator] = await Promise.all([
      getJSON(`${nodeURL}/status`),
      getJSON(`${nodeURL}/capabilities`),
      getJSON(`${nodeURL}/aen/status`),
      validatorSummary(),
    ]);

    const summary = {
      node_id: status.agent?.id || aen.node_id,
      node_height: status.height,
      validator_height: highestValidator.height,
      chain_id: status.chain_id,
      peers: Array.isArray(status.peers) ? status.peers.length : 0,
      reachable_peers: status.live?.reachable_peers ?? 0,
      capabilities_ok: hasAllCapabilities(status) && hasAllCapabilities(capabilities) && hasAllCapabilities(aen),
      aen_ready: aen.ready === true,
      converged: status.height === highestValidator.height && status.tip === highestValidator.tip,
    };
    console.table([summary]);

    if (
      summary.chain_id === highestValidator.chain_id &&
      summary.peers === validatorURLs.length &&
      summary.reachable_peers === validatorURLs.length &&
      summary.capabilities_ok &&
      summary.aen_ready &&
      summary.converged
    ) {
      console.log("Push-button SYNTHOS node is running, capability-complete, and synced.");
      process.exit(0);
    }
  } catch (error) {
    console.log(`waiting for push-button node: ${error.message}`);
  }

  await new Promise((resolve) => setTimeout(resolve, pollMs));
}

console.error("Push-button SYNTHOS node did not become ready before timeout.");
process.exit(1);

