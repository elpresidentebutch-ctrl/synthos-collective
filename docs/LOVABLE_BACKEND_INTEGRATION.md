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

export async function registerNodeCandidate(input) {
  const response = await fetch(`${API_BASE}/api/nodes/register`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}

export async function getNodeStatus(nodeId) {
  const response = await fetch(`${API_BASE}/api/nodes/${encodeURIComponent(nodeId)}/status`, {
    cache: "no-store",
  });
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}

export async function getExplorerStatus() {
  const response = await fetch(`${API_BASE}/api/explorer/status`, { cache: "no-store" });
  if (!response.ok) throw new Error(`status ${response.status}`);
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





The current deployed Lovable app uses Supabase auth and generated server functions. Keep Supabase auth if you want site login, but route SYNTHOS-specific node provisioning and status data through this backend.

`POST /api/nodes/provision` remains available only for old compatibility flows.
New website code should use `/api/nodes/register`, local/client-side key
generation, `/api/nodes/:id/status`, and signed `/api/nodes/heartbeat`.
Compatibility sessions that do not prove a real Ed25519 key are not reward
eligible.
