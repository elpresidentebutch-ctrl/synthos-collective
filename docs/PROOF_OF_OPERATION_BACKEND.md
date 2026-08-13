# SYNTHOS Proof-of-Operation Backend

The website should use `VITE_SYNTHOS_BACKEND_URL` to call the SYNTHOS
cloudless registry backend.

## Register a Node Candidate

`POST /api/nodes/register`

```json
{
  "node_id": "validator-example",
  "public_key": "32-byte-ed25519-public-key-hex",
  "role": "validator_candidate",
  "endpoint": "https://validator.example.com",
  "network": "mainnet",
  "capabilities": ["ed25519", "validator_registry"]
}
```

Registration does not create reward eligibility. It only records a candidate.

## Submit a Signed Heartbeat

`POST /api/nodes/heartbeat`

```json
{
  "node_id": "validator-example",
  "height": 123,
  "tip": "0xabc",
  "state_root": "0xdef",
  "timestamp": "2026-08-13T12:00:00Z",
  "nonce": "0000000001",
  "signature": "64-byte-ed25519-signature-hex",
  "capabilities": ["ed25519", "validator_registry"]
}
```

The signed message is:

```text
SYNTHOS_HEARTBEAT_V1
node_id=<node_id>
height=<height>
tip=<tip>
state_root=<state_root>
timestamp=<timestamp>
nonce=<nonce>
```

The backend rejects missing signatures, invalid Ed25519 signatures, and
non-increasing nonces.

## Read Node Status

`GET /api/nodes/:id/status`

Returns the node proof status, verified uptime, last heartbeat, height, and
validator reward policy.

## Read Network Status

`GET /api/network/status`

Returns live node counts from fresh signed heartbeats only. Registered nodes do
not count as running until they heartbeat.

## Validator Reward Policy

Validator rewards are paid in SYN one month in arrears:

- base reward: 5,000 SYN per verified month;
- performance bonus cap: up to 2,500 SYN per verified month;
- maximum monthly validator reward: 7,500 SYN;
- first eligibility requires one full month of verified validator uptime;
- no payment is made for clicking the button.
