#!/usr/bin/env bash
# install.sh: Sets up synthos-ci runner and hardens systemd priority and security
# on Ubuntu/Debian Linux (Salt Lake City / HostEons server).
set -euo pipefail

[ "$(id -u)" = 0 ] || { echo "Run as root (sudo)" >&2; exit 1; }

echo "== [1/6] Securing node folder & validator keys"
if [ -d "/var/lib/synthos" ]; then
  chown -R synthos:synthos /var/lib/synthos
  chmod 700 /var/lib/synthos
  echo "   /var/lib/synthos restricted to mode 700 (owner synthos only)."
fi

echo "== [2/6] Creating unprivileged synthos-ci system user"
if ! id synthos-ci >/dev/null 2>&1; then
  useradd --system --home /var/lib/synthos-ci --shell /usr/sbin/nologin synthos-ci
  echo "   Created user synthos-ci."
fi

mkdir -p /var/lib/synthos-ci/{repo,logs,tmp,go,.cache/go-build}
chown -R synthos-ci:synthos-ci /var/lib/synthos-ci
chmod 750 /var/lib/synthos-ci

echo "== [3/6] Installing executable CI scripts"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
install -m 755 "$SCRIPT_DIR/run-ci.sh" /usr/local/bin/synthos-ci-run
install -m 755 "$SCRIPT_DIR/clean-ci-cache.sh" /usr/local/bin/synthos-ci-clean-cache

echo "== [4/6] Protecting validator (synthos-node) priority and OOM score"
mkdir -p /etc/systemd/system/synthos-node.service.d
cp "$SCRIPT_DIR/synthos-node-protect.conf" /etc/systemd/system/synthos-node.service.d/override.conf

echo "== [5/6] Installing synthos-ci and cleanup timer systemd units"
cp "$SCRIPT_DIR/synthos-ci.service" /etc/systemd/system/synthos-ci.service
cp "$SCRIPT_DIR/synthos-ci-cleanup.service" /etc/systemd/system/synthos-ci-cleanup.service
cp "$SCRIPT_DIR/synthos-ci-cleanup.timer" /etc/systemd/system/synthos-ci-cleanup.timer

systemctl daemon-reload
systemctl enable --now synthos-ci-cleanup.timer
systemctl enable --now synthos-ci

# Apply override to running node without restarting validator if running
if systemctl is-active --quiet synthos-node; then
  echo "   synthos-node is active: applying resource limits and OOM score..."
  systemctl set-property synthos-node CPUWeight=1000 IOWeight=1000
fi

echo "== [6/6] Verifying isolation permissions"
if sudo -u synthos-ci ls /var/lib/synthos >/dev/null 2>&1; then
  echo "[WARNING] synthos-ci was able to read /var/lib/synthos directly!" >&2
  exit 1
else
  echo "   [CONFIRMED] synthos-ci has NO ACCESS to /var/lib/synthos or validator keys."
fi

echo
echo "=== synthos-ci installation & hardening complete ==="
echo "Status check: systemctl status synthos-ci"
echo "Logs:         journalctl -u synthos-ci -f"
echo "Timer status: systemctl list-timers synthos-ci-cleanup.timer"
