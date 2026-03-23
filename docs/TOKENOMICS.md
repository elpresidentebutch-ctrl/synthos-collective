# SYNTHOS — Tokenomics (SYN)

**Canonical source:** Solidity constants in [`contracts/src/synthos/SYNToken.sol`](../contracts/src/synthos/SYNToken.sol). If this document ever disagrees with deployed code, **the contracts win**.

This page is descriptive, not legal or investment advice.

---

## 1. Token identity

| Field | Value |
|--------|--------|
| **Name** | SYNTHOS |
| **Symbol** | SYN |
| **Decimals** | 18 |
| **Standard** | ERC-20 (+ burn, + snapshot hooks, + `Ownable`, + `Pausable`) |
| **Initial mint** | Entire supply minted once to the token contract address (`address(this)`) in the constructor |

There is **no** exposed `mint` path after deployment in `SYNToken.sol` beyond that single genesis mint. **Burn** is allowed (`ERC20Burnable`), so circulating supply can fall below the initial cap over time.

---

## 2. Fixed supply

| Metric | Raw (wei scale) | Human-readable |
|--------|------------------|----------------|
| **Maximum supply (genesis)** | `100_000_000_000 * 10**18` | **100,000,000,000 SYN** (100 billion) |

---

## 3. Genesis allocation buckets

These are **accounting / intent** constants in the token contract: they sum to **100%** of `INITIAL_SUPPLY` and are emitted as events at deploy. **Physical distribution** happens when the owner calls `allocateTokens(recipient, amount, allocationType)` subject to the contract still holding enough balance.

| Bucket | Share | Amount (18 decimals) |
|--------|-------|----------------------|
| **Ecosystem** | 40% | 40,000,000,000 SYN |
| **Validators** | 30% | 30,000,000,000 SYN |
| **Community** | 20% | 20,000,000,000 SYN |
| **Foundation** | 10% | 10,000,000,000 SYN |
| **Total** | 100% | 100,000,000,000 SYN |

**On-chain names** for `allocationType` strings used in code paths: `"ECOSYSTEM"`, `"VALIDATORS"` (plus other labels as governance chooses for remaining tranches).

---

## 4. How tokens move out of the contract

| Mechanism | Who | Notes |
|-----------|-----|--------|
| **`allocateTokens`** | `onlyOwner` | Transfers from contract balance to recipients; records `allocations`. After governance handover, owner should be governance / timelock / multisig — not an EOA in production. |
| **User transfers** | Token holders | Normal ERC-20 transfers (respects `pause` state). |
| **Burn** | Any holder | Permanently reduces supply. |

---

## 5. Utility (intended)

| Use | Where it shows up in code |
|-----|---------------------------|
| **Governance weight** | Token balance / delegation (`SYNTHOSGovernance.sol`) |
| **Validator staking** | `SYNTHOSStaking.sol` — stake and rewards denominated in SYN |
| **Rewards / vesting** | `RewardDistributor.sol`, `SYNTHOSVestingVault.sol` — schedules are **deployment-time**, not fixed in the token contract |

---

## 6. On-chain governance parameters (SYN)

From [`contracts/src/synthos/SYNTHOSGovernance.sol`](../contracts/src/synthos/SYNTHOSGovernance.sol):

| Parameter | Value |
|-----------|--------|
| **Proposal threshold** | ≥ **100,000 SYN** (100,000 × 10¹⁸) balance to create a proposal |
| **Voting period** | **3 days** |
| **Execution delay (timelock ETA)** | **2 days** after success |
| **Supermajority** | **66%** (constant `SUPERMAJORITY`) |

Voting uses the governance module’s own accounting (`delegates` / `voting_power`); align off-chain docs with the **actual** voting implementation before mainnet.

**Implementation notes (read before mainnet claims):**

- **Voting window length** is set in **blocks**, not seconds: `end_block = block.number + (VOTING_PERIOD / 12)`, i.e. it **assumes ~12s blocks**. On faster or slower chains, wall-clock duration differs.
- **Supermajority math** uses only **for + against** votes; **abstain does not** enter the denominator in `queueProposal` / `getProposalState`.
- **Execution** in this contract marks a proposal executed on-chain but does **not** by itself invoke the OpenZeppelin timelock batch — a production deployment may wrap or replace this with a full timelock execution path.

---

## 7. Staking parameters (validators)

From [`contracts/src/synthos/SYNTHOSStaking.sol`](../contracts/src/synthos/SYNTHOSStaking.sol):

| Parameter | Value |
|-----------|--------|
| **Minimum validator stake** | **100,000 SYN** |
| **Unstake cooldown** | **7 days** |
| **Slash rate (constant)** | **10%** (basis as implemented in contract) |
| **Epoch duration** | **1 day** (default in contract state) |

Reward **emission** rates (how much enters `reward_pool` per epoch) are **not** fixed as top-level constants in this file — document any emission schedule separately once finalized in code or governance.

---

## 8. Treasury & vesting (pattern)

| Contract | Role |
|----------|------|
| [`SYNTHOSTreasury.sol`](../contracts/src/synthos/SYNTHOSTreasury.sol) | Holds assets; **owner should be timelock/multisig**; outbound transfers are `onlyOwner`. |
| [`SYNTHOSVestingVault.sol`](../contracts/src/synthos/SYNTHOSVestingVault.sol) | Linear vesting via OpenZeppelin `VestingWallet`; **beneficiary, start, duration** are constructor parameters per deployment. |

**Suggested off-chain table (fill in per deployment):**

| Tranche | % / SYN | Vesting | Wallet / contract address |
|---------|---------|---------|---------------------------|
| *Example: team* | *TBD* | *TBD* | *TBD* |
| *Example: ecosystem* | *TBD* | *TBD* | *TBD* |

---

## 9. Risk controls

| Control | Contract |
|---------|----------|
| **Pause transfers** | `SYNToken.pause()` / `unpause()` — `onlyOwner` |
| **Snapshots** | `createSnapshot()` — `onlyOwner` (custom snapshot storage in token; align with governance design) |

---

## 10. Go L1 demo vs EVM SYN

The Go devnet / RPC demos may use **different** ledger metadata (e.g. integer balances in examples). **SYN tokenomics for investors and launchpads refer to the EVM `SYNToken` deployment**, not the Go demo ledger.

---

## 11. Checklist before any public deck or sale

- [ ] Every percentage and SYN figure matches `SYNToken` constants.
- [ ] “Who is owner?” answered (multisig / timelock addresses).
- [ ] Vesting tables filled for each tranche actually deployed.
- [ ] Governance and staking numbers in slides match `SYNTHOSGovernance` / `SYNTHOSStaking`.
- [ ] Counsel has reviewed offering structure and disclosures.

---

**Related:** [SEEDIFY_INCUBATION_READINESS.md](./SEEDIFY_INCUBATION_READINESS.md) · [POST_INCUBATION_LAUNCH_READINESS.md](./POST_INCUBATION_LAUNCH_READINESS.md) · [DEPLOYMENT_REGISTRY.md](./DEPLOYMENT_REGISTRY.md)
