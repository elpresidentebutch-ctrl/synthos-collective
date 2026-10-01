#!/usr/bin/env bash
# Installs a SYNTHOS node (CometBFT) on a fresh Ubuntu/Debian server and
# runs it as a service. Anyone can run one; it joins the network as a full
# node, and becomes a validator only if its operator bonds SYN to it.
#
#   curl -fsSL https://raw.githubusercontent.com/elpresidentebutch-ctrl/synthos-collective/feat/cometbft-migration/deploy/node/install.sh \
#     | sudo bash -s -- [options]
#
# Options:
#   --network NAME          network folder under networks/ (default synthos-testnet-1)
#   --ref REF               git branch/tag/commit to build (default feat/cometbft-migration)
#   --moniker NAME          node name shown to peers (default: hostname)
#   --peers LIST            id@host:port,... to stay connected to
#   --seeds LIST            id@host:port,... to discover peers from
#   --private-peer-ids IDS  node IDs never gossiped (validators behind this node)
#   --public-api            serve the HTTP API over HTTPS at https://<ip>.sslip.io
#
# Re-running it upgrades the program and keeps the node's keys and data.
set -euo pipefail

NETWORK=synthos-testnet-1
REF=feat/cometbft-migration
MONIKER=$(hostname)
PEERS=""
SEEDS=""
PRIVATE_IDS=""
PUBLIC_API=0
REPO=https://github.com/elpresidentebutch-ctrl/synthos-collective.git
GO_VERSION=1.25.0

while [ $# -gt 0 ]; do
  case "$1" in
    --network) NETWORK=$2; shift 2 ;;
    --ref) REF=$2; shift 2 ;;
    --moniker) MONIKER=$2; shift 2 ;;
    --peers) PEERS=$2; shift 2 ;;
    --seeds) SEEDS=$2; shift 2 ;;
    --private-peer-ids) PRIVATE_IDS=$2; shift 2 ;;
    --public-api) PUBLIC_API=1; shift ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done

[ "$(id -u)" = 0 ] || { echo "run as root (sudo)" >&2; exit 1; }
for v in "$NETWORK" "$MONIKER" "$PEERS" "$SEEDS" "$PRIVATE_IDS" "$REF"; do
  case "$v" in *[!A-Za-z0-9@:.,_/-]*) echo "invalid characters in '$v'" >&2; exit 2 ;; esac
done

echo "== packages"
export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get install -y -q git curl ca-certificates ufw
if [ "$PUBLIC_API" = 1 ]; then apt-get install -y -q caddy; fi

echo "== Go $GO_VERSION"
ARCH=$(dpkg --print-architecture)   # amd64 or arm64
if ! /usr/local/go/bin/go version 2>/dev/null | grep -q "go$GO_VERSION "; then
  curl -fsSL "https://go.dev/dl/go$GO_VERSION.linux-$ARCH.tar.gz" -o /tmp/go.tgz
  rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz && rm /tmp/go.tgz
fi
export PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=local

echo "== build synthos-comet from $REF"
SRC=/opt/synthos/src
if [ -d "$SRC/.git" ]; then
  git -C "$SRC" fetch -q --depth 1 origin "$REF"
  git -C "$SRC" checkout -q -f FETCH_HEAD
else
  mkdir -p /opt/synthos
  git init -q "$SRC"
  git -C "$SRC" remote add origin "$REPO"
  git -C "$SRC" fetch -q --depth 1 origin "$REF"
  git -C "$SRC" checkout -q FETCH_HEAD
fi
echo "   commit $(git -C "$SRC" rev-parse HEAD)"
(cd "$SRC/cometapp" && go build -trimpath -o /usr/local/bin/synthos-comet.new .)
mv /usr/local/bin/synthos-comet.new /usr/local/bin/synthos-comet

echo "== node home"
id synthos >/dev/null 2>&1 || useradd --system --home /var/lib/synthos --shell /usr/sbin/nologin synthos
HOME_DIR=/var/lib/synthos/node
GENESIS="$SRC/networks/$NETWORK/genesis.json"
[ -f "$GENESIS" ] || { echo "no genesis for network $NETWORK" >&2; exit 1; }
mkdir -p "$HOME_DIR"
synthos-comet join --home "$HOME_DIR" --genesis-file "$GENESIS"
chown -R synthos:synthos /var/lib/synthos

IP=$(curl -fsS --max-time 5 http://169.254.169.254/hetzner/v1/metadata/public-ipv4 2>/dev/null \
  || curl -fsS --max-time 5 https://api.ipify.org)
echo "   public address $IP"

ARGS="start --home $HOME_DIR --p2p tcp://0.0.0.0:26656 --rpc \"\" --external-address $IP:26656 --moniker $MONIKER --api 127.0.0.1:8080"
[ -n "$PEERS" ] && ARGS="$ARGS --peers $PEERS"
[ -n "$SEEDS" ] && ARGS="$ARGS --seeds $SEEDS"
[ -n "$PRIVATE_IDS" ] && ARGS="$ARGS --private-peer-ids $PRIVATE_IDS"
[ "$PUBLIC_API" = 1 ] && ARGS="$ARGS --trust-proxy"

cat > /etc/systemd/system/synthos-node.service <<EOF
[Unit]
Description=SYNTHOS node ($NETWORK)
After=network-online.target
Wants=network-online.target

[Service]
User=synthos
ExecStart=/usr/local/bin/synthos-comet $ARGS
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

echo "== firewall"
ufw allow 22/tcp >/dev/null
ufw allow 26656/tcp >/dev/null
if [ "$PUBLIC_API" = 1 ]; then
  ufw allow 80/tcp >/dev/null
  ufw allow 443/tcp >/dev/null
  HOSTNAME_API="$(echo "$IP" | tr . -).sslip.io"
  cat > /etc/caddy/Caddyfile <<EOF
$HOSTNAME_API {
	reverse_proxy 127.0.0.1:8080
}
EOF
  systemctl reload caddy || systemctl restart caddy
fi
ufw --force enable >/dev/null

systemctl daemon-reload
systemctl enable synthos-node >/dev/null
systemctl restart synthos-node

echo "== waiting for the node"
UP=0
for _ in $(seq 1 30); do
  sleep 2
  if curl -fsS http://127.0.0.1:8080/status >/dev/null 2>&1; then UP=1; break; fi
done
[ "$UP" = 1 ] || { echo "the node did not come up; see: journalctl -u synthos-node -n 50" >&2; exit 1; }
NODE_ID=$(synthos-comet node-id --home "$HOME_DIR")
echo
echo "SYNTHOS node is running ($NETWORK)."
echo "  node id    $NODE_ID"
echo "  peer       $NODE_ID@$IP:26656"
[ "$PUBLIC_API" = 1 ] && echo "  API        https://$HOSTNAME_API/status"
echo "  logs       journalctl -u synthos-node -f"
echo "  status     curl -s http://127.0.0.1:8080/status"
