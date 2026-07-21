# SYNTHOS Render Five-Validator Network

This repository's root `render.yaml` defines five persistent Render validator services:

- `synthos-validator-11`
- `synthos-validator-12`
- `synthos-validator-13`
- `synthos-validator-14`
- `synthos-validator-15`

Each service runs `/usr/local/bin/synthosd`, mounts its own `/data` disk, loads a matching `/config/render-validator-XX.json`, registers with `synthos-www`, and syncs blocks with the other validators over HTTPS using `/gossip/block` and `/blocks?from=N`.

## Secrets

Set a unique `SYNTHOS_PRIVATE_KEY` secret on every validator service for stable Ed25519 identity. Do not commit private keys. If this secret is missing, the node can still start, but it will generate a new runtime key after restarts.

## Verify

After Render finishes deploying, run:

```bash
node scripts/verify-render-network.mjs
```

The network is healthy when all five endpoints return the same `height`, `tip`, and `state_root`.
