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

### Register Node Candidate

```http
POST /api/nodes/register
Content-Type: application/json

{
  "node_id": "syn-node-example",
  "public_key": "32-byte-ed25519-public-key-hex",
  "role": "validator_candidate",
  "endpoint": "https://node.example.com",
  "network": "mainnet"
}
```

Registers a node candidate. The backend does not need and should not receive the
operator's private key. Client-side or local node software should generate and
store the node key, then submit signed heartbeat proofs.

### Submit Heartbeat Proof

```http
POST /api/nodes/heartbeat
Content-Type: application/json

{
  "node_id": "syn-node-example",
  "height": 123,
  "tip": "0xabc",
  "state_root": "0xdef",
  "timestamp": "2026-08-13T12:00:00Z",
  "nonce": "0000000001",
  "signature": "64-byte-ed25519-signature-hex",
  "capabilities": [
    "ed25519",
    "canonical_serialization",
    "validator_registry",
    "proposal_rotation",
    "quorum",
    "replay_protection",
    "persistent_storage"
  ]
}
```

The signature is over the canonical heartbeat message described in
`docs/PROOF_OF_OPERATION_BACKEND.md`.

### Legacy Provision Node

```http
POST /api/nodes/provision
```

This endpoint is retained for compatibility only. It prepares a node candidate
record and returns public metadata. Production key custody should be client-side
or local CLI based; backend-generated private keys must not be used for public
validator custody.

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
- `treasuryWallet`: `0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2` unless overridden
- `paymentRails`: `true`
- `paymentIntentUrl`: `/api/early-access/payment-intents`

Production deployment values are read from environment variables:

```bash
SYNTHOS_EARLY_ACCESS_CHAIN_ID=
SYNTHOS_EARLY_ACCESS_CHAIN_NAME=SYNTHOS
SYNTHOS_EARLY_ACCESS_RPC_URLS=https://rpc.ishamwilliamsblockchains.com
SYNTHOS_EARLY_ACCESS_SALE_CONTRACT=
SYNTHOS_EARLY_ACCESS_COMPLIANCE_REGISTRY=
SYNTHOS_EARLY_ACCESS_TREASURY_WALLET=0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2
SYNTHOS_EARLY_ACCESS_ASSETS_JSON=[]
SYNTHOS_EARLY_ACCESS_ALLOCATION_PRIVATE_KEY=
SYNTHOS_NATIVE_RPC_URL=https://rpc.ishamwilliamsblockchains.com
SYNTHOS_CORS_ORIGINS=https://www.ishamwilliamsblockchains.com,https://ishamwilliamsblockchains.com,https://lovable.dev
SYNTHOS_EARLY_ACCESS_WIDGET_PATH=/website/assets/early-access-sale.js
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

### Early Access Payment Intent

```http
POST /api/early-access/payment-intents
Content-Type: application/json

{
  "buyerWallet": "0xEVMBuyerWallet",
  "synthosAddress": "0xSYNTHOSRecipient",
  "assetSymbol": "USDC",
  "usdValue": "25.00"
}
```

Returns a payment intent with the treasury address, exact base-unit payment
amount, and SYN allocation at `$0.05` per SYN.

After the buyer sends the crypto payment, verify it:

```http
POST /api/early-access/payment-intents/{id}/verify
Content-Type: application/json

{
  "txHash": "0x..."
}
```

The backend verifies the EVM payment transaction. If
`SYNTHOS_EARLY_ACCESS_ALLOCATION_PRIVATE_KEY` and `SYNTHOS_NATIVE_RPC_URL` are
configured, it then submits the SYN allocation transaction on the SYNTHOS native
network.

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
  window.SYNTHOS_API_URL = "https://www.ishamwilliamsblockchains.com";
</script>
<script src="https://www.ishamwilliamsblockchains.com/assets/early-access-sale.js"></script>
```

For this same-domain setup, the `www` host must route `/api/*` and
`/assets/early-access-sale.js` to the SYNTHOS backend.

The current deployed Lovable app uses Supabase auth and generated server functions. Keep Supabase auth if you want site login, but route SYNTHOS-specific node provisioning and status data through this backend.
