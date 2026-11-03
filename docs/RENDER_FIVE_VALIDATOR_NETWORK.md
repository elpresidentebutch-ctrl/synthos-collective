# SYNTHOS Render Five-Validator Network (Decommissioned)

> [!NOTE]
> Render hosting has been decommissioned in favor of direct VPS and sovereign node deployments to avoid split-brain state on `cloudless-registry.json` and eliminate unnecessary cloud hosting costs. This document is retained for historical architectural reference.

This repository previously defined five persistent Render validator services:

- `synthos-validator-11`
- `synthos-validator-12`
- `synthos-validator-13`
- `synthos-validator-14`
- `synthos-validator-15`

Each service ran `/usr/local/bin/synthosd`, mounted its own `/data` disk, loaded a matching `/config/render-validator-XX.json`, registered with the primary relay, and synced blocks with the other validators over HTTPS using `/gossip/block` and `/blocks?from=N`.

## Secrets

Set a unique `SYNTHOS_PRIVATE_KEY` secret on every validator service for stable Ed25519 identity. Do not commit private keys. If this secret is missing, the node can still start, but it will generate a new runtime key after restarts.

## Verify

Run:

```bash
node scripts/verify-render-network.mjs
```

The network is healthy when all endpoints return the same `height`, `tip`, and `state_root`.
