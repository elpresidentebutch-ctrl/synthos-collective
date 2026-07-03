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

Verify:

```powershell
Invoke-RestMethod http://127.0.0.1:8080/health
Invoke-RestMethod http://127.0.0.1:8080/status
Invoke-RestMethod http://127.0.0.1:8080/blocks
```

## Open Public RPC

Run the generated Docker Compose stack on a server. Put an HTTPS reverse proxy
in front of validator 1's RPC port:

```text
https://rpc.ishamwilliamsblockchains.com -> http://127.0.0.1:8080
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
```

## Connect Website And Explorer

Set the website RPC URL to:

```text
https://rpc.ishamwilliamsblockchains.com
```

The existing website explorer reads `/status`, `/blocks`, `/mempool`, and
balance endpoints from that RPC.

## Important Boundary

Opening this native SYNTHOS network does not automatically let early adopters
pay with real USDC, USDT, ETH, BTC, or WBTC. Those assets live on EVM networks
unless you bridge or wrap them onto SYNTHOS.

For the crypto pre-sale, deploy `SYNTHOSEarlyAdopterPresale` to an EVM network.
For the SYNTHOS chain itself, run this open-network stack.
