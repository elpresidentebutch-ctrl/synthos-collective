# Running a Synthos Validator on Deno Deploy

Deno Deploy gives you a globally distributed validator node with zero infrastructure.  
Free tier: 100K requests/day, built-in KV database, no Docker needed.

---

## 1. Prerequisites

- A GitHub account (Deno Deploy links to your repo)
- The Synthos repo pushed to GitHub
- Optionally: [Deno CLI](https://deno.land/manual/getting_started/installation) + `deployctl` for CLI deploys

---

## 2. Deploy via Deno Dashboard (Recommended)

1. Go to [https://dash.deno.com](https://dash.deno.com) and sign in with GitHub.  
2. Click **New Project**.  
3. Link your **synthos-collective** repository.  
4. Set the **entry point** to:
   ```
   deploy/deno/validator.ts
   ```
5. Set **environment variables**:

   | Variable | Value | Description |
   |----------|-------|-------------|
   | `WORKER_NAME` | `deno-validator-1` | Unique name for this validator (used in round-robin proposer selection) |
   | `SELF_URL` | Deno Deploy assigned project URL | The public URL Deno assigns your project |

6. Click **Deploy**.

Your validator is live at the project URL shown in the Deno Deploy dashboard.

---

## 3. Deploy via CLI

Install the deploy tool:

```bash
deno install -A jsr:@deno/deployctl
```

Deploy:

```bash
cd synthos-collective
deployctl deploy --project=synthos-validator deploy/deno/validator.ts \
  --env=WORKER_NAME=deno-validator-1 \
  --env=SELF_URL=https://synthos-validator.deno.dev
```

---

## 4. Verify It's Running

```bash
curl $SELF_URL/status
```

Expected response:

```json
{
  "chain_id": "synthos-mainnet",
  "height": 0,
  "mempool_size": 0,
  "validators": ["deno-validator-1", "synthos-validator-11", ...],
  "next_proposer": "..."
}
```

The validator starts at genesis and auto-syncs blocks from peers within 6 seconds.

---

## 5. API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/status` | Chain height, mempool size, validators, next proposer |
| GET | `/blocks?from=0&to=10` | Fetch blocks by height range |
| POST | `/submitTx` | Submit a transaction `{ from, to, amount }` |
| GET | `/account?address=agent-0` | Account balance and nonce |
| GET | `/balance?address=agent-0` | Balance only |
| GET | `/mempool` | Pending transactions |
| GET | `/peers` | Known validators |
| POST | `/gossip/block` | Receive a gossiped block |
| POST | `/gossip/tx-batch` | Receive gossiped transactions |
| POST | `/reset` | Reset chain to genesis (careful!) |

---

## 6. Running Multiple Deno Validators

Each validator needs a unique `WORKER_NAME`. Create separate Deno Deploy projects:

| Project | WORKER_NAME | SELF_URL |
|---------|-------------|----------|
| synthos-deno-1 | `deno-validator-1` | `https://synthos-deno-1.deno.dev` |
| synthos-deno-2 | `deno-validator-2` | `https://synthos-deno-2.deno.dev` |
| synthos-deno-3 | `deno-validator-3` | `https://synthos-deno-3.deno.dev` |

They will discover each other through the shared peer list (the `FALLBACK_PEERS` array in the source).

---

## 7. Adding Your Deno Validator to the Network

To integrate your Deno validator with existing nodes, add its URL to the `FALLBACK_PEERS` array in `deploy/deno/validator.ts`:

```typescript
const FALLBACK_PEERS = [
  "https://synthos-validator-11.jamesishamwilliams.workers.dev",
  // ... existing peers ...
  process.env.SELF_URL,  // Deno validator URL from deployment environment
];
```

Also add it to the mobile validator's peer list (Peers tab → Add Peer) so phones sync from it too.

---

## 8. Connecting the Mobile PWA to Your Deno Validator

1. Open the mobile validator PWA.  
2. Go to the **Peers** tab.  
3. Enter the Deno Deploy project URL and tap **Add Peer**.  
4. Tap **Sync Now** on the Chain tab.

Your phone will now pull blocks from the Deno validator in addition to any other peers.

---

## 9. How Consensus Works

Synthos uses **round-robin Proof of Authority**:

- Validators are sorted alphabetically by name.  
- At each block height, `validatorOrder[height % validatorOrder.length]` is the designated proposer.  
- The proposer bundles mempool transactions, computes the new state root, and broadcasts the block.  
- Other validators verify the block (re-execute TXs, check state root + hashes) before accepting.  

Your Deno validator participates automatically — the heartbeat loop (every 6s) handles syncing, gossiping, and proposing.

---

## 10. Deno Deploy Limits (Free Tier)

| Resource | Limit |
|----------|-------|
| Requests | 100,000/day |
| KV reads | 450,000/day |
| KV writes | 45,000/day |
| CPU time | 50ms/request |
| Outbound data | 100 GiB/month |

A validator with a 6-second heartbeat uses ~14,400 requests/day for its own heartbeat loop, leaving plenty of headroom for incoming queries and gossip.

---

## 11. Troubleshooting

| Problem | Fix |
|---------|-----|
| `/status` returns height 0 after minutes | Check `FALLBACK_PEERS` — are any peers reachable? The heartbeat syncs from them. |
| "KV not available" | Make sure you deploy to Deno Deploy (not just `deno run` locally without `--unstable-kv`). |
| Validator never proposes blocks | Your `WORKER_NAME` must be in the sorted validator list. Add your URL to `FALLBACK_PEERS` in other validators too. |
| Requests return 500 | Check the Deno Deploy dashboard logs for stack traces. |
| Running locally | `deno run --allow-net --allow-env --unstable-kv deploy/deno/validator.ts` |
