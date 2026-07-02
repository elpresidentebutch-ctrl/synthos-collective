# Lovable Backend Integration

The SYNTHOS website backend is served by the cloudless registry:

```bash
go run ./cmd/cloudless-registry -listen :8090
```

For production, run the same binary behind HTTPS and set the Lovable app's API base URL to that public backend origin.

## API Endpoints

### Network Status

```http
GET /api/network/status
```

Returns the node totals, validator and immune-node counts, fresh heartbeat count, and the latest registry view.

### Node List

```http
GET /api/nodes
```

Returns all registered nodes in the peer registry.

### Provision Node

```http
POST /api/nodes/provision
Content-Type: application/json

{
  "kind": "validator",
  "network": "testnet",
  "label": "genesis-01"
}
```

Returns an Ed25519 node identity and a ready-to-save node config. The private key is returned once and is not persisted by the registry.

### Contact

```http
POST /api/contact
Content-Type: application/json

{
  "name": "Operator Name",
  "email": "operator@example.com",
  "topic": "Running a node",
  "message": "I want to join the network."
}
```

Stores the contact message in the registry state file and returns a reference ID.

### Early Access Config

```http
GET /api/early-access/config
```

Returns the launch-ready early adopter sale configuration for the website:

- `tokenPriceUsd`: `0.05`
- `activeTrancheSyn`: `250,000,000`
- `maxTrancheUsd`: `$12,500,000`
- `communitySourceBucket`: `COMMUNITY_EARLY_ADOPTER_CAMPAIGNS`
- `treasuryWallet`: `0xdAE5DF4807274D7a115bB5078c94b023453A05F5` unless overridden

Production deployment values are read from environment variables:

```bash
SYNTHOS_EARLY_ACCESS_CHAIN_ID=
SYNTHOS_EARLY_ACCESS_CHAIN_NAME=SYNTHOS
SYNTHOS_EARLY_ACCESS_RPC_URLS=https://rpc.ishamwilliamsblockchains.com
SYNTHOS_EARLY_ACCESS_SALE_CONTRACT=
SYNTHOS_EARLY_ACCESS_COMPLIANCE_REGISTRY=
SYNTHOS_EARLY_ACCESS_TREASURY_WALLET=0xdAE5DF4807274D7a115bB5078c94b023453A05F5
SYNTHOS_EARLY_ACCESS_ASSETS_JSON=[]
```

`SYNTHOS_EARLY_ACCESS_ASSETS_JSON` should contain the live payment assets for the same EVM chain as the sale contract. Example shape:

```json
[
  { "symbol": "USDC", "address": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", "decimals": 6, "usdPrice": "1.00" },
  { "symbol": "USDT", "address": "0xdAC17F958D2ee523a2206206994597C13D831ec7", "decimals": 6, "usdPrice": "1.00" },
  { "symbol": "WETH", "address": "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", "decimals": 18, "usdPrice": "2000.00" },
  { "symbol": "WBTC", "address": "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599", "decimals": 8, "usdPrice": "60000.00" },
  { "symbol": "ETH", "native": true, "decimals": 18, "usdPrice": "2000.00" }
]
```

Those asset addresses are Ethereum mainnet addresses. If the SYNTHOS sale is deployed on a different EVM chain, use that chain's real token or wrapped-token addresses instead.

### Windows Installer

```http
GET /api/node/windows-installer.ps1
```

Returns a small bootstrap installer that points operators at the local SYNTHOS node installer.

## Lovable Wiring

In Lovable, replace the existing node provisioning and contact server functions with fetch calls to the backend origin:

```js
const API_BASE = import.meta.env.VITE_SYNTHOS_API_URL || "http://127.0.0.1:8090";

export async function getNetworkStatus() {
  const response = await fetch(`${API_BASE}/api/network/status`, { cache: "no-store" });
  if (!response.ok) throw new Error(`status ${response.status}`);
  return response.json();
}

export async function provisionNode(input) {
  const response = await fetch(`${API_BASE}/api/nodes/provision`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}

export async function submitContact(input) {
  const response = await fetch(`${API_BASE}/api/contact`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}

export async function getEarlyAccessConfig() {
  const response = await fetch(`${API_BASE}/api/early-access/config`, { cache: "no-store" });
  if (!response.ok) throw new Error(`status ${response.status}`);
  return response.json();
}
```

For the early adopter section, set the API origin and load the widget:

```html
<div data-synthos-early-access></div>
<script>
  window.SYNTHOS_API_URL = window.location.origin;
</script>
<script src="/assets/early-access-sale.js"></script>
```

The current deployed Lovable app uses Supabase auth and generated server functions. Keep Supabase auth if you want site login, but route SYNTHOS-specific node provisioning and status data through this backend.
