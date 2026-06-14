# SYNTHOS - Tokenomics (SYN)

**Canonical source:** Solidity constants in [`contracts/src/synthos/SynCoin.sol`](../contracts/src/synthos/SynCoin.sol). If this document ever disagrees with deployed code, **the contracts win**.

This page is descriptive, not legal, financial, or investment advice.

---

## 1. Token identity

| Field | Value |
|--------|--------|
| **Name** | SYNTHOS |
| **Symbol** | SYN |
| **Decimals** | 18 |
| **Standard** | ERC-20 with burn, pause, snapshots, and owner-distributed genesis supply |
| **Initial mint** | Entire supply minted once to the token contract at deployment |

There is **no** exposed mint path after deployment. Physical distribution happens from the token contract's genesis balance through `allocateTokens`.

---

## 2. Fixed supply

| Metric | Raw value | Human-readable |
|--------|-----------|----------------|
| **Maximum supply** | `100_000_000_000 * 10**18` | **100,000,000,000 SYN** |

---

## 3. Genesis allocation buckets

These six buckets sum to 100% of the fixed supply.

| Bucket | Share | Amount |
|--------|-------|--------|
| **Locked DEX Liquidity** | 29% | 29,000,000,000 SYN |
| **Validator / Immune Node / Adopter Rewards** | 30% | 30,000,000,000 SYN |
| **Community / Public Distribution** | 14.5% | 14,500,000,000 SYN |
| **Ecosystem / Treasury** | 6% | 6,000,000,000 SYN |
| **Founder 10-Year Vesting** | 20% | 20,000,000,000 SYN |
| **Founder Operations Grant** | 0.5% | 500,000,000 SYN |
| **Total** | 100% | 100,000,000,000 SYN |

Recommended `allocationType` strings:

| Allocation | String |
|------------|--------|
| Locked DEX Liquidity | `LOCKED_DEX_LIQUIDITY` |
| Validator / Immune Node / Adopter Rewards | `VALIDATOR_REWARDS` |
| Community / Public Distribution | `COMMUNITY` |
| Ecosystem / Treasury | `ECOSYSTEM_TREASURY` |
| Founder Vesting | `FOUNDER_VESTING` |
| Founder Operations Grant | `FOUNDER_OPERATIONS_GRANT` |

---

## 4. Founder schedule

The founder long-haul allocation is **20,000,000,000 SYN**, released as **2,000,000,000 SYN every May 29** for 10 years.

| Release | Date (UTC) | Unix timestamp | Amount |
|---------|------------|----------------|--------|
| 1 | May 29, 2027 | `1811548800` | 2,000,000,000 SYN |
| 2 | May 29, 2028 | `1843171200` | 2,000,000,000 SYN |
| 3 | May 29, 2029 | `1874707200` | 2,000,000,000 SYN |
| 4 | May 29, 2030 | `1906243200` | 2,000,000,000 SYN |
| 5 | May 29, 2031 | `1937779200` | 2,000,000,000 SYN |
| 6 | May 29, 2032 | `1969401600` | 2,000,000,000 SYN |
| 7 | May 29, 2033 | `2000937600` | 2,000,000,000 SYN |
| 8 | May 29, 2034 | `2032473600` | 2,000,000,000 SYN |
| 9 | May 29, 2035 | `2064009600` | 2,000,000,000 SYN |
| 10 | May 29, 2036 | `2095632000` | 2,000,000,000 SYN |

The 20B founder vesting tranche should be sent to [`SYNTHOSFounderAnnualVesting.sol`](../contracts/src/synthos/SYNTHOSFounderAnnualVesting.sol), with the founder wallet as beneficiary and the exact timestamp array above.

The **500,000,000 SYN Founder Operations Grant** is the launch-period allocation. It exists so the founder can fund legal work, audits, infrastructure, listings, validators, liquidity preparation, community operations, and project execution before the first May 29, 2027 vesting release. It should be disclosed separately from founder vesting so the market can see the founder is locked for the long haul.

---

## 5. How tokens move out of the contract

| Mechanism | Who | Notes |
|-----------|-----|-------|
| `allocateTokens` | `onlyOwner` | Transfers undistributed genesis supply from the token contract and records `allocatedByType`. |
| User transfers | Token holders | Normal ERC-20 transfers, blocked while paused. |
| Burn | Token holders | Permanently reduces circulating supply and total supply. |
| Snapshot | `onlyOwner` | Creates an accounting snapshot for governance or reporting. |

Production ownership should move to governance, a timelock, or a multisig before public launch.

---

## 6. Utility

| Use | Code path |
|-----|-----------|
| Governance weight | [`SYNTHOSGovernance.sol`](../contracts/src/synthos/SYNTHOSGovernance.sol) |
| Validator staking | [`SYNTHOSStaking.sol`](../contracts/src/synthos/SYNTHOSStaking.sol) |
| One-button adopter rewards | [`SYNTHOSAdopterRewards.sol`](../contracts/src/synthos/SYNTHOSAdopterRewards.sol) |
| Decentralized exchange | [`SYNTHOSDex.sol`](../contracts/src/synthos/SYNTHOSDex.sol) |
| Rewards | [`RewardDistributor.sol`](../contracts/src/RewardDistributor.sol) |
| Founder vesting | [`SYNTHOSFounderAnnualVesting.sol`](../contracts/src/synthos/SYNTHOSFounderAnnualVesting.sol) |
| General vesting | [`SYNTHOSVestingVault.sol`](../contracts/src/synthos/SYNTHOSVestingVault.sol) |

---

## 7. Launch checklist

- [ ] Deployed `SynCoin` constants match this document.
- [ ] Founder operations wallet address is recorded.
- [ ] Founder annual vesting vault is deployed with the May 29, 2027-2036 schedule.
- [ ] 20B founder vesting allocation is transferred to the vesting vault.
- [ ] 500M founder operations grant is transferred and publicly disclosed.
- [ ] Adopter rewards contract is funded from the 30B validator / immune node / adopter rewards bucket.
- [ ] Desktop, Android, and iOS node apps create local hardware commitments and submit heartbeat proofs.
- [ ] Adopter Merkle tree is generated, `ADOPTER_MERKLE_ROOT` is set, and launch gating is explicitly chosen with `ADOPTER_MERKLE_GATE_REQUIRED`.
- [ ] `SYNTHOSDex` is deployed, verified, and configured with real asset pool addresses.
- [ ] DEX liquidity lock address, terms, and unlock conditions are published.
- [ ] Treasury / governance owner is a multisig, timelock, or DAO-controlled address.
- [ ] Counsel reviews public sale, listing, founder allocation, and DEX materials.

---

## 8. Go L1 demo vs EVM SYN

The Go devnet / RPC demos may use different ledger metadata. SYN tokenomics for investors, launchpads, the DEX, and public disclosures refer to the EVM `SynCoin` deployment unless a separate native mainnet migration is formally announced.

---

## Legal notice

SYNTHOS Collective, SYNTHOS, and related names, marks, documentation, and technical materials in this document are the **exclusive property of James G. Isham Williams, Sr.** Unauthorized reproduction, distribution, or commercial use without express written permission is prohibited except as allowed under applicable open-source licenses for identified files. No rights are waived.

This document is informational only and is not legal, financial, or investment advice. The canonical legal notice is in **docs/LEGAL_NOTICE.md** in the SYNTHOS Collective repository.
