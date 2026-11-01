# SYNTHOS Collective

SYNTHOS Collective is an agent-native Layer 1 blockchain project. The network is built around validator agents that combine seven working roles: immune node, economist, governor, communicator, simulator, enforcer, and citizen.

This repository contains the Go blockchain node, peer registry, live explorer files, early-access web routes, deployment configuration, contract workspace, and operational documentation for the SYNTHOS network.

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).

## Current Status

Status as of October 31, 2026:

### Network / Consensus Engine
- **Consensus**: CometBFT (BFT proof-of-stake consensus, live on mainnet since October 22, 2026).
- **Production Node**: `synthos-comet` in [`cometapp/`](cometapp/) handles CometBFT p2p, consensus voting, validator set rotation, and executes the SYNTHOS state machine via ABCI.
- **Outbound Candidate Client**: `synthos-node` in [`cmd/silentnode/`](cmd/silentnode/) is an outbound-only Proof-of-Uptime candidate client (no open ports required) for operators onboarding toward validator eligibility.
- **Legacy Engine**: [`cmd/synthosd/`](cmd/synthosd/) is preserved as the pre-CometBFT reference engine and migration source.
- **Base L2 Anchor**: Synthos Collective commits hourly block fingerprints to Base mainnet via the `SynthosAnchor` contract at [`0xae4C167ff08d13CaC9b397D13c50904bb9c881a5`](https://basescan.org/address/0xae4C167ff08d13CaC9b397D13c50904bb9c881a5).
- **Network Name**: `synthos-mainnet-1`
- **Native Coin**: `SYN` (fixed supply: 100,000,000,000 SYN)

### Live Services
- **Registry & Backend Service**: `https://synthos-www.onrender.com` (deployed from `Dockerfile` via Render blueprint `render.yaml`). Serves the peer registry, node heartbeat endpoints, explorer backend, and early-access routes.
- **Public Website**: `https://www.ishamwilliamsblockchains.com`
- **Live Explorer**: `https://82-29-54-7.sslip.io`

### Release Binaries
Cross-platform binaries are published for Linux (amd64), macOS (arm64, amd64), and Windows (amd64) with SHA256 checksums:
- `synthos-comet`: The production CometBFT validator node binary (`cometapp/`).
- `synthos-node`: The candidate Proof-of-Uptime node binary (`cmd/silentnode/`).
- `synthosd`: The legacy consensus node binary (`cmd/synthosd/`, archive reference).

## What Is In This Repo

```text
cometapp/            Production CometBFT validator node (synthos-comet), ABCI app, and tests
cmd/                 Runnable Go entrypoints (cloudless-registry, silentnode, bridgerelayer, synthosd)
internal/            Chain state machine, cryptographic primitives, RPC, storage, and networking
config/              Genesis specifications and network configuration
contracts/           Smart contracts (Solidity workspace with Hardhat)
website/             Static web pages and assets served by the registry backend
docs/                Architecture, onboarding, tokenomics, and validator runbooks
deploy/              Deployment configurations and runbooks
scripts/             Operational automation and helper scripts
render.yaml          Render blueprint deployment configuration
Dockerfile           Container build for cloudless-registry and node binaries
src/                 Legacy Python framework prototype (archived)
```

## Quick Start

### 1. Build and Run the Production CometBFT Node (`synthos-comet`)

```bash
# Enter cometapp directory
cd cometapp

# Run all CometBFT application and multi-node network tests
go test -v ./...

# Build the production node binary
go build -o synthos-comet .

# Inspect node commands
./synthos-comet --help
```

### 2. Run the Candidate Node (`cmd/silentnode`)

To connect to the live network as a Proof-of-Uptime candidate node without opening inbound ports:

```bash
go run ./cmd/silentnode -id "my-node-1" -relay "https://synthos-www.onrender.com"
```

### 3. Run the Peer Registry / Backend Locally

```bash
# In local development mode:
go run ./cmd/cloudless-registry -listen :8090 --insecure-dev
```

Check health:
```bash
curl http://127.0.0.1:8090/health
```

### 4. Run the Full Go Test Suite

```bash
# Root module tests
go test ./...

# CometBFT production node tests
(cd cometapp && go test -race -count=1 ./...)
```

### 5. Build Container Image

```bash
docker build -t synthos-collective:local .
docker run --rm -p 8080:8080 -v synthos-data:/data synthos-collective:local
```

## Important Security Notice

This repository is public project infrastructure, not a key vault. Do not commit private keys, wallet seed phrases, deployer keys, recovery codes, API tokens, or payment processor secrets. Local generated wallet material belongs only under ignored local folders such as `.synthos/`.

## Smart Contracts

Smart contract workspace lives in `contracts/`:

```bash
cd contracts
npm install
npm run compile
npx hardhat test
```

## Contributions

SYNTHOS Collective is currently maintained as a founder-controlled project. Outside pull requests are closed without review until an official contribution framework is announced.

## License

SYNTHOS Collective source code is licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
