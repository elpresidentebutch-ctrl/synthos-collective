## SYNTHOS Collective — Audit Packet (Go)

### What this is

This repository contains an early-stage Go implementation of the SYNTHOS Collective L1: a minimal ledger (`internal/chain`), a 2/3+ vote finality engine (`internal/consensus`), and an agent/envelope + transport layer that drives consensus message exchange (`internal/agent`, `internal/network`, `internal/node`).

This audit packet is intended to help reviewers quickly understand the system and focus on the highest-risk areas.

### Current maturity (important)

- **This is not a production-ready L1** yet.
- Consensus is a minimal finality engine; proposer selection, fork-choice, and real networking are intentionally simplified.
- The provided `cmd/devnet` runs an in-memory multi-node demo. `cmd/rpcnode` exposes a local HTTP API for inspection and tx submission.

### How to build and run

- **Devnet (in-memory, multi-node)**:
  - `go run ./cmd/devnet`
- **Local RPC node**:
  - `go run ./cmd/rpcnode`
  - Default RPC: `:8080`
  - Data directory: set `SYNTHOS_DATA_DIR` (defaults to `.synthos-data`)

### What to review first (recommended)

- **Ledger correctness**: `internal/chain/*`
  - tx validation, nonce handling, deterministic block building, state root computation
- **Finality safety**: `internal/consensus/*`
  - vote tracking, threshold math, duplicate handling, equivocation handling (if any)
- **Envelope crypto & replay**: `internal/agent/*`, `internal/network/*`, `internal/crypto/*`
  - signature verification, freshness windows, replay protection, topic separation
- **Node glue logic**: `internal/node/node.go`
  - validator gating, peer registry assumptions, rate limiting, finalize flow
- **RPC surface & persistence**: `internal/rpc/*`, `internal/storage/*`
  - input validation, serialization, snapshot consistency, corruption recovery

### Audit scope

This audit packet focuses on:

- Safety of state transitions and ledger invariants
- Finality logic and consensus message verification
- Basic network abuse resistance (replay, spam)
- RPC security for local node mode

Out of scope (unless you explicitly choose otherwise):

- P2P networking at Internet scale / NAT traversal / relay trust model beyond basic assumptions
- Tokenomics / staking / slashing / governance (not implemented as full modules)
- Smart contract VM (docs exist, but a VM is not implemented as production code here)

### Key documents

- `docs/audit/ARCHITECTURE.md`: component map and data flows
- `docs/audit/THREAT_MODEL.md`: assets, actors, trust assumptions, attack classes
- `docs/audit/INVARIANTS.md`: intended safety properties
- `docs/audit/REVIEW_CHECKLIST.md`: concrete reviewer checklist

---

## Legal notice

SYNTHOS Collective, SYNTHOS, and related names, marks, documentation, and technical materials in this document are the **exclusive property of James G. Isham Williams, Sr.** Unauthorized reproduction, distribution, or commercial use without express written permission is prohibited except as allowed under applicable open-source licenses for identified files. No rights are waived.

This document is informational only and is not legal, financial, or investment advice. The canonical legal notice is in **docs/LEGAL_NOTICE.md** in the SYNTHOS Collective repository.
