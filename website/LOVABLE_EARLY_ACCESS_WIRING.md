# Lovable Early Access Wiring

I checked the live `www.ishamwilliamsblockchains.com` bundle. The current
deployed route map includes:

```text
/
/deploy
/status
/pitch-deck
/whitepaper
/contact
```

There is no deployed `/early-access` route in the current bundle.

## What To Add In Lovable

Create a new authenticated page:

```text
/early-access
```

Add one container where the widget should render:

```html
<div data-synthos-early-access></div>
```

After deploying the sale contracts to the public EVM network, load the
integration script with the deployed addresses:

```html
<script>
  window.SYNTHOS_EARLY_ACCESS_CONFIG = {
    chainId: 1,
    chainName: "Ethereum",
    rpcUrls: ["https://eth.llamarpc.com"],
    saleContract: "<the deployed SYNTHOSEarlyAdopterSale address>",
    complianceRegistry: "<the deployed SYNTHOSComplianceRegistry address>",
    treasuryWallet: "0xdAE5DF4807274D7a115bB5078c94b023453A05F5",
    assets: [
      { symbol: "USDC", address: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", decimals: 6, usdPrice: "1.00" },
      { symbol: "USDT", address: "0xdAC17F958D2ee523a2206206994597C13D831ec7", decimals: 6, usdPrice: "1.00" },
      { symbol: "WETH", address: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", decimals: 18, usdPrice: "<current ETH/USD price>" },
      { symbol: "WBTC", address: "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599", decimals: 8, usdPrice: "<current BTC/USD price>" },
      { symbol: "ETH", native: true, decimals: 18, usdPrice: "<current ETH/USD price>" }
    ]
  };
</script>
<script src="/assets/early-access-sale.js"></script>
```

The sale contract must be deployed on the same EVM network as the configured
payment assets. Native BTC cannot trigger this EVM sale automatically; use WBTC
for automatic Bitcoin exposure.

## Automatic Flow

The widget does not use manual payment confirmation. It performs:

1. Connect wallet.
2. Check `SYNTHOSComplianceRegistry.eligibleToReceive(wallet, Community)`.
3. Quote SYN at `$0.05`.
4. Approve ERC-20 payment asset when needed.
5. Call `buyWithToken` or `buyWithNative`.
6. SYN is delivered in the same transaction.

## Current Public Blocker

The repo currently only has a local Hardhat rehearsal deployment for the sale
contract. Do not use the Hardhat address on the public website. Deploy
`SYNTHOSEarlyAdopterSale` to the intended public EVM network first, then put the
real sale and compliance-registry addresses into the Lovable config above.
