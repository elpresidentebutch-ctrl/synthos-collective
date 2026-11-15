# synthos-ci (HostEons / Salt Lake City Server Setup)

Production configuration for running `synthos-ci` safely co-located alongside the primary CometBFT validator node (`synthos-comet.service`).

---

## Security & Resource Allocation Guarantees

1. **Unprivileged User Isolation (Never Root)**:
   - Runs strictly under the dedicated system user `synthos-ci`.
   - `NoNewPrivileges=true`, `ProtectSystem=strict`, `ProtectHome=true`, `PrivateTmp=true`.
   - The node folders `/var/lib/synthos` and `/var/lib/synthos-comet` are set to `chmod 700` (owned exclusively by `synthos:synthos`).
   - `InaccessiblePaths=-/var/lib/synthos -/var/lib/synthos-comet -/root -/home` ensures `synthos-ci` cannot read, list, or traverse any node directories or validator keys (`priv_validator_key.json`), while using `-` prefix so missing paths do not abort unit startup.

2. **Resource Throttling & Priority (Validator Always Comes First)**:
   - **Scheduling Priority**: `Nice=19` (lowest CPU priority) and `IOSchedulingClass=idle`.
   - **CPU Quota**: `CPUQuota=60%` and `CPUWeight=10` (compared to validator `CPUWeight=1000` and `Nice=-5`). CometBFT consensus scheduling is never starved.
   - **Memory Cap**: `MemoryHigh=1200M` and `MemoryMax=1500M`.

3. **Kernel OOM Protection (Kill CI Before Validator)**:
   - `synthos-ci.service` sets `OOMScoreAdjust=1000` (highest sacrificial score).
   - `synthos-comet.service` sets `OOMScoreAdjust=-900` via `/etc/systemd/system/synthos-comet.service.d/override.conf`.
   - If server RAM runs low, the Linux kernel Out-Of-Memory killer will immediately terminate `synthos-ci` and protect `synthos-comet`.

4. **Automated Build Cache Cleanup (Disk Never Fills)**:
   - `run-ci.sh` trims Go test caches immediately after each run and keeps only the last 20 build logs.
   - `synthos-ci-cleanup.timer` runs every 6 hours to clear Go build caches, temporary files older than 24 hours, and run git garbage collection.

---

## 1-Command Installation on the Server

Run on the server as root (or via `sudo`):

```bash
cd /opt/synthos/src/deploy/synthos-ci
sudo bash install.sh
```

---

## Manual Verification Commands

### 1. Verify User & Filesystem Isolation
Confirm `synthos-ci` cannot see inside the validator directories:
```bash
sudo -u synthos-ci ls -la /var/lib/synthos
sudo -u synthos-ci ls -la /var/lib/synthos-comet
# Expected output: Permission denied
```

### 2. Verify OOM Scores
Check that the kernel prioritizes killing CI over the validator:
```bash
cat /proc/$(pgrep synthos-comet)/oom_score_adj
# Expected output: -900 (heavily protected)

cat /proc/$(pgrep -f synthos-ci-run)/oom_score_adj
# Expected output: 1000 (sacrificial)
```

### 3. Check CI Status and Logs
```bash
systemctl status synthos-ci
journalctl -u synthos-ci -f
```

### 4. Check Cleanup Timer
```bash
systemctl list-timers synthos-ci-cleanup.timer
```
