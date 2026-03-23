## SYNTHOS Collective — Security Review Checklist (v0)

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

- [ ] Finality threshold math is correct for all N (including N=0/1/2/3/4…)
- [ ] Duplicate votes are idempotent and cannot cause double counting
- [ ] Votes are keyed by authenticated validator identity (not attacker-controlled fields)
- [ ] Engine behavior under network partitions is understood and documented
- [ ] Conflicting-finality risk: can two different blocks at same height both become “finalized” under the current rules? If so, document and propose mitigation.

### Envelope / transport (`internal/agent`, `internal/network`, `internal/crypto`)

- [ ] Envelope signature validation binds `FromAgentID` to the public key used
- [ ] Freshness window is applied to all consensus-relevant messages
- [ ] Replay protection exists (or is explicitly missing and called out)
- [ ] Payload size limits exist (or are explicitly missing and called out)
- [ ] Rate limiting cannot be trivially bypassed (agentID spoofing, relay fanout)

### Node orchestration (`internal/node`)

- [ ] Only validators can propose blocks; only validator votes are counted
- [ ] Unknown peers are dropped safely (no state mutation)
- [ ] Hardware hash “clone detection” cannot be abused for trivial DoS
- [ ] Finalize logic cannot finalize a block that fails validation

### RPC + persistence (`internal/rpc`, `internal/storage`, `cmd/rpcnode`)

- [ ] RPC handlers validate input strictly (addresses, numeric bounds, required fields)
- [ ] SubmitTx path cannot crash via malformed JSON or invalid types
- [ ] Snapshot load/save is consistent and resistant to partial writes

### Documentation quality (audit usability)

- [ ] Threat model matches implementation reality
- [ ] Invariants are testable and mapped to code
- [ ] Known limitations and “not production ready” items are clearly stated

---

## Legal notice

SYNTHOS Collective, SYNTHOS, and related names, marks, documentation, and technical materials in this document are the **exclusive property of James G. Isham Williams, Sr.** Unauthorized reproduction, distribution, or commercial use without express written permission is prohibited except as allowed under applicable open-source licenses for identified files. No rights are waived.

This document is informational only and is not legal, financial, or investment advice. The canonical legal notice is in **docs/LEGAL_NOTICE.md** in the SYNTHOS Collective repository.
