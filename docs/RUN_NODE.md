## Running a SYNTHOS node (v0)

This guide shows how to run a single SYNTHOS node using the configurable `synthosd` binary. Networking is still local-only (no Internet P2P), but the structure is in place so that when a real transport is added, others can join the same chain using the same config and genesis files.

### 1. Build / run synthosd

From the repo root:

```bash
go run ./cmd/synthosd
```

By default this looks for:

- Node config: `config/node.json`
- Genesis: path inside `config/node.json` (e.g. `config/genesis.json`)

You can override the node config path with:

```bash
SYNTHOS_CONFIG=path/to/your-node.json go run ./cmd/synthosd
```

### 2. Configure a node

Copy the example config:

```bash
cp config/node.example.json config/node.json
cp config/genesis.example.json config/genesis.json
```

Edit `config/node.json`:

- `node_id`: unique ID for this node (e.g. `"node-1"`).
- `data_dir`: where chain snapshots are stored.
- `is_validator`: `true` if this node should act as a validator for now.
- `rpc_listen`: HTTP address, e.g. `":8080"`.
- `listen_addr`: TCP listen address for consensus messages, e.g. `":9001"`.
- `genesis_path`: `"config/genesis.json"`.
- `peers`: list of `"agentID@host:port"` entries for peers (for example, `"node-2@127.0.0.1:9002"`).

Edit `config/genesis.json`:

- `chain_id`: chain identifier (all nodes must share this).
- `alloc`: map of addresses to initial balances.
- `symbol` / `decimals`: metadata for the native token.

### 3. Interact with the node

When `synthosd` is running, it exposes the same HTTP API as `cmd/rpcnode` on the configured `rpc_listen` address:

- `GET /health` — lightweight liveness (`{"ok":true,"service":"synthos-rpc"}`)
- `GET /status`
- `GET /balance?address=0x...`
- `GET /mempool`
- `POST /submitTx` (JSON body)

### 4. Two-node local setup (example)

You can run two nodes on your machine as follows:

- Node 1 config:
  - `node_id`: `"node-1"`
  - `listen_addr`: `":9001"`
  - `peers`: `["node-2@127.0.0.1:9002"]`
- Node 2 config (copy `node.json` to `node2.json` and adjust):
  - `node_id`: `"node-2"`
  - `listen_addr`: `":9002"`
  - `peers`: `["node-1@127.0.0.1:9001"]`

Start them in two terminals:

```bash
SYNTHOS_CONFIG=config/node.json go run ./cmd/synthosd
SYNTHOS_CONFIG=config/node2.json go run ./cmd/synthosd
```

Both nodes share the same `genesis.json` and exchange consensus messages over TCP.

### 5. Decentralization readiness

This layout (genesis + node config + long-lived `synthosd` process) means:

- You can define a shared genesis file and distribute it to others.
- Each operator can have their own `node.json` pointing at that genesis.
- Multiple machines can now join the same chain and participate in consensus using shared genesis and appropriate `peers` / `listen_addr` settings in their node configs.

---

## Legal notice

SYNTHOS Collective, SYNTHOS, and related names, marks, documentation, and technical materials in this document are the **exclusive property of James G. Isham Williams, Sr.** Unauthorized reproduction, distribution, or commercial use without express written permission is prohibited except as allowed under applicable open-source licenses for identified files. No rights are waived.

This document is informational only and is not legal, financial, or investment advice. The canonical legal notice is in **docs/LEGAL_NOTICE.md** in the SYNTHOS Collective repository.
