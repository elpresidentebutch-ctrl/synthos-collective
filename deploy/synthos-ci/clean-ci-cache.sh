#!/usr/bin/env bash
# clean-ci-cache.sh: Periodically cleans Go build caches, module caches, and logs
# to ensure the disk on the server never fills up.
set -euo pipefail

export HOME=/var/lib/synthos-ci
export GOPATH=/var/lib/synthos-ci/go
export GOCACHE=/var/lib/synthos-ci/.cache/go-build
export PATH=/usr/local/go/bin:$PATH

echo "[CI Cleanup] Starting scheduled build cache cleanup at $(date -u)..."

# 1. Clean Go test cache and compilation cache
if command -v go >/dev/null 2>&1; then
  go clean -cache 2>/dev/null || true
  go clean -testcache 2>/dev/null || true
fi

# 2. Prune old Go build cache directory directly if files are older than 3 days
if [ -d "$GOCACHE" ]; then
  find "$GOCACHE" -type f -atime +3 -delete 2>/dev/null || true
fi

# 3. Clean temporary files older than 24 hours
find /tmp -maxdepth 1 -user synthos-ci -mtime +1 -exec rm -rf {} + 2>/dev/null || true
find /var/lib/synthos-ci/tmp -mindepth 1 -mtime +1 -exec rm -rf {} + 2>/dev/null || true

# 4. Prune build logs older than 7 days
find /var/lib/synthos-ci/logs -name "build-*.log" -mtime +7 -delete 2>/dev/null || true

# 5. Clean git garbage collection in the checkout directory
if [ -d "/var/lib/synthos-ci/repo/.git" ]; then
  git -C /var/lib/synthos-ci/repo gc --prune=now --quiet 2>/dev/null || true
fi

DISK_STATUS=$(df -h /var/lib/synthos-ci 2>/dev/null | awk 'NR==2 {print $5 " used, " $4 " available"}')
echo "[CI Cleanup] Cleanup complete. Disk status: $DISK_STATUS"
