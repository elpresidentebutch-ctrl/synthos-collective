# SYNTHOS Bridge Audit Notes

This document is the internal audit packet for the SYNTHOS bridge. It does not
replace an independent third-party audit. It defines what reviewers should
inspect before any public bridge liquidity is enabled.

## Scope

Bridge code under review:

- `contracts/src/synthos/SYNTHOSBridgeVault.sol`
- `contracts/test/bridge-vault.test.js`
- `cmd/bridgerelayer`
- `internal/chain` bridge receipt logic
- `internal/rpc` bridge endpoints
- `website/bridge.html`
- `website/assets/bridge.js`

## Security model

The bridge uses a guarded vault plus relayer/validator proof model:

1. EVM outbound locks are emitted by `SYNTHOSBridgeVault.BridgeLocked`.
2. Relayers observe confirmed EVM logs.
3. Native releases require bridge-validator Ed25519 signatures when bridge
   validators are configured in genesis metadata.
4. Native state tracks processed source events to prevent replay.
5. EVM releases require configured relayer quorum.
6. EVM vault releases can be capped and delayed.

## Intended bridge invariants

- A source event can release value only once.
- Unsupported assets cannot be locked or released.
- Disabled chains cannot be used.
- A relayer cannot approve the same release twice.
- An outsider cannot approve releases.
- Release quorum cannot exceed the active relayer count.
- If native bridge validators are configured, native releases without quorum
  signatures are rejected.
- EVM per-lock and per-release caps are enforced when non-zero.
- EVM epoch release caps are enforced when non-zero.
- Emergency release delay queues payouts after quorum instead of paying
  immediately.
- The bridge is paused by default after deployment.

## High-risk areas for external review

- Manual ABI decoding in `cmd/bridgerelayer`.
- Browser-side calldata encoding in `website/assets/bridge.js`.
- EVM vault delay/limit interactions.
- Native bridge validator genesis configuration and key rotation.
- The difference between relayer quorum on EVM and validator quorum on native.
- Operational risk if the same person controls all relayers/bridge validators.
- Liquidity accounting across wrapped/native SYN supply.

## Required pre-launch controls

- Third-party Solidity audit of exact deployed commit.
- Reproducible deployment files and verified contract source.
- Multisig/timelock owner for bridge vault.
- At least 3-of-5 independent relayers for public testing.
- At least 3-of-5 independent bridge-validator signatures for native release
  proof.
- Non-zero lock/release/epoch caps.
- Non-zero release delay.
- Monitoring for unmatched lock/release totals.
- Documented emergency pause procedure.
- Small testnet-only liquidity until multiple rehearsals pass.

## Internal pre-audit command

From the repository root:

```powershell
.\scripts\bridge_audit.ps1
```

This runs:

- Go tests;
- Hardhat compile;
- Hardhat tests;
- bridge-focused source checks.

