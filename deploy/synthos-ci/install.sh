#!/usr/bin/env bash
# install.sh: Sets up synthos-ci runner and hardens systemd priority and security
# on Ubuntu/Debian Linux (Salt Lake City / HostEons server).
set -euo pipefail

[ "$(id -u)" = 0 ] || { echo "Run as root (sudo)" >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "== [1/6] Securing node folder & validator keys"
for node_dir in /var/lib/synthos /var/lib/synthos-comet; do
  if [ -d "$node_dir" ]; then
    chown -R synthos:synthos "$node_dir" 2>/dev/null || true
    chmod 700 "$node_dir"
    echo "   $node_dir restricted to mode 700 (owner synthos only)."
  fi
done

echo "== [2/6] Creating unprivileged synthos-ci system user"
if ! id synthos-ci >/dev/null 2>&1; then
  useradd --system --home /var/lib/synthos-ci --shell /usr/sbin/nologin synthos-ci
  echo "   Created user synthos-ci."
fi

mkdir -p /var/lib/synthos-ci/{repo,logs,tmp,go,.cache/go-build}
chown -R synthos-ci:synthos-ci /var/lib/synthos-ci
chmod 750 /var/lib/synthos-ci
sudo -u synthos-ci git config --global --add safe.directory '*' 2>/dev/null || true

echo "== [3/6] Installing executable CI scripts"
install -m 755 "$SCRIPT_DIR/run-ci.sh" /usr/local/bin/synthos-ci-run
install -m 755 "$SCRIPT_DIR/clean-ci-cache.sh" /usr/local/bin/synthos-ci-clean-cache

echo "== [4/6] Protecting validator (synthos-comet) priority and OOM score"
# Primary validator service on Salt Lake City is synthos-comet.service
for svc in synthos-comet synthos-node; do
  if systemctl list-unit-files "$svc.service" >/dev/null 2>&1 || [ -f "/etc/systemd/system/$svc.service" ]; then
    echo "   Configuring resource priority and OOM protection for $svc.service..."
    mkdir -p "/etc/systemd/system/$svc.service.d"
    cp "$SCRIPT_DIR/synthos-comet-protect.conf" "/etc/systemd/system/$svc.service.d/override.conf"
    if systemctl is-active --quiet "$svc"; then
      echo "   $svc is active: dynamically applying CPU and I/O weights..."
      systemctl set-property "$svc" CPUWeight=1000 IOWeight=1000 2>/dev/null || true
    fi
  fi
done

echo "== [5/6] Installing synthos-ci and cleanup timer systemd units"
cp "$SCRIPT_DIR/synthos-ci.service" /etc/systemd/system/synthos-ci.service
cp "$SCRIPT_DIR/synthos-ci-cleanup.service" /etc/systemd/system/synthos-ci-cleanup.service
cp "$SCRIPT_DIR/synthos-ci-cleanup.timer" /etc/systemd/system/synthos-ci-cleanup.timer

systemctl daemon-reload
systemctl enable --now synthos-ci-cleanup.timer
systemctl enable --now synthos-ci

echo "== [6/6] Verifying isolation permissions"
CHECK_PASSED=1
for node_dir in /var/lib/synthos /var/lib/synthos-comet; do
  if [ -d "$node_dir" ]; then
    if sudo -u synthos-ci ls "$node_dir" >/dev/null 2>&1; then
      echo "[ERROR] synthos-ci was able to read $node_dir!" >&2
      CHECK_PASSED=0
    else
      echo "   [CONFIRMED] synthos-ci has NO ACCESS to $node_dir."
    fi
  fi
done

if [ "$CHECK_PASSED" = 0 ]; then
  exit 1
fi

echo
echo "=== synthos-ci installation & hardening complete ==="
echo "Validator protection: /etc/systemd/system/synthos-comet.service.d/override.conf"
echo "CI service status:    systemctl status synthos-ci"
echo "CI live logs:         journalctl -u synthos-ci -f"
echo "Cleanup timer status: systemctl list-timers synthos-ci-cleanup.timer"
