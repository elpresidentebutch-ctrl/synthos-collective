# Open The SYNTHOS Network

This runbook opens a founder-operated SYNTHOS L1 network using the existing
`synthosd` node, a shared genesis file, and four validator configs.

This is the SYNTHOS native network. Use it when you want to open the SYNTHOS
ledger, RPC, block data, balances, and validator set.

## Generate Network Files

From the repo root:

```powershell
go run ./cmd/opennet
```

This writes private launch material to:

```text
.synthos/open-network/
```

Generated files include:

- `genesis.json`
- `founder-wallet.private.json`
- `validator-wallets.private.json`
- `validator-1.json` through `validator-4.json`
- `docker-compose.yml`
- `manifest.public.json`

Do not commit `.synthos/open-network/`. It contains private keys.

## Start Locally

```powershell
cd .synthos/open-network
docker compose up --build
```

Local RPC endpoints:

```text
http://127.0.0.1:8080
http://127.0.0.1:8081
http://127.0.0.1:8082
http://127.0.0.1:8083
```

```

## Start Validators 11-15 Locally

To run the current five-validator cloudless test network from the repo root:

```powershell
powershell.exe -ExecutionPolicy Bypass -File .\scripts\start-validator-11-15.ps1
```

This command generates private network material under:

```text
.synthos/open-network-11-15/
```

That folder is ignored by Git because it contains validator private keys.

The launcher starts validators 11 through 15 with:

- one shared genesis document;
- five Ed25519 validator keys;
- a validator registry containing all five public keys;
- provider-neutral HTTP peer catch-up;
- validator 11 producing empty heartbeat blocks every 15 seconds;
- persistent Docker volumes for every validator; and
- a verification step that confirms all five nodes are healthy, expose the seven core capabilities, and converge to the same height/tip/state root.

Local RPC endpoints:

```text
http://127.0.0.1:8111
http://127.0.0.1:8112
http://127.0.0.1:8113
http://127.0.0.1:8114
http://127.0.0.1:8115
```

Re-run verification:

```powershell
node .\scripts\verify-validator-11-15.mjs
```

Stop the network:

```powershell
powershell.exe -ExecutionPolicy Bypass -File .\scripts\stop-validator-11-15.ps1
```

## Public Node Requirement

The previous local-only push-button node path has been removed. A node should
not be described as part of the public SYNTHOS network unless it has a stable
public peer endpoint and can be reached by the rest of the validator set.

For now, this runbook keeps the verified validators 11 through 15 local and
explicit. The next public-network implementation should add real public peer
addresses, operator keys, persistent storage, and deployment verification before
website copy calls it a live public node.

## Open Public RPC

Run the generated Docker Compose stack on a server. Put an HTTPS reverse proxy
in front of validator 1's RPC port:

```text
https://rpc.ishamwilliamsblockchains.com -> http://127.0.0.1:8080
```

```

## Deployment (Direct Host / VPS)

Deploy using Docker or a direct systemd service on your host or VPS. Render hosting has been decommissioned.

## Connect Website And Explorer

Set the website RPC URL to:

```text
https://rpc.ishamwilliamsblockchains.com
```

Ethereum mainnet using a public RPC. Before accepting large payments, replace
the public RPC with a provider you control, confirm current ETH pricing, and run
a small payment test.

For BTC exposure, use a wrapped token on an EVM network or a dedicated BTC
payment processor/watcher. Native BTC is not verified by this EVM payment rail.
