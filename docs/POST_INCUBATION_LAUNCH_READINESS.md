# SYNTHOS Collective — Post-incubation / pre-TGE readiness

This is the **next gate after incubator acceptance**: moving from “credible prototype + deck” to **launch rehearsal** (public testnet, contract verification, external audit, and operational discipline). Use with counsel for any public sale or token distribution.

## How this differs from incubation

| Phase | Focus |
|--------|--------|
| **Incubation** | Story, team, demo, architecture honesty, initial tokenomics narrative |
| **Post-incubation** | **Reproducible builds**, **deployed artifacts**, **verified contracts**, **external audit**, **runbooks**, **multisig / timelock procedures**, **registry of addresses** |

## Technical gates (in-repo)

### 1. Go stack

- [ ] `go test ./...` green in CI (see `.github/workflows/go.yml`).
- [ ] Public or partner **testnet** procedure documented (`docs/RUN_NODE.md` for multi-node TCP; `cmd/devnet` for demos only).
- [ ] RPC exposes **`GET /health`** for probes and **`GET /status`** for dashboards.

### 2. Solidity stack

- [ ] From `contracts/`: `npm install` then `npm run compile` — all contracts compile.
- [ ] `npx hardhat test` passes (includes deploy smoke in `contracts/test/smoke.test.js`).
- [ ] **Ordered deploy** rehearsed: `npm run deploy:synthos:local` or  
  `npx hardhat run scripts/deploy-synthos.js --network <your_testnet>`.
- [ ] **Address registry**: commit or securely store JSON emitted by the deploy script; keep **explorer links** and **ABI hashes** alongside.
- [ ] **Block explorer verification** for every immutable contract (`hardhat verify` or explorer UI).

### 3. Distribution & governance (EVM)

- [ ] **SYNToken** supply and buckets match **every** public document (source: `contracts/src/synthos/SYNToken.sol`).
- [ ] **SYNTHOSTimelock** `minDelay` and **admin** (multisig) defined for production; rehearse role grants/revocations on testnet.
- [ ] **RewardDistributor.approveToken** (and similar **onlyGovernance** calls) planned as **governance/timelock** actions, not deployer EOA shortcuts in production.
- [ ] **Vesting / treasury** flows (`SYNTHOSVestingVault.sol`, `SYNTHOSTreasury.sol`) wired in a **testnet dress rehearsal** before mainnet.

### 4. Security

- [ ] **External smart-contract audit** completed; findings triaged; **public report** linked from repo or site.
- [ ] `docs/audit/` kept aligned with the **exact commit** under review.
- [ ] **Bug bounty** (optional but common post-incubation) scoped to deployed code with clear rules.

### 5. Operations

- [ ] **Secrets**: no private keys in git; use CI secrets and hardware/multisig for production deployers.
- [ ] **Backups**: chain data dirs (`data_dir` / `SYNTHOS_DATA_DIR`) and **deployment JSON** off-site.
- [ ] **Incident**: single-page runbook (who rotates keys, who pauses token if `Pausable` is used, comms template).

## What you still do outside the repo

- Final **legal** opinion on token classification and **geoblocking** / sale terms.
- **Marketing site** with disclosures consistent with the audit and tokenomics.
- **Community** moderation and **official channel** list (avoid impersonation).
- **Exchange / launchpad** technical questionnaire (RPC, chain ID, contract addresses, liquidity plan).

## Quick commands (copy-paste)

```bash
# Go
go test -count=1 ./...

# Contracts (from repository contracts/ directory)
cd contracts
npm install
npm run compile
npx hardhat test
npm run deploy:synthos:local
```

## Related docs

- [TOKENOMICS.md](./TOKENOMICS.md) — SYN supply, buckets, governance & staking parameters vs Solidity  
- [SEEDIFY_INCUBATION_READINESS.md](./SEEDIFY_INCUBATION_READINESS.md) — prior gate  
- [audit/AUDIT_PACKET.md](./audit/AUDIT_PACKET.md) — reviewer scope and maturity statement  
- [RUN_NODE.md](./RUN_NODE.md) — node operation  

---

This document is operational guidance only, not legal or investment advice.
