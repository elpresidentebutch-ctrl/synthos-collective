# Open The SYNTHOS Network

This runbook opens a founder-operated SYNTHOS L1 network using the existing
`synthosd` node, a shared genesis file, and four validator configs.

This is the SYNTHOS native network. It is separate from the EVM pre-sale
contract path. Use the EVM pre-sale contract when you want to collect USDC,
USDT, ETH, WETH, or WBTC. Use this network when you want to open the SYNTHOS
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
- `early-access.env`
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

Local early-access backend:

```text
http://127.0.0.1:8090
```

Verify:

```powershell
Invoke-RestMethod http://127.0.0.1:8080/health
Invoke-RestMethod http://127.0.0.1:8080/status
Invoke-RestMethod http://127.0.0.1:8080/blocks
Invoke-RestMethod http://127.0.0.1:8090/api/early-access/config
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

## Start A Push-Button Node

For a website button or one-line install, use the GitLab-hosted bootstrap
script. This free path does not require Render, a VPS, or Cloudflare:

```powershell
irm https://gitlab.com/synthos-collective-group/synthos-collective/-/raw/main/scripts/install-push-button-node.ps1 | iex
```

That command clones or updates the repo under:

```text
%USERPROFILE%\Documents\SYNTHOS\synthos-collective
```

Then it starts the local validator network if needed and starts the local
push-button node.

After validators 11 through 15 are running, start a local non-validator
SYNTHOS node with one command:

```powershell
powershell.exe -ExecutionPolicy Bypass -File .\scripts\start-push-button-node.ps1
```

If the validator network is not already running, use:

```powershell
powershell.exe -ExecutionPolicy Bypass -File .\scripts\start-push-button-node.ps1 -StartValidators
```

The push-button node creates local private material under:

```text
.synthos/push-button-node/
```

That folder is ignored by Git. It contains the node's Ed25519 private key,
local config, persistent data, and process logs.

The node:

- generates or reuses its own Ed25519 identity;
- keeps the private key local;
- connects to validators 11 through 15 over provider-neutral HTTP peer sync;
- exposes local RPC on `http://127.0.0.1:8120`;
- exposes `/health`, `/status`, `/capabilities`, and `/aen/status`;
- presents the seven core SYNTHOS capabilities; and
- verifies that it reaches the same height, tip, and state root as the validator network.

Verify it again:

```powershell
node .\scripts\verify-push-button-node.mjs
```

Stop only the push-button node:

```powershell
powershell.exe -ExecutionPolicy Bypass -File .\scripts\stop-push-button-node.ps1
```

## Open Public RPC

Run the generated Docker Compose stack on a server. Put an HTTPS reverse proxy
in front of validator 1's RPC port:

```text
https://rpc.ishamwilliamsblockchains.com -> http://127.0.0.1:8080
```

Put the early-access backend behind HTTPS too:

```text
https://api.ishamwilliamsblockchains.com -> http://127.0.0.1:8090
```

At your domain provider, point:

```text
rpc.ishamwilliamsblockchains.com
```

to the server running the RPC.

Verify public access:

```powershell
Invoke-RestMethod https://rpc.ishamwilliamsblockchains.com/health
Invoke-RestMethod https://rpc.ishamwilliamsblockchains.com/status
Invoke-RestMethod https://api.ishamwilliamsblockchains.com/api/early-access/config
```

## Render Blueprint

The repo root includes `render.yaml`, so Render can configure this repository as
a Blueprint. In Render:

1. Create a new Blueprint.
2. Connect `elpresidentebutch-ctrl/synthos-collective`.
3. Select branch `codex/security-foundation`.
4. Render will read `render.yaml`.
5. Fill the private values Render asks for:
   - `SYNTHOS_EARLY_ACCESS_ALLOCATION_PRIVATE_KEY`
   - `SYNTHOS_EARLY_ACCESS_ASSETS_JSON`

Use your local file for those private values:

```text
.synthos/early-access-keys/early-access.env
```

Do not commit or upload that env file publicly.

## Connect Website And Explorer

Set the website RPC URL to:

```text
https://rpc.ishamwilliamsblockchains.com
```

Set the early-access widget backend URL to:

```text
https://api.ishamwilliamsblockchains.com
```

The existing website explorer reads `/status`, `/blocks`, `/mempool`, and
balance endpoints from that RPC.

## Important Boundary

The generated `early-access.env` turns on ETH, USDC, and USDT payment checks on
Ethereum mainnet using a public RPC. Before accepting large payments, replace
the public RPC with a provider you control, confirm current ETH pricing, and run
a small payment test.

For BTC exposure, use a wrapped token on an EVM network or a dedicated BTC
payment processor/watcher. Native BTC is not verified by this EVM payment rail.
