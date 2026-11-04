#!/usr/bin/env bash
# devnet-4node.sh: Bootstraps and orchestrates a local 4-validator CometBFT devnet
set -euo pipefail

DEVNET_DIR="${1:-$HOME/.synthos/devnet-4node}"
echo "=== Bootstrapping SYNTHOS 4-Node CometBFT Devnet in ${DEVNET_DIR} ==="

mkdir -p "${DEVNET_DIR}"
cd "$(dirname "$0")/.."

# Compile synthos-comet if not present
if [ ! -f ./synthos-comet ]; then
    echo "Building synthos-comet binary..."
    go build -o synthos-comet .
fi

# Initialize node directories
for i in 1 2 3 4; do
    NODE_HOME="${DEVNET_DIR}/node${i}"
    mkdir -p "${NODE_HOME}/config" "${NODE_HOME}/data"
    echo "Creating validator key for Node ${i}..."
    ./synthos-comet validator-key --home "${NODE_HOME}" > "${NODE_HOME}/val_key.txt"
done

echo "Devnet nodes initialized."
echo "Launch with: docker-compose -f docker-compose.devnet.yml up -d"
