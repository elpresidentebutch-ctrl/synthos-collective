# SYNTHOS Collective - Testing Guide

This guide details the test suites across the SYNTHOS Collective codebase, covering the production Go chain engine, the CometBFT consensus application, the smart contract workspace, and the historical Python prototype.

---

## Testing Architecture

The codebase contains four distinct test domains:

| Domain | Directory | Language / Runner | Scope |
| :--- | :--- | :--- | :--- |
| **CometBFT Production Node** | `cometapp/` | Go 1.26 (`go test`) | ABCI application, live in-process CometBFT networks, consensus rotation, state export, legacy API |
| **L1 Core & Registry** | `internal/`, `cmd/` | Go 1.26 (`go test`) | State machine, block execution, cryptographic signatures, peer registry, fail-closed auth, bridge relayer |
| **Smart Contracts** | `contracts/` | Solidity / Hardhat (`npm test`) | `SynthosAnchor` on Base L2, ERC20 tokens, sale & staking contracts |
| **Agent Prototype (Archive)** | `src/`, `tests/` | Python 3.8+ (`pytest`) | Historical 7-role agent conceptual simulation |

---

## 1. CometBFT Production Node Tests (`cometapp/`)

The tests in `cometapp/` validate the application state machine against ABCI calls as well as spinning up real, multi-node in-process CometBFT networks.

### Run All CometBFT Tests
```bash
cd cometapp
go test -v ./...
```

### Run with Race Detector
```bash
cd cometapp
go test -race -count=1 ./...
```

### Key Test Packages:
- **`cometapp/app`**:
  - `TestGenesisRulesForCometBFT`: Genesis constraints and validation.
  - `TestTransferPaysFeeToProposerOperator`: Fee attribution to block producers.
  - `TestBondJoinsValidatorSetAtEpochBoundary`: Validator staking and voting power transitions.
  - `TestFullUnbondRemovesValidatorAtEpoch`: Unbonding queues and validator exit.
  - `TestCometMisbehaviorSlashesAndRemovesImmediately`: Slashing for double-signing evidence.
  - `TestTwoNodesReachIdenticalAppHashes`: Deterministic AppHash verification across nodes.
  - `TestRestartResumesFromDisk`: Persistent storage recovery across restarts.
- **`cometapp/devnode`**:
  - `TestSingleNodeDevnet`: Spins up an in-process CometBFT single-node network and produces blocks.
  - `TestValidatorNetwork`: Spins up a multi-node validator consensus network, tests peer discovery, newcomer bonding, and validator offline failover.
- **`cometapp/export`**:
  - `TestExportKeepsEveryAccountAndStake`: Migration verification ensuring every account and balance is preserved.
- **`cometapp/legacyapi`**:
  - Tests for `/status`, `/account`, `/balance`, `/submitTx`, rate limiting, and faucet funding.

---

## 2. Core Chain & Registry Tests (`internal/`, `cmd/`)

### Run All Root Tests
```bash
# Run unit and race tests across all internal and cmd packages
go test -race -count=1 ./internal/... ./cmd/...
```

### Run Repeat Consensus Tests (Reproducibility Gate)
```bash
go test -count=25 ./internal/consensus ./internal/chain
```

### Key Test Suites:
- **`internal/chain`**:
  - State machine transitions, Merkle tree odd-leaf hashing, transactions, blocks, citizen staking, and state cloning.
- **`cmd/cloudless-registry`**:
  - Signed node heartbeats, hardware deduplication (`hardware_test.go`), network status computation, reward keeper payouts, and fail-closed secret authentication (`registry_hardening_test.go`).
- **`cmd/synthosd`**:
  - Node keys persistence, mailbox relaying, and validator roster synchronization.
- **`cmd/bridgerelayer`**:
  - Base anchor contract event parsing and EVM transaction submission.

---

## 3. Smart Contracts Tests (`contracts/`)

Smart contracts are tested using Hardhat and Mocha/Chai.

```bash
cd contracts
npm ci --legacy-peer-deps
npm run compile
npm test
```

### Security Scans:
Run static analysis with Slither (configured to fail on high-severity findings):
```bash
slither . --filter-paths "node_modules|artifacts|cache|test" --fail-high
```

---

## 4. Automated CI Security & Linting Policies

The CI workflows ([`.github/workflows/`](.github/workflows/)) enforce strict testing gates:
- **`go.yml`**: Formats with `gofmt`, vets with `go vet`, runs race detector, builds all commands, and runs `l1check` across both root and `cometapp` modules.
- **`security.yml`**:
  - **GoSec**: Static analysis pinned to `v2.22.11` failing on high severity findings.
  - **Go Vulnerability Audit**: `govulncheck@v1.1.4` scanning both root and `cometapp` dependencies.
  - **Coverage Threshold**: Enforces a minimum of 15% code coverage across the Go codebase.
  - **Slither**: Static analysis on contracts with `--fail-high`.

---

## 5. Prototype Python Framework Tests (Historical / Archive)

The original Python prototype tests live in `tests/test_core.py` and `tests/test_roles.py`.

### Setup and Run
```bash
pip install -r requirements.txt
pip install pytest pytest-asyncio

# Run prototype test suite
pytest tests/ -v
```
