# SYNTHOS Cloudless Network

## Proof-of-Operation definition

Proof-of-Operation is a SYNTHOS-specific bootstrap eligibility system. It is not
the consensus algorithm and should not be presented as a generic industry term.
It proves that an operator registered a node identity, controls the node key,
and keeps sending valid signed heartbeat proofs over time.

The public push-button node flow starts onboarding. It does not by itself create
reward eligibility. Validator rewards require real Ed25519 signed heartbeats and
one full month of verified uptime.

SYNTHOS currently runs through a bootstrap network path:

1. Self-hosted SYNTHOS registry for peer discovery.
2. `synthosd` validator nodes connected over TCP.
3. Optional HTTP relay compatibility for outbound-only nodes.
4. Public websites and dashboards pointed at self-hosted RPC endpoints.

Managed edge adapters have been removed from the current launch path. Earlier
experiments used provider-managed relays/object storage. Those experiments are
not the permanent trust model and must not be described as fully cloudless,
un-DDoSable, or censorship-proof.

## Current maturity statement

The current registry/relay layer is bootstrap infrastructure for discovery,
operator onboarding, outbound-only nodes, and website status. It is not a
production decentralized gossip layer by itself.

Until SYNTHOS has multiple independently operated relays plus direct validator
peer gossip, the project should say:

- nodes can run without inbound ports;
- node messages and heartbeats are signed and independently verifiable;
- the registry/relay does not own private keys or chain state; and
- relay/registry hosting remains an operational dependency during bootstrap.

The project should not claim:

- no single point of failure;
- no address to attack;
- permanent censorship resistance at the transport layer; or
- an un-DDoSable network.

## Self-hosted components

| Component | Self-hosted implementation |
| --- | --- |
| Peer registry / relay | `go run ./cmd/cloudless-registry` |
| Validator endpoints | `go run ./cmd/synthosd` or built `synthosd` binaries |
| Block relay | Self-hosted RPC `/status`, `/blocks`, and peer registry |
| Public site backend | Static site pointed at self-hosted RPC URLs |
| Message/state storage | Local registry JSON state plus node disk state |

## Start a self-hosted registry

From the repository root:

```bash
go run ./cmd/cloudless-registry -listen :8090
```

Health check:

```bash
curl http://127.0.0.1:8090/health
```

Register a reachable node:

```bash
curl -X POST http://127.0.0.1:8090/register \
  -H "Content-Type: application/json" \
  -d '{"name":"validator-0","url":"http://127.0.0.1:8080","cloud":"cloudless","mode":"reachable","inbound_ports":1}'
```

List active peers:

```bash
curl http://127.0.0.1:8090/peers/active
```

## Run a local proof network

The current proof command launches four real validator processes, connects them over TCP, submits a signed transaction, finalizes a block, restarts one validator, and verifies convergence:

```bash
go run ./cmd/l1netcheck
```

That is the fastest evidence that the network can operate through the self-hosted path.

## Run a long-lived node

Create a node config using the shape in `config/node.example.json`, then run:

```bash
set SYNTHOS_CONFIG=config/node.json
go run ./cmd/synthosd
```

On PowerShell:

```powershell
$env:SYNTHOS_CONFIG = "config/node.json"
go run ./cmd/synthosd
```

## Use registry discovery for HTTP/outbound nodes

The existing `cmd/rpcnode` can register with the cloudless registry because it speaks the same `/register`, `/peers/active`, and `/mailbox` protocol as the old Worker registry.

PowerShell example:

```powershell
$env:REGISTRY_URL = "http://127.0.0.1:8090"
$env:WORKER_NAME = "cloudless-rpc-1"
$env:SELF_URL = "http://127.0.0.1:8080"
$env:PORT = "8080"
go run ./cmd/rpcnode
```

The variable `WORKER_NAME` is kept for backward compatibility. Treat it as the node name.

## Registry endpoints

The cloudless registry implements the compatible API:

| Endpoint | Purpose |
| --- | --- |
| `GET /health` | Registry liveness |
| `POST /register` | Register or heartbeat a node |
| `GET /peers` | List known peers |
| `GET /peers/active` | List peers seen in the last five minutes |
| `GET /mailbox?name=NODE` | Poll messages for outbound-only nodes |
| `POST /mailbox` | Queue a message for an outbound-only node |
| `DELETE /peers/NODE` | Remove a peer |

Set `REGISTRY_SECRET` when running the registry to protect mailbox writes and peer deletion:

```bash
REGISTRY_SECRET=change-me go run ./cmd/cloudless-registry -listen :8090
```

Clients then send:

```text
X-Registry-Secret: change-me
```

## Migration plan

1. Keep provider-specific Worker/R2 code frozen as legacy bootstrap material.
2. Run `go run ./cmd/l1netcheck` on every release.
3. Start at least one self-hosted registry.
4. Add at least two more independently operated registries/relays on different
   providers or community hosts.
5. Start at least four `synthosd` validators using static TCP peers.
6. Add direct validator-to-validator gossip as the primary consensus transport.
7. Treat relays as discovery/mailbox fallbacks only.
8. Register public RPC endpoints with more than one registry.
9. Point website config at a multi-relay backend list, not a single vendor URL.
10. Remove Worker/R2 URLs from public pages.
11. Archive Worker deployment docs after the multi-relay/self-hosted path is
    verified.

## Operator reward language

Running the registry or node software does not by itself guarantee rewards. Reward eligibility still follows `docs/TOKENOMICS.md`: one reward stream per verified immune node operator, 500 SYN early verified operator reward, and 1,000 SYN monthly heartbeat reward paid in arrears after verified uptime.
