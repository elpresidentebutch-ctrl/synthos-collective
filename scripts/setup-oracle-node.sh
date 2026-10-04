#!/usr/bin/env bash
set -euo pipefail

echo "=================================================="
echo " SYNTHOS Global Node - Oracle Cloud Always-Free   "
echo "=================================================="

if [ "$EUID" -ne 0 ]; then
  echo "[-] Please run as root: sudo bash $0"
  exit 1
fi

echo "[+] Updating apt repositories..."
apt-get update -y
apt-get install -y docker.io git curl ca-certificates

echo "[+] Enabling and starting Docker..."
systemctl enable docker
systemctl start docker

INSTALL_DIR="/opt/synthos"
echo "[+] Setting up SYNTHOS in $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR"

if [ -d "synthos-collective/.git" ]; then
  echo "[+] Updating existing repository..."
  cd synthos-collective
  git fetch --all
  git reset --hard origin/main
else
  echo "[+] Cloning repository..."
  git clone https://github.com/elpresidentebutch-ctrl/synthos-collective.git
  cd synthos-collective
fi

echo "[+] Building SYNTHOS container..."
docker build -t synthos:latest .

echo "[+] Installing systemd service..."
cat << 'EOF' > /etc/systemd/system/synthos-node.service
[Unit]
Description=SYNTHOS Global Node Daemon
After=docker.service
Requires=docker.service

[Service]
TimeoutStartSec=0
Restart=always
RestartSec=10
ExecStartPre=-/usr/bin/docker stop synthos-node
ExecStartPre=-/usr/bin/docker rm synthos-node
ExecStart=/usr/bin/docker run --name synthos-node \
  -p 8080:8080 \
  -v /var/lib/synthos-data:/data \
  -e SYNTHOS_CONFIG=/config/global-node.json \
  -e SYNTHOS_DATA_DIR=/data \
  -e SYNTHOS_NODE_ID=synthos-oracle-node \
  -e SYNTHOS_REGISTRY_URL=https://synthos-www.onrender.com \
  -e SYNTHOS_BOOTSTRAP_IMMUNE_NODE=true \
  synthos:latest /usr/local/bin/synthosd
ExecStop=/usr/bin/docker stop synthos-node

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable synthos-node
systemctl restart synthos-node

echo "=================================================="
echo "[+] SYNTHOS Global Node successfully deployed!"
echo "[+] Node service status: systemctl status synthos-node"
echo "[+] View live logs: docker logs -f synthos-node"
echo "=================================================="
