# Lovable Early Access Wiring

The early adopters section should render the SYN purchase widget into this
container:

```html
<div data-synthos-early-access></div>
```

Load the widget after the container:

```html
<script>
  window.SYNTHOS_API_URL = "https://www.ishamwilliamsblockchains.com";
</script>
<script src="https://www.ishamwilliamsblockchains.com/assets/early-access-sale.js"></script>
```

The backend serves the widget script and sends CORS headers for the website and
Lovable origins.
For this same-domain setup, `www.ishamwilliamsblockchains.com` must route
`/api/*` and `/assets/early-access-sale.js` to the SYNTHOS backend.

## Backend Config

The widget fetches:

```http
GET /api/early-access/config
```

That endpoint supplies the first early adopter tranche:

```text
250,000,000 SYN at $0.10 per SYN
$25,000,000 max tranche value
Source bucket: COMMUNITY_EARLY_ADOPTER_CAMPAIGNS
Treasury wallet: 0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2
RPC URL: https://rpc.ishamwilliamsblockchains.com
```

Production contract values are read from the backend environment:

```bash
SYNTHOS_EARLY_ACCESS_CHAIN_ID=
SYNTHOS_EARLY_ACCESS_CHAIN_NAME=SYNTHOS
SYNTHOS_EARLY_ACCESS_RPC_URLS=https://rpc.ishamwilliamsblockchains.com
SYNTHOS_EARLY_ACCESS_SALE_CONTRACT=
SYNTHOS_EARLY_ACCESS_COMPLIANCE_REGISTRY=
SYNTHOS_EARLY_ACCESS_TREASURY_WALLET=0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2
SYNTHOS_EARLY_ACCESS_ASSETS_JSON=[]
SYNTHOS_CORS_ORIGINS=https://www.ishamwilliamsblockchains.com,https://ishamwilliamsblockchains.com,https://lovable.dev
SYNTHOS_EARLY_ACCESS_WIDGET_PATH=/website/assets/early-access-sale.js
```

Leave the sale contract and compliance registry empty until the public contracts
are deployed. The widget disables live purchases when either address is missing
or when a local Hardhat chain is configured.

## Automatic Flow

The widget does not use manual payment confirmation. It performs:

1. Connect wallet.
2. Switch to the configured EVM chain when needed.
3. Check `SYNTHOSComplianceRegistry.eligibleToReceive(wallet, Community)`.
4. If self-registration is open, call `selfRegisterCommunity`.
5. Quote SYN at `$0.10`.
6. Approve ERC-20 payment asset when needed.
7. Call `buyWithToken` or `buyWithNative`.
8. Deliver SYN in the purchase transaction.

Native BTC cannot trigger this EVM sale automatically. Use WBTC for automatic
Bitcoin exposure.
