# SYNTHOS Collective — Incubation / launchpad readiness

This document summarizes what is **already in the repository** for incubator-style reviews (for example Seedify or similar programs), what **demo paths** exist, and what **you still own off-repo** (legal, token sale structure, marketing). Application forms differ by season and program; use this as an internal checklist, not as legal or investment advice.

## One-liner

**Agent-native L1 prototype in Go** (ledger, minimal 2/3+ finality, RPC node, devnet) plus a **Python agent framework**, **Solidity token and treasury/vesting/timelock contracts**, and **audit-oriented documentation** under `docs/audit/`.

## What reviewers can verify today (technical)

| Area | Location | Notes |
|------|----------|--------|
| Build & run (Go) | `README.md`, `docs/RUN_NODE.md` | `go run ./cmd/devnet`, `go run ./cmd/rpcnode` |
| Automated checks | `go test ./...`; `contracts/` `npx hardhat test` | Go ledger/consensus/crypto tests; Solidity compile + deploy smoke (CI) |
| Reproducible container | `Dockerfile` | Builds `rpcnode` (default entrypoint) and includes `devnet` binary |
| Consensus / ledger scope | `docs/audit/AUDIT_PACKET.md` | Explicit maturity statement: **not production L1** |
| Threat model & invariants | `docs/audit/` | Architecture, threat model, invariants, review checklist |
| Token & allocations (EVM) | `contracts/src/synthos/SYNToken.sol` | **100B** `SYN` initial supply; allocation constants in contract |
| Distribution mechanics | `contracts/src/synthos/SYNTHOSVestingVault.sol`, `SYNTHOSTimelock.sol`, `SYNTHOSTreasury.sol` (same folder) | Review with counsel before any sale |
| Python agent stack | `src/`, `example.py`, `pytest` | See `COMPLETION_SUMMARY.md`, `QUICKSTART.md` |

## Token snapshot (on-chain source of truth)

The **authoritative** supply and bucket sizes are the **constants in `contracts/src/synthos/SYNToken.sol`** (not prose in older docs, which may cite different figures). Before any public deck, reconcile every slide with the deployed or candidate Solidity.

## Typical incubator ask-list (you should prepare externally)

Programs often ask for some combination of the following. This repo supports the **technical** items; the rest is your responsibility outside Git.

1. **Pitch deck** — problem, solution, differentiation (agent-native / deterministic reasoning story), roadmap, team.
2. **Tokenomics one-pager** — utility, emission, lockups, use of proceeds; must match contracts and legal advice.
3. **Demo** — screen recording or live: devnet logs, RPC submitting a tx, or Docker run instructions.
4. **Security posture** — link `docs/audit/`; plan for **external** smart-contract audit before mainnet/TGE.
5. **Compliance** — jurisdictions, KYC/AML, securities analysis: **hire qualified counsel**.
6. **Community / distribution** — official channels, disclosure norms, bug bounty policy (if claimed).

## Suggested “ready to apply” technical bar

Use this as a team gate before submitting to an incubator:

- [ ] `go test ./...` and `contracts/` Hardhat compile + tests pass in CI (see `.github/workflows/go.yml`).
- [ ] `docker build -t synthos:local .` succeeds; `docker run -p 8080:8080 synthos:local` serves RPC as documented in `README.md` / `docs/RUN_NODE.md`.
- [ ] `docs/audit/AUDIT_PACKET.md` is up to date with the code you are showing.
- [ ] Solidity compiles (`contracts/` + Hardhat config); testnet deployment addresses documented when available ([DEPLOYMENT_REGISTRY.md](./DEPLOYMENT_REGISTRY.md)).
- [ ] Deck + tokenomics PDF **match** `SYNToken.sol` and vesting/timelock/treasury design.

## Checklist walkthrough (verify before you apply)

Use this table against the current repo. **Technical** items can be re-checked locally; **external** items are your responsibility.

| # | Item | Type | Status | How to verify |
|---|------|------|--------|----------------|
| — | **Technical inventory (table above)** | Repo | **Pass** | Paths exist; Go + Solidity layouts as documented |
| 1 | `go test ./...` passes | Technical | **Pass** | Run from repo root: `go test -count=1 ./...` |
| 2 | Hardhat compile + tests pass | Technical | **Pass** | `cd contracts && npx hardhat compile && npx hardhat test` |
| 3 | Same checks in CI | Technical | **Pass** | `.github/workflows/go.yml` runs Go + contracts jobs |
| 4 | `docker build` + RPC | Technical | **Partial** | `Dockerfile` present; run `docker build` where Docker is installed (not verified on every machine) |
| 5 | `docs/audit/AUDIT_PACKET.md` matches code | Technical | **Pass** | Still references `cmd/devnet`, `cmd/rpcnode`, `internal/*` as implemented |
| 6 | Solidity compiles | Technical | **Pass** | Hardhat `sources` = `./src` (under `contracts/`) |
| 7 | Testnet addresses documented **when available** | Technical | **N/A / documented** | No public testnet claimed; see [DEPLOYMENT_REGISTRY.md](./DEPLOYMENT_REGISTRY.md) |
| 8 | Deck + tokenomics match `SYNToken.sol` | External | **You** | Create/review materials vs `contracts/src/synthos/SYNToken.sol` |
| A | Pitch deck | External | **You** | Problem, solution, roadmap, team |
| B | Tokenomics one-pager | External | **You** | Must match contract + counsel |
| C | Demo (recording or live) | External | **You** | `go run ./cmd/devnet`, RPC, or Docker |
| D | Security posture narrative | External | **You** | Point reviewers at `docs/audit/`; plan external Solidity audit |
| E | Compliance | External | **You** | Qualified counsel |
| F | Community / channels | External | **You** | Official links, disclosures |

**Bottom line:** The repo meets the **suggested technical bar** for applying; you still need **deck, tokenomics PDF, demo asset, legal/compliance, and community** on your side.

## Docker quick reference

```bash
docker build -t synthos-collective:incubation .
docker run --rm -p 8080:8080 -v synthos-data:/data synthos-collective:incubation
```

Override entrypoint to run the in-memory multi-node demo:

```bash
docker run --rm --entrypoint /usr/local/bin/devnet synthos-collective:incubation
```

## Honest gaps (do not oversell in applications)

- The Go L1 is an **early prototype**: simplified networking, proposer selection, and fork-choice vs a production chain (`docs/audit/AUDIT_PACKET.md`).
- **No production VM** for user smart contracts on the L1 is claimed in the audit packet; EVM assets are separate contracts.
- **External audit** of Solidity and protocol economics is not replaced by internal `docs/audit/` material.

## Next phase

After incubator acceptance, use **[POST_INCUBATION_LAUNCH_READINESS.md](./POST_INCUBATION_LAUNCH_READINESS.md)** (testnet, verified deploys, external audit, ops).

---

**SYNTHOS Collective** — keep technical claims aligned with code and counsel-approved disclosures.
