# SYNTHOS Mailbox Relay Protocol

The mailbox relay lets silent nodes receive work without opening inbound ports.

Relays are a redundant transport layer. They are not consensus, not custody, and not a source of truth.

## Rules

- Nodes never listen.
- Nodes register with `url: ""`.
- Nodes heartbeat outbound to `/register` on every configured relay.
- Nodes poll outbound from `/mailbox?name=<node-id>` on every configured relay.
- Authorized relayers enqueue work with `POST /mailbox`.
- Nodes verify message signatures and chain state locally.
- Important public state is checkpointed on-chain.

## Multi-Relay Mode

Desktop silent nodes use `SYNTHOS_RELAY_URLS`:

```powershell
$env:SYNTHOS_RELAY_URLS="https://relay-1.example,https://relay-2.example,https://relay-3.example"
```

The node deduplicates the list, strips trailing slashes, sends the same heartbeat to each relay, and polls each relay mailbox. Relay errors are logged but do not stop the node.

Browser validators use `PEER_REGISTRY_URLS` in their page code and pass the same relay set to the service worker for background heartbeats.

## Failure Model

A relay can fail without taking SYNTHOS down:

- If one relay is offline, nodes continue polling the rest.
- If one relay censors, other relays can still deliver messages.
- If one relay injects bad data, node-side signature and state checks reject it.
- If all relays are temporarily unavailable, the node keeps local identity and last-known state, then resumes outbound polling later.

The target production posture is several official relays plus community-operated relays. New relays can be added without opening ports on adopter devices.

## Register Silent Node

```http
POST /register
Content-Type: application/json
```

```json
{
  "name": "desktop-abcd1234",
  "url": "",
  "cloud": "desktop-silent",
  "mode": "absolute_silence_outbound_only",
  "inbound_ports": 0,
  "hardware_commitment": "0x1111111111111111111111111111111111111111111111111111111111111111",
  "heartbeat_proof": "0x2222222222222222222222222222222222222222222222222222222222222222"
}
```

## Queue Mail

```http
POST /mailbox
Content-Type: application/json
X-Registry-Secret: <secret if configured>
```

```json
{
  "to": "desktop-abcd1234",
  "type": "command",
  "from": "validator-11",
  "payload": {
    "type": "sync"
  }
}
```

## Poll Mail

```http
GET /mailbox?name=desktop-abcd1234
```

Response:

```json
[
  {
    "id": "...",
    "type": "command",
    "from": "validator-11",
    "payload": { "type": "sync" },
    "created_at": 1780000000000
  }
]
```

Polling drains the mailbox. Messages expire after one hour if not collected.

## Current Implementations

- Self-hosted registry: `cmd/cloudless-registry`
- Go relay transport: `internal/network/relay_transport.go`
- Desktop silent node: `cmd/silentnode/main.go`
- Mobile PWA polling: `workers/mobile-validator/index.html`
- Desktop PWA polling: `workers/desktop-validator/index.html`
