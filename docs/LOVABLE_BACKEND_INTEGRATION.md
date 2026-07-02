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
```

The current deployed Lovable app uses Supabase auth and generated server functions. Keep Supabase auth if you want site login, but route SYNTHOS-specific node provisioning and status data through this backend.
