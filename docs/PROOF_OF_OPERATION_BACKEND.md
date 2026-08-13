# SYNTHOS Proof-of-Operation Backend

## Definition

**Proof-of-Operation is a SYNTHOS-specific bootstrap eligibility system, not a
consensus algorithm and not a known external standard.** It answers one launch
question: did this operator actually keep a SYNTHOS node online and proving
itself over time?

In this phase, a node moves through three states:

1. `registered` — the operator created a candidate record and registered a
   public key or bootstrap identity.
2. `proving` — the backend has received fresh heartbeat proof.
3. `eligible` — the node has completed the required reward epoch with verified
   operation.

Registration is never enough for rewards. Clicking the website button may start
onboarding, but payment eligibility requires real operation.

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
If a public website build cannot yet generate an Ed25519 key, the backend may
create a hosted bootstrap proof session so the user can continue onboarding.
Hosted bootstrap sessions are **not reward eligible** and must be rotated to a
real locally generated key with signed heartbeats before validator payment.

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

## Verified Operation Mechanics

For reviewer diligence, "verified operation" means:

- the node has a registered Ed25519 public key;
- every heartbeat is signed by the matching private key;
- the heartbeat uses the canonical `SYNTHOS_HEARTBEAT_V1` serialization above;
- the heartbeat includes the node ID, height, tip, state root, timestamp, and
  nonce;
- nonces must increase, so replayed heartbeats are rejected;
- the node must keep sending fresh heartbeat proofs near the 15-second target;
- the backend only accumulates verified uptime from accepted heartbeats;
- stale gaps are capped and do not create unlimited backdated uptime;
- reward eligibility requires one complete reward epoch of verified uptime.

The current public backend uses a 30-day reward epoch and a two-minute freshness
window for heartbeat accounting. Those values are protocol parameters and should
be published before any production reward period starts.

## Read Node Status

`GET /api/nodes/:id/status`

Returns the node proof status, verified uptime, last heartbeat, height, and
validator reward policy.

## Read Network Status

`GET /api/network/status`

Returns live node counts from fresh signed heartbeats only. Registered nodes do
not count as running until they heartbeat.

## Explorer Endpoints

The public backend also exposes explorer-style endpoints:

- `GET /api/explorer/status`
- `GET /api/explorer/blocks`
- `GET /api/explorer/mempool`

If `SYNTHOS_RPC_URL` is configured, these endpoints proxy the canonical RPC node.
If no RPC node is attached, they return signed heartbeat checkpoints and active
node counts from the registry. That fallback is useful for launch visibility,
but it is not a substitute for a full canonical block explorer.

## Validator Reward Policy

Validator rewards are paid in SYN one month in arrears:

- base reward: 5,000 SYN per verified month;
- performance bonus cap: up to 2,500 SYN per verified month;
- maximum monthly validator reward: 7,500 SYN;
- first eligibility requires one full month of verified validator uptime;
- no payment is made for clicking the button;
- hosted bootstrap proof sessions do not qualify for rewards.

## Key Custody Rule

Production node private keys must be generated client-side or in local node
software. The backend should receive public keys and signed heartbeats only.
Any legacy candidate or provision path that generated a key server-side is
bootstrap-only and should be treated as requiring key rotation before rewards.
