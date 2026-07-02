# SYNTHOS Cloudless Network

SYNTHOS runs through the self-hosted network path:

1. Self-hosted SYNTHOS registry for peer discovery.
2. `synthosd` validator nodes connected over TCP.
3. Optional HTTP relay compatibility for outbound-only nodes.
4. Public websites and dashboards pointed at self-hosted RPC endpoints.

Managed edge adapters have been removed from the launch path.

## Self-hosted components

| Component | Self-hosted implementation |
| --- | --- |
| Worker peer registry | `go run ./cmd/cloudless-registry` |
| Worker validator endpoints | `go run ./cmd/synthosd` or built `synthosd` binaries |
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

1. Keep Worker code frozen as legacy.
2. Run `go run ./cmd/l1netcheck` on every release.
3. Start at least one self-hosted registry.
4. Start at least four `synthosd` validators using static TCP peers.
5. Register public RPC endpoints with the cloudless registry.
6. Point website config at self-hosted RPC endpoints.
7. Remove Worker URLs from public pages.
8. Archive Worker deployment docs after the self-hosted site is verified.

## Operator reward language

Running the registry or node software does not by itself guarantee rewards. Reward eligibility still follows `docs/TOKENOMICS.md`: one reward stream per verified immune node operator, 500 SYN early verified operator reward, and 1,000 SYN monthly heartbeat reward paid in arrears after verified uptime.
