# SYNTHOS six-validator serverless testnet

This directory defines a public testnet with one Cloudflare Worker validator and five Deno Deploy validators.

## Protocol

- Chain ID: `synthos-testnet-1`
- Heartbeat target: 15 seconds
- Validators: 6 independent Ed25519 identities
- Finality: 5 distinct validator signatures out of 6
- Proposal rotation: `(height - 1) mod 6`
- Canonical encoding: recursively key-sorted JSON encoded as UTF-8
- Block hashing: SHA-256 over the canonical block without `hash`
- Message authentication: Ed25519 detached signatures over canonical envelopes without `signature`
- Replay protection: persistent `(validator ID, nonce)` uniqueness plus a ten-minute timestamp window
- Equivocation protection: one proposal and one block choice per validator per height, with conflicting messages retained as evidence
- Persistence: Cloudflare Durable Object SQLite storage and one independently assigned Deno KV database per Deno validator
- Catch-up: finalized hash-chain pull from peer `/blocks` endpoints

The 15-second loop is driven by recurring Cloudflare Durable Object alarms. Cloudflare and Deno cron jobs are one-minute safety triggers. Serverless scheduling is best-effort and is not a hard real-time guarantee.

## Endpoints

Every validator exposes:

- `GET /health`
- `GET /status`
- `GET /peers`
- `GET /blocks?from=1&limit=25`
- `GET /blocks/:height`
- `POST /transactions`
- `POST /messages`
- `POST /tick`

## Deployment order

Prerequisites:

- Deno 2.8 or newer, authenticated with `deno deploy`
- Node.js 22 or newer
- Authenticated Wrangler CLI
- A Deno Deploy organization
- A Cloudflare Workers account with Durable Objects enabled

1. Copy `endpoints.example.json` to ignored file `endpoints.json` and replace every placeholder with the six real provider URLs.
2. Generate six fresh identities and genesis:

   ```powershell
   deno run -A scripts/generate-network.ts endpoints.json
   ```

3. Deploy the Cloudflare validator:

   ```powershell
   ./scripts/deploy-cloudflare.ps1
   ```

4. Deploy five Deno validators and five independent KV databases:

   ```powershell
   ./scripts/deploy-deno.ps1 -Organization YOUR_DENO_ORG
   ```

5. Verify identity, registry, transaction acceptance, quorum, finality, and convergence:

   ```powershell
   deno run -A scripts/verify-network.ts
   ```

6. Monitor all six heads continuously:

   ```powershell
   deno run --allow-net --allow-read scripts/monitor-network.ts
   ```

## Secret handling

`generated/` contains all six private Ed25519 seeds and is ignored by Git. Back it up only to an encrypted secrets manager. Do not commit it, paste it into issues or chat, or reuse the private keys previously committed under `validator-deployment/`.

The deployment scripts load each private key only into its corresponding provider application. A validator must never receive another validator's private key.

## Operational acceptance criteria

The network is accepted only when `verify-network.ts` reports:

- six reachable HTTPS endpoints;
- six identities matching the genesis registry;
- exactly six registered peers per node;
- a submitted verification transaction;
- a newly finalized block;
- identical finalized height and hash on all six validators.

## Security boundary

This is testnet software. Before carrying economic value, add externally audited finality certificates to catch-up responses, signed client transactions with account-state execution, rate limiting, authenticated administrative ticks, independent scheduler redundancy, key rotation, backup restoration exercises, alert routing, and adversarial fault-injection tests.
