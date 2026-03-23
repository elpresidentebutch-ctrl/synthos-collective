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
| Token & allocations (EVM) | `contracts/synthos/SYNToken.sol` | **100B** `SYN` initial supply; allocation constants in contract |
| Distribution mechanics | `SYNTHOSVestingVault.sol`, `SYNTHOSTimelock.sol`, `SYNTHOSTreasury.sol` | Review with counsel before any sale |
| Python agent stack | `src/`, `example.py`, `pytest` | See `COMPLETION_SUMMARY.md`, `QUICKSTART.md` |

## Token snapshot (on-chain source of truth)

The **authoritative** supply and bucket sizes are the **constants in `SYNToken.sol`** (not prose in older docs, which may cite different figures). Before any public deck, reconcile every slide with the deployed or candidate Solidity.

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
- [ ] Solidity compiles (`contracts/` + Hardhat config); testnet deployment addresses documented when available.
- [ ] Deck + tokenomics PDF **match** `SYNToken.sol` and vesting/timelock/treasury design.

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
