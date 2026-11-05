# Antigravity Rules for SYNTHOS Collective

## Validator Safety & Consensus Rules

1. **Strict Key and State Integrity**:
   - **NEVER** reset, modify, overwrite, delete, or copy `priv_validator_state.json` or `priv_validator_key.json` on any node or validator under any circumstances.
   - Any manual or automated mutation of consensus signing state risks double-signing or consensus faults.

2. **No Unreviewed Validator Upgrades**:
   - **NEVER** switch, deploy, update, restart, or migrate live network validators to a new binary or build until the user has explicitly reviewed and approved the code and build.
   - All validator software must remain uniform across the network. Never deploy split builds across validators.

3. **Secret Storage**:
   - **NEVER** store private keys, validator keys, or operator secrets in OneDrive-synced directories. Keep offline keys strictly in non-synced, designated local storage (`C:\Users\elpre\Desktop\CC KEYS`).
