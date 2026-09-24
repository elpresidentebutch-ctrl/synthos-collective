# SYNTHOS Collective

SYNTHOS Collective is an agent-native Layer 1 blockchain project. The network is built around validator agents that combine seven working roles: immune node, economist, governor, communicator, simulator, enforcer, and citizen.

This repository contains the Go blockchain node, peer registry, live explorer files, early-access web routes, deployment configuration, contract workspace, and operational documentation for the SYNTHOS network.

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).

## Current Status

Status as of September 23, 2026:

### Network / live services

Carried forward from the last confirmed check on August 12, 2026 -- not re-verified in this update:

- Live RPC endpoint: `https://rpc.ishamwilliamsblockchains.com`
- Public website: `https://www.ishamwilliamsblockchains.com` (the bare domain without `www.` does not currently resolve -- a DNS fix is still needed)
- Live explorer (linked from the public website): `https://synthos-explorer.onrender.com`
- Render registry/backend service: `https://synthos-www.onrender.com` (serves `/explorer.html`, `/api/explorer/*`, and the early-access routes; matches what `render.yaml` actually deploys)
- Network name: `synthos-mainnet-1`
- Coin display name: `SYN coins`
- Coin symbol: `SYN`

As of the last check, the network service was online and serving RPC/status routes. Public sale/payment execution remains intentionally gated until production custody, legal, and deployment controls are finalized.

Note: an old Render service at `https://synthos-collective.onrender.com` may still exist under this account, disconnected from the live network (it reports zero active nodes). It predates the current `synthos-www` / `synthos-explorer` setup and should be retired or redirected in the Render dashboard so it doesn't get shared as the live site by mistake.

### Source control & release pipeline

- **GitHub** ([`elpresidentebutch-ctrl/synthos-collective`](https://github.com/elpresidentebutch-ctrl/synthos-collective)) is the primary public repository. GitHub Actions is currently unable to run because of an account billing lock, so the release workflow (`.github/workflows/release.yml`) exists in the repo but cannot execute until that's resolved.
- **GitLab** (mirror, `synthos-collective-group/synthos-collective`) has a working release pipeline (`.gitlab-ci.yml`). Release `v0.1.0` is live and public, with all 8 cross-platform node binaries (`synthos-node-*`, `synthosd-*` for linux/amd64, darwin/arm64, darwin/amd64, windows/amd64) plus `SHA256SUMS` attached.
- **Bitbucket** (mirror, `synthoscollective/synthos-collective`, private) was added this week with the full commit history imported from GitHub. A `bitbucket-pipelines.yml` publishes the same cross-platform binaries to Bitbucket Downloads on a tag push, but it hasn't been exercised with a real release yet -- treat it as unverified until a tag is pushed and the run is checked.

In short: GitHub is the canonical public source, GitLab is the one platform with a binary release actually published, and Bitbucket is a private mirror with an as-yet-untested release pipeline.

## Important Security Notice

This repository is public project infrastructure, not a key vault.

Do not commit private keys, wallet seed phrases, deployer keys, recovery codes, API tokens, or payment processor secrets. Local generated wallet material belongs only under ignored local folders such as `.synthos/`.

Previously exposed or test keys must be treated as compromised and must not be reused for live assets.

## What Is In This Repo

```text
cmd/                 Runnable Go entrypoints
internal/            Chain, consensus, networking, RPC, storage, and agent code
config/              Genesis and network configuration
contracts/           Smart contract workspace
website/             Static web pages served by the backend
docs/                Launch, wallet, security, and operating documentation
deploy/              Deployment support files
scripts/             Local helper scripts
render.yaml          Render deployment configuration
Dockerfile           Container build for the live backend
```

## Quick Start

Run the proof network:

```bash
go run ./cmd/l1netcheck
```

Run the local registry/backend:

```bash
go run ./cmd/cloudless-registry -listen :8090
```

Check health:

```bash
curl http://127.0.0.1:8090/health
```

Run the Go test suite:

```bash
go test ./...
```

Build and run the Docker service:

```bash
docker build -t synthos-collective:local .
docker run --rm -p 8080:8080 -v synthos-data:/data synthos-collective:local
```

## Live RPC Routes

The backend exposes these public network routes:

```text
GET  /health
GET  /status
GET  /account
GET  /balance
GET  /mempool
GET  /blocks
GET  /peers
POST /submitTx
POST /proposeBlock
POST /gossip/block
POST /gossip/tx-batch
```

Agent capability routes are also available on compatible validator builds:

```text
GET /capabilities
GET /aen/status
```

## SYN Coins

The current configured display coin is `SYN coins` with symbol `SYN`.

Named genesis wallet allocations are documented in:

- [docs/SYN_COINS_GENESIS_WALLETS.md](docs/SYN_COINS_GENESIS_WALLETS.md)

That document lists public wallet addresses and allocation amounts only. Private wallet files are intentionally local and ignored by Git.

## Early Access

The early-access sale path is present, but payment execution is closed by default until production controls are ready.

Before enabling real payments or token distribution, the project still needs:

- final receiving wallet/custody policy
- deployed sale contract on the intended production network
- verified chain ID and RPC routing
- production ABI and contract address file
- legal review for token sale language
- security review of payment and allocation flow

## Cloudless Launch Path

SYNTHOS does not require Cloudflare, Deno, or a managed edge runtime. The canonical launch path is self-hosted and container-friendly:

1. Run the peer registry/backend from `cmd/cloudless-registry`.
2. Run validator nodes using the shared genesis and peer configuration.
3. Expose RPC/status endpoints from operator-controlled infrastructure.
4. Point websites, explorers, and wallets at the live RPC URL.

See:

- [docs/CLOUDLESS_NETWORK.md](docs/CLOUDLESS_NETWORK.md)
- [docs/RUN_NODE.md](docs/RUN_NODE.md)

## Agent Roles

Each SYNTHOS agent can support these roles:

- Immune Node: validates blocks and transactions
- Economist: manages incentives and resource allocation
- Governor: coordinates proposals and governance
- Communicator: handles peer-to-peer coordination
- Simulator: models outcomes and protocol scenarios
- Enforcer: monitors compliance and slashing conditions
- Citizen: participates in usage, staking, and governance flows

## Contracts

Smart contract work lives in `contracts/`.

```bash
cd contracts
npm install
npm run compile
npx hardhat test
```

Use `contracts/.env.example` as the template for deployment variables. Never commit a real `.env` file.

## Deployment

The active hosted backend is deployed on Render from this repository.

Useful public URLs:

- RPC/status: `https://rpc.ishamwilliamsblockchains.com`
- Render backend (registry + early access): `https://synthos-www.onrender.com`
- Explorer: `https://synthos-explorer.onrender.com`
- Early access page: `https://synthos-www.onrender.com/early-access`

This repository on GitHub (`elpresidentebutch-ctrl/synthos-collective`) is the only repo Render should ever be connected to for deploys. No GitLab project is authorized to host or deploy this code.

## Contributions

SYNTHOS Collective is not accepting outside code contributions at this time.

The repository is maintained as a founder-controlled project. Issues, merge requests, unsolicited patches, and third-party feature submissions may be closed without review until an official contribution process is announced.

## License

SYNTHOS Collective source code is licensed under the Apache License, Version 2.0.

SYNTHOS Collective, SYNTHOS, SYN coins, and related names, marks, and logos remain reserved except as permitted by the license for identifying the origin of the work.

This repository is informational and technical infrastructure only. It is not legal, financial, or investment advice.
