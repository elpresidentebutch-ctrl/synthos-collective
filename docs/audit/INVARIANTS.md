## SYNTHOS Collective — Intended Invariants (v0)

These are the safety properties the implementation is intended to satisfy. Reviewers can use them to test whether the code matches the intended behavior.

### Ledger invariants

- **I1 — Valid signatures only**: A transaction must be signed by the `From` account key to be applied.
- **I2 — Nonce monotonicity**: For a given sender, nonces must increase monotonically; a tx with an unexpected nonce must not apply.
- **I3 — No negative balances**: A state transition must not reduce any balance below zero.
- **I4 — State root correctness**: A block’s `StateRoot` must equal the root obtained by replaying its txs from the parent state.
- **I5 — Deterministic block building**: Given the same mempool and same parent state, `BuildBlock` must produce the same ordered tx list and same `StateRoot`.

### Block invariants

- **B1 — Single-parent extension**: A non-genesis block must reference the current tip hash as `ParentHash` when being validated for extension.
- **B2 — Height increments by 1**: A valid extension block height must be `tip.height + 1`.
- **B3 — Timeless runtime**: Non-genesis blocks must not carry wall-clock timestamps (must be zero).

### Consensus/message invariants

- **C1 — Validator gating**: Only configured validators can submit proposals and votes that are processed.
- **C2 — Vote authenticity**: A vote’s `VoterID` must match the authenticated envelope sender.
- **C3 — Finality threshold**: A block is finalized only after \( \lceil 2N/3 \rceil \) distinct validator votes-for have been recorded.
- **C4 — Idempotency**: Duplicate votes from the same validator for the same proposal must not increase vote count.

### Operational invariants

- **O1 — Spam resistance**: Nodes should be able to drop malformed or excessive inbound messages without unbounded memory growth.
- **O2 — Crash safety (rpcnode)**: Persisted snapshots should be loadable and should not allow trivial corruption via malformed input.

