# SYN Token Launch Runbook

This runbook is the working path from the current SYNTHOS repository to a responsible SYN token launch. It is operational guidance for James G. Isham Williams, Sr. and the SYNTHOS Collective team; it is not legal, tax, financial, or investment advice.

## Source of truth

The launch tokenomics source of truth is `docs/TOKENOMICS.md`, backed by the Solidity constants in:

- `contracts/src/synthos/SynCoin.sol`
- `contracts/src/synthos/SYNTHOSAdopterRewards.sol`
- `contracts/src/synthos/SYNTHOSStaking.sol`
- `contracts/src/synthos/SYNTHOSFounderAnnualVesting.sol`

Current approved supply plan:

| Bucket | Amount |
| --- | ---: |
| Immune Node Rewards | 22,000,000,000 SYN |
| Locked DEX Liquidity | 20,000,000,000 SYN |
| Founder Vesting | 17,000,000,000 SYN |
| Validator/Security Rewards | 12,000,000,000 SYN |
| Community/Adopter Rewards | 12,500,000,000 SYN |
| Ecosystem Treasury | 10,000,000,000 SYN |
| CMO Launch Grant | 3,000,000,000 SYN |
| Strategic Reserve | 3,000,000,000 SYN |
| Founder Launch Allocation | 500,000,000 SYN |

Founder vesting is 1,700,000,000 SYN every May 29 for ten years. The CMO launch grant is 3,000,000,000 SYN upon successful SYN token launch. Immune node rewards are paid per verified operator, not per node count.

## Launch gates

### Gate 1: Repo readiness

Must be true before any public contract link is treated as final:

- `contracts` compile successfully.
- Contract tests pass.
- Contract readiness check passes.
- `docs/TOKENOMICS.md`, public site copy, deck copy, whitepaper copy, and contract constants all match.
- No private keys, seed phrases, production API keys, or unpublished wallet addresses are committed.

Command:

```bash
cd contracts
npm run readiness:contracts
```

### Gate 2: Wallet readiness

Before testnet deployment, prepare these addresses:

| Variable | Purpose |
| --- | --- |
| `FOUNDER_WALLET` | Beneficiary of the ten-year founder vesting vault |
| `FOUNDER_OPS_WALLET` | 500,000,000 SYN founder launch allocation |
| `CMO_WALLET` | 3,000,000,000 SYN CMO launch grant |
| `IMMUNE_NODE_REWARDS_WALLET` | Immune node rewards controller, contract, or multisig |
| `VALIDATOR_REWARDS_WALLET` | Validator rewards controller, staking contract, or multisig |
| `DEX_LIQUIDITY_WALLET` | Locked liquidity reserve wallet |
| `COMMUNITY_WALLET` | Community, adopter, builder, and grant wallet |
| `TREASURY_WALLET` | Ecosystem treasury wallet |
| `STRATEGIC_RESERVE_WALLET` | Strategic reserve wallet |

For production, use multisig or timelock-controlled addresses where possible. Do not use a personal hot wallet as the permanent controller for treasury, reserve, liquidity, or reward pools.

### Gate 3: Testnet deployment

Deploy to testnet before mainnet:

```bash
cd contracts
npx hardhat run scripts/deploy-synthos.js --network synthos
```

After deployment:

- Save `contracts/deployments/<network>-<timestamp>.json`.
- Confirm every wallet received the exact allocation expected.
- Confirm founder vesting received exactly 17,000,000,000 SYN.
- Confirm the CMO wallet received exactly 3,000,000,000 SYN.
- Confirm immune rewards and validator rewards are funded to their intended controllers.
- Confirm the DEX liquidity reserve is separated from operating wallets.
- Publish testnet addresses as testnet-only.

### Gate 4: Operator reward rehearsal

Before public operator rewards begin:

- Register at least one immune node operator on testnet.
- Confirm the 500 SYN early operator reward pays once.
- Confirm a second node from the same operator does not create a second reward stream.
- Advance time or wait one full heartbeat interval.
- Confirm the 1,000 SYN heartbeat reward pays monthly in arrears.
- Confirm used heartbeat proofs cannot be replayed.
- Confirm the owner can deactivate a disqualified operator.

### Gate 5: Governance and control

Before mainnet:

- Define the production timelock delay.
- Assign production governance, treasury, reserve, and reward-control roles.
- Rehearse role grants and revocations on testnet.
- Remove deployer-only shortcuts from production operations.
- Record every admin address and emergency procedure in a deployment registry.

### Gate 6: External review

Minimum review before real value is attached:

- Independent smart contract audit or public peer review of the exact commit.
- Legal review of launch language, CMO grant language, operator reward language, and any public sale or exchange activity.
- Tax/accounting review for founder, CMO, treasury, rewards, and grants.
- Written decision on restricted jurisdictions, sanctions screening, and who can receive rewards.

If money is tight, the lowest-cost version is still useful: freeze the commit, publish the exact scope, ask for public review, run a small private testnet, and do not promise profit, investment return, or guaranteed income.

## Mainnet launch sequence

1. Freeze the launch commit and tag it.
2. Run `npm run readiness:contracts`.
3. Deploy to testnet and rehearse the full allocation.
4. Verify contracts on the target explorer.
5. Update deployment registry with addresses, chain ID, explorer links, and ABI hashes.
6. Publish final tokenomics with the same values as `docs/TOKENOMICS.md`.
7. Deploy mainnet contracts.
8. Verify mainnet contracts.
9. Transfer ownership/admin paths to governance, timelock, or multisig.
10. Publish only the verified mainnet contract links.

## Hard no-go conditions

Do not launch the token if any of these are true:

- The CMO wallet, founder wallet, treasury wallet, or reward wallets are unknown or placeholders.
- The public site, deck, whitepaper, or contract constants disagree.
- The deployer wallet remains the long-term controller of all funds.
- The reward contracts are not funded or tested.
- The founder vesting release dates are wrong.
- The CMO grant terms are not signed or at least acknowledged in writing.
- There is no incident plan for a bad deployment, compromised key, or incorrect wallet address.

## Current operator reward policy

Immune node operators:

- One reward stream per verified operator.
- Running one node or one hundred nodes earns the same base reward.
- 500 SYN one-time early verified operator reward.
- 1,000 SYN monthly heartbeat reward paid in arrears.
- Maximum 120 heartbeat claims over ten years.
- Maximum ten-year base reward per operator: 120,500 SYN.
- Target network size: 100,000 immune node operators.

Validator operators:

- Target validator network size: 5,000 validators.
- 10,000 SYN activation or migration reward.
- 5,000 SYN monthly base validator reward.
- Up to 2,500 SYN monthly performance bonus.
- Maximum ten-year validator reward per validator: 910,000 SYN.

Community, builder, and adopter rewards are grant-based and campaign-based. They are not wages, employment compensation, or guaranteed income.
