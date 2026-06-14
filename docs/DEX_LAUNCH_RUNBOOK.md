# The Collective DEX Launch Runbook

This is the path to a real SYNTHOS DEX launch.

## What Exists

- `SYNTHOSDex.sol`: deployable constant-product AMM.
- `SynCoin.sol`: fixed 100B SYN supply with 29B locked DEX liquidity allocation.
- `deploy-synthos.js`: deploys SYN, governance, adopter rewards, founder vesting, and the DEX.
- `THE_COLLECTIVE_DEX_LIVE.html`: wallet-facing DEX page for deployed contracts.
- `cmd/silentnode`: adopter node process with no inbound ports.

## Architecture Rule

The DEX and nodes must respect SYNTHOS silence:

- The DEX is smart contracts plus a static website.
- Wallets make outbound RPC calls.
- Nodes heartbeat/poll outbound.
- No adopter should open ports or expose a listener.

Local Hardhat ports are developer-only rehearsal infrastructure, not the SYNTHOS node model.

## Production Inputs

Set real production addresses before deploying outside local Hardhat:

```powershell
$env:FOUNDER_WALLET="0x..."
$env:FOUNDER_OPS_WALLET="0x..."
$env:DEX_LIQUIDITY_WALLET="0x..."
$env:COMMUNITY_WALLET="0x..."
$env:TREASURY_WALLET="0x..."
```

Set the adopter Merkle checkpoint if rewards should be gated by a published allowlist:

```powershell
$env:ADOPTER_MERKLE_FILE="merkle/adopter-merkle.json"
$env:ADOPTER_MERKLE_ROOT="0x..."
$env:ADOPTER_MERKLE_GATE_REQUIRED="true"
```

If `ADOPTER_MERKLE_GATE_REQUIRED` is `true`, adopters must call `registerAndClaimWithProof` with a valid proof against the on-chain root.

To generate the root and proofs from an adopter list:

```powershell
cd contracts
Copy-Item merkle/adopters.example.json merkle/adopters.json
npm run merkle:adopters
```

The builder writes `merkle/adopter-merkle.json`. The deploy script auto-loads that file and writes its `merkleRoot` on-chain. For an already deployed rewards contract:

```powershell
npx hardhat run scripts/set-adopter-merkle-root.js --network synthos
```

Set real asset pools as JSON:

```powershell
$env:DEX_POOLS_JSON='[
  {"symbol":"B12","address":"0xAssetToken","syn":"10000000","asset":"50000","decimals":18},
  {"symbol":"NGOT","address":"0xAssetToken","syn":"5000000","asset":"100000","decimals":18}
]'
```

The deployment script will refuse to invent mock assets on production networks.

## Deploy

```powershell
cd contracts
npm run deploy:synthos:local
```

For a real network:

```powershell
npx hardhat run scripts/deploy-synthos.js --network synthos
```

The script writes:

```text
contracts/deployments/latest.json
```

## Open The Live DEX

Open:

```text
THE_COLLECTIVE_DEX_LIVE.html
```

Paste the deployed `SYNTHOSDex` address from `contracts/deployments/latest.json`, connect wallet, load pools, approve input token, and swap.

## Do Not Claim Mainnet Live Until

- Contracts are deployed to the intended network.
- `SYNTHOSDex` address is published.
- Initial pools are funded with real assets.
- LP wallet / lock terms are published.
- Website points to the deployed DEX address.
- Independent review/audit is complete or the launch is clearly labeled beta/testnet.

## Honest Launch Language

Safe today:

> SYNTHOS Collective DEX contracts are built and ready for deployment testing.

Safe after testnet deployment:

> SYNTHOS Collective DEX is live on testnet.

Safe after production deployment, liquidity, and public address publication:

> SYNTHOS Collective DEX is live.
