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

Use the generated local env file for custody-controlled launch values:

```text
# PRIVATE_KEY is intentionally stored only in .synthos/mainnet-secrets/2026-07-02/
SYNTHOS_RPC_URL=http://localhost:8545
SYNTHOS_CHAIN_ID=1234
TIMELOCK_MIN_DELAY=172800
MULTISIG_OWNERS=0xa68A867aAdA7652eB3FeE14a5786B92317139B5c,0xD37fCaa767d425E11Ff7CC074B4e924cE60DcdB5,0x6DA0C1148c76b5bd77EF5455eE79A6859e865290
MULTISIG_THRESHOLD=2
TREASURY_WALLET=0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2
```

The private key is stored only in `.synthos/mainnet-secrets/2026-07-02/`
and `contracts/.env.mainnet.local`, both ignored by Git.

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

## 8. Bitcoin & WBTC Payment Setup

The core `SYNTHOSEarlyAdopterSale` contract only auto-delivers SYN for
EVM-native payments (ETH, USDC, USDT, WETH, WBTC). Native BTC and any other
non-EVM asset cannot trigger it directly.

To accept WBTC (no new contract, config only):

```bash
WBTC_ADDRESS=<wbtc token address on the target chain> \
WBTC_USD_PRICE=<current BTC/USD price> \
pnpm hardhat run scripts/enable-wbtc-payment.js --network synthos
```

To accept native Bitcoin (see `BITCOIN_ADOPTER_SALE.md` for the full trust
model before enabling this):

```bash
BITCOIN_SALE_CONFIRMER=<address that will verify and confirm BTC payments> \
BITCOIN_SALE_ALLOCATION=<SYN to fund the contract with> \
pnpm hardhat run scripts/deploy-bitcoin-sale.js --network synthos
```

The confirmer address must be held to treasury-signer custody standards. It
can release SYN for any Bitcoin transaction it claims occurred, verified or
not, so do not launch native BTC acceptance until you have a specific,
accountable process for who checks payments and how.
