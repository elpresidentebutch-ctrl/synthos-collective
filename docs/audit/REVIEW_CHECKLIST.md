## SYNTHOS Collective â€” Security Review Checklist (v0)

### Build & execution

- [ ] `go test ./...` runs (or document why tests are missing)
- [ ] `go run ./cmd/devnet` produces a finalized height increase across validators
- [ ] `go run ./cmd/rpcnode` starts and serves endpoints without panics

### Ledger correctness (`internal/chain`)

- [ ] Tx signature verification cannot be bypassed
- [ ] Nonce rules are enforced (replay/double-spend prevention)
- [ ] Balance underflow is impossible
- [ ] `BuildBlock` ordering is deterministic (no map iteration nondeterminism leaks into tx ordering)
- [ ] `ValidateBlock` replay produces the same `StateRoot` and rejects mismatches
- [ ] Hash/ID computations are stable and collision-resistant enough for intended use

### Consensus finality (`internal/consensus`)

- [ ] Finality threshold math is correct for all N (including N=0/1/2/3/4â€¦)
- [ ] Duplicate votes are idempotent and cannot cause double counting
- [ ] Votes are keyed by authenticated validator identity (not attacker-controlled fields)
- [ ] Engine behavior under network partitions is understood and documented
- [ ] Conflicting-finality risk: can two different blocks at same height both become â€œfinalizedâ€ under the current rules? If so, document and propose mitigation.

### Envelope / transport (`internal/agent`, `internal/network`, `internal/crypto`)

- [ ] Envelope signature validation binds `FromAgentID` to the public key used
- [ ] Freshness window is applied to all consensus-relevant messages
- [ ] Replay protection exists (or is explicitly missing and called out)
- [ ] Payload size limits exist (or are explicitly missing and called out)
- [ ] Rate limiting cannot be trivially bypassed (agentID spoofing, relay fanout)

### Node orchestration (`internal/node`)

- [ ] Only validators can propose blocks; only validator votes are counted
- [ ] Unknown peers are dropped safely (no state mutation)
- [ ] Hardware hash â€œclone detectionâ€ cannot be abused for trivial DoS
- [ ] Finalize logic cannot finalize a block that fails validation

### RPC + persistence (`internal/rpc`, `internal/storage`, `cmd/rpcnode`)

- [ ] RPC handlers validate input strictly (addresses, numeric bounds, required fields)
- [ ] SubmitTx path cannot crash via malformed JSON or invalid types
- [ ] Snapshot load/save is consistent and resistant to partial writes

### Documentation quality (audit usability)

- [ ] Threat model matches implementation reality
- [ ] Invariants are testable and mapped to code
- [ ] Known limitations and â€œnot production readyâ€ items are clearly stated

### Bridge review (`contracts/src/synthos/SYNTHOSBridgeVault.sol`, `cmd/bridgerelayer`, `internal/chain`)

- [ ] `./scripts/bridge_audit.ps1` passes on the exact review commit
- [ ] Bridge vault starts paused
- [ ] Unsupported assets and disabled chains are rejected
- [ ] Release replay protection cannot be bypassed
- [ ] Relayer quorum cannot double-count duplicate approvals
- [ ] Release delay cannot be skipped
- [ ] Epoch release limits cannot be bypassed by queued releases
- [ ] Native `bridge_release_native` requires validator signatures when genesis bridge validators are configured
- [ ] Manual EVM event decoding matches the Solidity event ABI
- [ ] Browser bridge calldata encoding matches the contract ABI
- [ ] Owner controls are assigned to multisig/timelock before public liquidity
---

## Legal notice

SYNTHOS Collective source code and repository materials covered by the root `LICENSE` file are licensed under the **Apache License, Version 2.0**. SYNTHOS Collective, SYNTHOS, and related names, marks, and logos remain reserved except as permitted by that license for describing the origin of the work.

This document is informational only and is not legal, financial, or investment advice. The canonical legal notice is in **docs/LEGAL_NOTICE.md** in the SYNTHOS Collective repository.


