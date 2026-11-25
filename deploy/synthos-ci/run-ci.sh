#!/usr/bin/env bash
# run-ci.sh: Polls GitHub repository and executes test suite and builds
# strictly as the unprivileged synthos-ci user.
set -euo pipefail

REPO_DIR="/var/lib/synthos-ci/repo"
REPO_URL="https://github.com/elpresidentebutch-ctrl/synthos-collective.git"
BRANCH="${SYNTHOS_CI_BRANCH:-main}"
LOG_DIR="/var/lib/synthos-ci/logs"

mkdir -p "$LOG_DIR" /var/lib/synthos-ci/.cache/go-build /var/lib/synthos-ci/go /var/lib/synthos-ci/tmp

export HOME=/var/lib/synthos-ci
export GOPATH=/var/lib/synthos-ci/go
export GOCACHE=/var/lib/synthos-ci/.cache/go-build
export TMPDIR=/var/lib/synthos-ci/tmp
export PATH=/usr/local/go/bin:$GOPATH/bin:$PATH

# Report each run's result to GitHub as a "synthos-ci" commit status, so
# commits and pull requests show a real pass/fail check without GitHub Actions.
# The token (fine-grained, synthos-collective only, "Commit statuses: write")
# lives in a root-created file readable only by the synthos-ci user. If the
# file is missing, CI still runs; it just doesn't report to GitHub.
STATUS_TOKEN_FILE="${SYNTHOS_CI_TOKEN_FILE:-/etc/synthos-ci/github-token}"
if [ ! -e "$STATUS_TOKEN_FILE" ] && [ -e /etc/synthos-ci/github.token ]; then
  STATUS_TOKEN_FILE=/etc/synthos-ci/github.token
fi
STATUS_REPO="elpresidentebutch-ctrl/synthos-collective"
post_status() { # usage: post_status <pending|success|failure> "<description>"
  [ -r "$STATUS_TOKEN_FILE" ] || return 0
  # The header is passed through a file descriptor so the token never
  # appears in the process list.
  curl -fsS -o /dev/null -X POST \
    -H @<(printf 'Authorization: Bearer %s\n' "$(tr -d '[:space:]' < "$STATUS_TOKEN_FILE")") \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/$STATUS_REPO/statuses/$REMOTE_REV" \
    -d "{\"state\":\"$1\",\"context\":\"synthos-ci\",\"description\":\"$2\"}" \
    || echo "[CI] Warning: could not post '$1' status to GitHub" >&2
}

# Avoid git dubious ownership failures
git config --global --add safe.directory "$REPO_DIR" 2>/dev/null || true

# Security check: ensure synthos-ci CANNOT read node or validator keys
for node_dir in /var/lib/synthos /var/lib/synthos-comet; do
  if [ -d "$node_dir" ]; then
    if ls "$node_dir" >/dev/null 2>&1; then
      echo "[SECURITY VIOLATION] synthos-ci has access to $node_dir! Aborting immediately." >&2
      exit 1
    fi
  fi
done

# Clone or fetch repository
if [ ! -d "$REPO_DIR/.git" ]; then
  echo "[CI] Initializing repository checkout..."
  git clone --depth 50 "$REPO_URL" "$REPO_DIR"
fi

cd "$REPO_DIR"
git remote set-url origin "$REPO_URL" 2>/dev/null || true
git fetch origin "$BRANCH" --depth 50

LOCAL_REV=$(git rev-parse HEAD 2>/dev/null || echo "none")
REMOTE_REV=$(git rev-parse "origin/$BRANCH")

if [ "$LOCAL_REV" = "$REMOTE_REV" ] && [ -f "$LOG_DIR/last_run.ok" ]; then
  # Already tested this revision, sleep and poll again
  sleep 60
  exit 0
fi

echo "[CI] New revision detected: $REMOTE_REV (current: $LOCAL_REV). Starting CI run..."
git checkout -f "$REMOTE_REV"
git clean -fdx -e .cache/

BUILD_LOG="$LOG_DIR/build-$(date +%Y%m%d-%H%M%S)-$REMOTE_REV.log"
echo "=== SYNTHOS CI Run: $(date -u) ===" > "$BUILD_LOG"
echo "Commit: $REMOTE_REV" >> "$BUILD_LOG"
post_status pending "Running tests and builds on HostEons (Salt Lake City)"

# Step 1: Run complete Go test suite
echo "[CI] Running test suite (go test ./...)..." | tee -a "$BUILD_LOG"
if go test -v ./... >> "$BUILD_LOG" 2>&1; then
  echo "[CI] Tests: PASS" | tee -a "$BUILD_LOG"
else
  echo "[CI] Tests: FAIL (see $BUILD_LOG)" | tee -a "$BUILD_LOG"
  post_status failure "go test ./... failed on HostEons (Salt Lake City)"
  rm -f "$LOG_DIR/last_run.ok"
  sleep 60
  exit 1
fi

# Step 2: Test building core binaries
echo "[CI] Verifying core binary compilations..." | tee -a "$BUILD_LOG"
BUILD_OK=1
(cd cometapp && go build -trimpath -o /dev/null .) >> "$BUILD_LOG" 2>&1 || BUILD_OK=0
go build -trimpath -o /dev/null ./cmd/silentnode >> "$BUILD_LOG" 2>&1 || BUILD_OK=0
go build -trimpath -o /dev/null ./cmd/wallet >> "$BUILD_LOG" 2>&1 || BUILD_OK=0
go build -trimpath -o /dev/null ./cmd/filecoin-archive >> "$BUILD_LOG" 2>&1 || BUILD_OK=0

if [ "$BUILD_OK" = 1 ]; then
  echo "[CI] Binary Builds: PASS" | tee -a "$BUILD_LOG"
  touch "$LOG_DIR/last_run.ok"
  post_status success "Tests and builds passed on HostEons (Salt Lake City)"
else
  echo "[CI] Binary Builds: FAIL (see $BUILD_LOG)" | tee -a "$BUILD_LOG"
  post_status failure "Binary builds failed on HostEons (Salt Lake City)"
  rm -f "$LOG_DIR/last_run.ok"
  sleep 60
  exit 1
fi

# Step 3: Immediate post-run cache trim to conserve disk space
go clean -testcache 2>/dev/null || true

# Keep only the latest 20 log files
ls -1t "$LOG_DIR"/build-*.log 2>/dev/null | tail -n +21 | xargs -r rm -f --

echo "[CI] Cycle complete for commit $REMOTE_REV."
