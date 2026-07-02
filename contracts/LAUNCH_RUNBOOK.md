# SYNTHOS Launch Runbook

This runbook is the operational checklist for launching the SYNTHOS contract
stack. Do not fund production wallets until every required check below is
complete.

## 1. Wallets and Custody

Required launch wallets:

- `PRIVATE_KEY`: deployer key with enough native gas token for deployment
- `MULTISIG_OWNERS`: comma-separated sovereign multisig owner addresses
- `MULTISIG_THRESHOLD`: required owner confirmations, recommended `2` for `3` owners
- `FOUNDER_WALLET`
- `FOUNDER_OPS_WALLET`
- `CMO_WALLET`
- `COMMUNITY_WALLET`
- `DEX_LIQUIDITY_WALLET`
- `TREASURY_WALLET`
- `STRATEGIC_RESERVE_WALLET`
- `IMMUNE_NODE_REWARDS_WALLET`, optional; defaults to adopter rewards contract
- `VALIDATOR_REWARDS_WALLET`, optional; defaults to staking contract

Custody rules:

- Never commit `.env`.
- Do not paste private keys into chat, issues, or docs.
- Production multisig owner keys should be backed up offline.
- The deployer is a bootstrap actor only.
- After deploy, `SYNTHOSMultisig` administers `SYNTHOSTimelock`.
- `SYNTHOSGovernance` is the timelock proposer.
- Timelock execution is open after the delay matures.
- `SynCoin`, `SYNTHOSAdopterRewards`, `SYNTHOSDex`, and
  `SYNTHOSComplianceRegistry` ownership transfers to `SYNTHOSTimelock`.

## 2. Environment

Copy `.env.example` to `.env`, then fill in production values:

```text
PRIVATE_KEY=0x...
SYNTHOS_RPC_URL=https://...
TIMELOCK_MIN_DELAY=172800
MULTISIG_OWNERS=0xOwner1,0xOwner2,0xOwner3
MULTISIG_THRESHOLD=2
TREASURY_WALLET=0xTreasuryOrMultisig
```

If `TREASURY_WALLET` is omitted, the deploy script uses the sovereign multisig
as the Treasury Recycling Burn destination.

## 3. Required Local Checks

Run from `contracts/`:

```bash
pnpm readiness:contracts
```

This must pass:

- Solidity compile
- Hardhat contract tests
- tokenomics allocation totals
- Treasury Recycling Burn category approvals
- reward configuration checks

## 4. Local Deployment Rehearsal

Run from `contracts/`:

```bash
pnpm deploy:synthos:local
```

Confirm output includes:

- `SYNTHOSMultisig`
- `TIMELOCK_ADMIN: <multisig>`
- `TIMELOCK_PROPOSER: <governance>`
- `TIMELOCK_EXECUTOR: open`
- `SynCoin_OWNER: <timelock>`
- `SYNTHOSAdopterRewards_OWNER: <timelock>`
- `SYNTHOSDex_OWNER: <timelock>`
- `SYNTHOSComplianceRegistry_OWNER: <timelock>`

Local Hardhat deployment JSON files are rehearsal artifacts. Do not treat them
as production addresses.

## 5. Production Deploy

Run only after the audit/security checklist and wallet checklist are complete:

```bash
pnpm hardhat run scripts/deploy-synthos.js --network synthos
```

Save the produced deployment JSON from `contracts/deployments/`.

## 6. Post-Deploy Verification

Run against a persistent network, not a fresh ephemeral Hardhat process:

```bash
DEPLOYMENT_FILE=deployments/latest.json pnpm postdeploy:check --network synthos
```

For local rehearsal, use a persistent `localhost` Hardhat node if you want to
run deployment and post-deploy checks as separate commands.

Verify on-chain:

- Multisig owners match `MULTISIG_OWNERS`
- Multisig threshold matches `MULTISIG_THRESHOLD`
- Timelock admin role is held by multisig
- Governance has proposer/canceller role
- Executor role is open
- Deployer no longer has timelock admin role
- Token treasury matches intended treasury
- Treasury Recycling Burn categories are approved:
  - `SPEND_PROTOCOL`
  - `SPEND_NODE_REGISTRATION`
  - `SPEND_SERVICE_FEE`
  - `SPEND_MARKETPLACE`
- Ownable launch contracts are owned by timelock
- Genesis allocation balances match deployment JSON
- DEX pools have intended reserves

## 7. Publish

Publish only these public values:

- Chain ID and RPC URL
- `SynCoin` address
- `SYNTHOSMultisig` address
- `SYNTHOSTimelock` address
- `SYNTHOSGovernance` address
- DEX address
- Reward and staking addresses
- Token risk disclosure

Never publish private keys, seed phrases, or raw `.env` files.
