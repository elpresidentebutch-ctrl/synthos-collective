import nacl from "npm:tweetnacl@1.0.3";
import { Genesis } from "../core.ts";

function base64(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

const endpointsPath = Deno.args[0] ?? "endpoints.json";
const endpoints = JSON.parse(await Deno.readTextFile(endpointsPath)) as Record<string, string>;
const expected = ["cloudflare-1", "deno-1", "deno-2", "deno-3", "deno-4", "deno-5"];
if (Object.keys(endpoints).length !== expected.length || expected.some((id) => !endpoints[id])) {
  throw new Error(`endpoints file must define exactly: ${expected.join(", ")}`);
}
for (const [id, url] of Object.entries(endpoints)) {
  const parsed = new URL(url);
  if (parsed.protocol !== "https:" || parsed.hostname.includes("<")) throw new Error(`invalid production HTTPS URL for ${id}`);
}

const generated = new URL("../generated/", import.meta.url);
await Deno.mkdir(generated, { recursive: true, mode: 0o700 });

const keys = expected.map((id) => {
  const pair = nacl.sign.keyPair();
  return { id, seed: pair.secretKey.slice(0, nacl.sign.seedLength), publicKey: pair.publicKey };
});

const genesis: Genesis = {
  protocolVersion: 1,
  chainId: "synthos-testnet-1",
  network: "synthos-testnet-1",
  genesisTime: new Date().toISOString(),
  heartbeatMs: 15000,
  quorum: 5,
  validators: keys.map((key) => ({ id: key.id, publicKey: base64(key.publicKey), url: endpoints[key.id] })),
  allocations: {
    "testnet-faucet": "100000000000",
  },
};

const genesisJson = JSON.stringify(genesis);
await Deno.writeTextFile(new URL("genesis.json", generated), JSON.stringify(genesis, null, 2), { mode: 0o600 });
await Deno.writeTextFile(new URL("public-registry.json", generated), JSON.stringify(genesis.validators, null, 2), { mode: 0o600 });

for (const key of keys) {
  const env = [
    `VALIDATOR_ID=${key.id}`,
    `VALIDATOR_PRIVATE_KEY=${base64(key.seed)}`,
    `GENESIS_JSON=${genesisJson}`,
    "",
  ].join("\n");
  const path = new URL(`${key.id}.env`, generated);
  await Deno.writeTextFile(path, env, { mode: 0o600 });
  try { await Deno.chmod(path, 0o600); } catch { /* Windows ACLs are managed separately. */ }
}

console.log(`Generated ${keys.length} independent Ed25519 identities in serverless-testnet/generated/`);
console.log("This directory contains private keys. Never commit, upload, email, or paste its contents.");
