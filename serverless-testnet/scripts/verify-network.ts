import { Genesis } from "../core.ts";

const genesisPath = Deno.args[0] ?? new URL("../generated/genesis.json", import.meta.url).pathname;
const genesis = JSON.parse(await Deno.readTextFile(genesisPath)) as Genesis;
const deadline = Date.now() + 120_000;

async function getJson(url: string): Promise<Record<string, unknown>> {
  const response = await fetch(url, { headers: { "cache-control": "no-cache" } });
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status} ${await response.text()}`);
  return await response.json();
}

console.log("Checking six HTTPS endpoints and registered identities...");
for (const validator of genesis.validators) {
  const health = await getJson(`${validator.url}/health`);
  const status = await getJson(`${validator.url}/status`);
  const peers = await getJson(`${validator.url}/peers`) as unknown as Array<Record<string, unknown>>;
  if (health.validatorId !== validator.id) throw new Error(`${validator.id}: health identity mismatch`);
  if (status.publicKey !== validator.publicKey) throw new Error(`${validator.id}: public key mismatch`);
  if (!Array.isArray(peers) || peers.length !== 6) throw new Error(`${validator.id}: expected six registered peers`);
  console.log(`OK ${validator.id} ${validator.url}`);
}

const tx = {
  id: `verification-${crypto.randomUUID()}`,
  sender: "verification-client",
  nonce: 0,
  payload: { type: "network-verification", submittedAt: new Date().toISOString() },
};
const submission = await fetch(`${genesis.validators[0].url}/transactions`, {
  method: "POST",
  headers: { "content-type": "application/json" },
  body: JSON.stringify(tx),
});
if (!submission.ok) throw new Error(`transaction submission failed: ${submission.status} ${await submission.text()}`);

let baseline = -1;
while (Date.now() < deadline) {
  await Promise.allSettled(genesis.validators.map((validator) => fetch(`${validator.url}/tick`, { method: "POST" })));
  await new Promise((resolve) => setTimeout(resolve, genesis.heartbeatMs));
  const statuses = await Promise.all(genesis.validators.map((validator) => getJson(`${validator.url}/status`)));
  const heads = statuses.map((status) => status.head as { height: number; hash: string });
  if (baseline < 0) baseline = Math.min(...heads.map((head) => head.height));
  const same = heads.every((head) => head.height === heads[0].height && head.hash === heads[0].hash);
  console.log(`heads: ${heads.map((head, index) => `${genesis.validators[index].id}=${head.height}`).join(" ")}`);
  if (same && heads[0].height > baseline) {
    console.log(JSON.stringify({
      verified: true,
      validators: genesis.validators.length,
      quorum: genesis.quorum,
      heartbeatMs: genesis.heartbeatMs,
      finalizedHeight: heads[0].height,
      finalizedHash: heads[0].hash,
      transactionSubmitted: tx.id,
    }, null, 2));
    Deno.exit(0);
  }
}

throw new Error("network did not converge on a new finalized block within 120 seconds");
