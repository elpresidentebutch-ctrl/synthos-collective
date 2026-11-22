# SYNTHOS Base L2 Anchor Service

Hourly relayer service that records the latest finalized SYNTHOS block hash, app hash, and timestamp onto Base mainnet (`SynthosAnchor` contract at `0xae4C167ff08d13CaC9b397D13c50904bb9c881a5`).

## Files & Deployment

- `anchor.mjs`: Node.js relayer script.
- `synthos-anchor.service`: Systemd oneshot unit invoking `anchor.mjs anchor`.
- `synthos-anchor.timer`: Systemd timer firing hourly (`OnCalendar=hourly`, `Persistent=true`).
- `package.json`: Module dependencies (`ethers` v6).

## Server Configuration

1. Place code in `/opt/synthos-anchor/`.
2. Secure configuration in `/etc/synthos-anchor/`:
   - `/etc/synthos-anchor/relayer.key`: Relayer private key (mode `0600`, root-only, never committed).
   - `/etc/synthos-anchor/contract`: Anchor contract address (`0xae4C167ff08d13CaC9b397D13c50904bb9c881a5`).
3. Install and enable systemd units:
   ```bash
   cp deploy/synthos-anchor/synthos-anchor.service /etc/systemd/system/
   cp deploy/synthos-anchor/synthos-anchor.timer /etc/systemd/system/
   systemctl daemon-reload
   systemctl enable --now synthos-anchor.timer
   ```

## Security & Verification

- Never commit or expose `relayer.key`.
- Any run that fails to submit (e.g., stale height, insufficient ETH for gas, RPC timeout, revert) logs an explicit error and exits non-zero (`process.exit(1)`).
- Verify state on Base anytime:
   ```bash
   node /opt/synthos-anchor/anchor.mjs status
   ```
