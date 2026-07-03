# Lovable Early Access Wiring

The early adopters section should render the SYN purchase widget into this
container:

```html
<div data-synthos-early-access></div>
```

Load the widget after the container:

```html
<script>
  window.SYNTHOS_API_URL = "https://api.ishamwilliamsblockchains.com";
</script>
<script src="https://api.ishamwilliamsblockchains.com/assets/early-access-sale.js"></script>
```

The backend serves the widget script and sends CORS headers for the website and
Lovable origins.

## Backend Config

The widget fetches:

```http
GET /api/early-access/config
```

That endpoint supplies the first early adopter tranche:

```text
250,000,000 SYN at $0.05 per SYN
$12,500,000 max tranche value
Source bucket: COMMUNITY_EARLY_ADOPTER_CAMPAIGNS
Treasury wallet: 0xdAE5DF4807274D7a115bB5078c94b023453A05F5
RPC URL: https://rpc.ishamwilliamsblockchains.com
```

Production contract values are read from the backend environment:

```bash
SYNTHOS_EARLY_ACCESS_CHAIN_ID=
SYNTHOS_EARLY_ACCESS_CHAIN_NAME=SYNTHOS
SYNTHOS_EARLY_ACCESS_RPC_URLS=https://rpc.ishamwilliamsblockchains.com
SYNTHOS_EARLY_ACCESS_SALE_CONTRACT=
SYNTHOS_EARLY_ACCESS_COMPLIANCE_REGISTRY=
SYNTHOS_EARLY_ACCESS_TREASURY_WALLET=0xdAE5DF4807274D7a115bB5078c94b023453A05F5
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
5. Quote SYN at `$0.05`.
6. Approve ERC-20 payment asset when needed.
7. Call `buyWithToken` or `buyWithNative`.
8. Deliver SYN in the purchase transaction.

Native BTC cannot trigger this EVM sale automatically. Use WBTC for automatic
Bitcoin exposure.
