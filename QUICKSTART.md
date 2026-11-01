# SYNTHOS Collective - Quick Start Guide

This guide covers running the production Go and CometBFT node, running a candidate Proof-of-Uptime node, running the local registry, and running the historical Python prototype.

---

## 1. Production Node: CometBFT (`synthos-comet`)

The live Synthos Collective network has operated on CometBFT consensus since October 22, 2026. The production node lives in `cometapp/` and compiles to `synthos-comet`.

### Prerequisites
- Go 1.26 or later (`go version`)
- Git

### Build and Test
```bash
# Clone the repository
git clone https://github.com/elpresidentebutch-ctrl/synthos-collective.git
cd synthos-collective/cometapp

# Run all ABCI and in-process network consensus tests
go test -v ./...

# Build the node binary
go build -trimpath -ldflags="-s -w" -o synthos-comet .
```

### Basic Node Operations

#### 1. Identity & Keys
```bash
# Create or display this node's validator consensus key
./synthos-comet validator-key --home ~/.synthos-comet

# Display node P2P identity
./synthos-comet node-id --home ~/.synthos-comet
```

#### 2. Network Genesis Setup
To join an existing network, `join` requires the network's full CometBFT `genesis.json` (not just an app genesis or validator roster):

- **Joining Testnet**:
  Use the committed testnet genesis document:
  ```bash
  ./synthos-comet join --home ~/.synthos-comet --genesis-file ../networks/synthos-testnet-1/genesis.json
  ```

- **Joining Mainnet**:
  Obtain the canonical mainnet `genesis.json` from the network RPC or seed node, then install:
  ```bash
  # Install genesis and initialize home directory
  ./synthos-comet join --home ~/.synthos-comet --genesis-file /path/to/mainnet-genesis.json
  ```

- **Initializing a New Network / Local Devnet**:
  To initialize a fresh network from a SYNTHOS app genesis template:
  ```bash
  ./synthos-comet init --home ~/.synthos-comet --genesis app-genesis-mainnet.json
  ```

#### 3. Starting the Node
```bash
# Start node with persistent peers and expose the synthosd-compatible API
./synthos-comet start --home ~/.synthos-comet \
  --peers "514928022ac348f9648fba4be0d60235876e69f8@82.29.54.7:26656,4b2d706807e5cd6487548c54e686c67f872896aa@82.29.130.11:26656" \
  --api :8545
```

---

## 2. Candidate Node: Outbound-Only Proof-of-Uptime (`synthos-node`)

For home or desktop operators who want to build uptime history toward validator eligibility without opening inbound router ports or obtaining static IPs:

```bash
# Run silent candidate node
go run ./cmd/silentnode -id "my-operator-node" -relay "https://82-29-54-7.sslip.io"
```

The node connects outbound via HTTPS, generates a local Ed25519 keypair in `.synthos/`, and sends signed periodic heartbeats to the network registry.

---

## 3. Local Peer Registry Backend (`cmd/cloudless-registry`)

To run the full registry and website backend locally:

```bash
# Development mode (bypasses required REGISTRY_SECRET for testing)
go run ./cmd/cloudless-registry -listen :8090 --insecure-dev

# Production mode (fails closed if REGISTRY_SECRET is not set):
REGISTRY_SECRET="your-secure-secret" go run ./cmd/cloudless-registry -listen :8090

# Check backend health
curl http://127.0.0.1:8090/health
```

---

## 4. Running the Full Go Test Suite

```bash
# Root module tests (chain state machine, crypto, rpc, storage)
go test ./...

# CometBFT production node tests
(cd cometapp && go test -race -count=1 ./...)
```

---

## 5. Prototype Python Framework (Historical / Archive)

The original conceptual prototype of the 7-role agent network is in `src/`.

### Prerequisites
- Python 3.8+
```bash
pip install -r requirements.txt
pip install pytest pytest-asyncio
```

### Run Example & Tests
```bash
# Run Python simulation example
python example.py

# Run Python unit tests
pytest tests/ -v
```
