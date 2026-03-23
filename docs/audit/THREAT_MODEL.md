## SYNTHOS Collective — Threat Model (v0)

### Assets to protect

- **Canonical chain state**: account balances, nonces, state root integrity (`internal/chain/state.go`)
- **Block history**: finalized blocks and their hashes (`internal/chain/block.go`)
- **Finality decisions**: when a block is treated as finalized (`internal/consensus/engine.go`, `internal/node/node.go`)
- **Message authenticity**: envelope signatures and sender identity binding (`internal/agent`, `internal/crypto`, `internal/network`)
- **Availability**: ability for honest validators to make progress

### Actors

- **Honest validator agent**: proposes/votes correctly
- **Honest non-validator agent**: may observe, submit txs, relay messages
- **Malicious validator agent**: may equivocate, spam, or vote incorrectly
- **Malicious non-validator agent**: attempts to inject invalid messages/txs
- **Relay adversary / network attacker**: delays, drops, reorders, replays messages; attempts amplification/spam
- **RPC user**: submits txs, queries chain (local RPC mode)

### Trust assumptions (explicit)

- Peer public keys are distributed out-of-band and are correct (static peer registry).
- Validator membership is configured out-of-band (static validator set for v0).
- Cryptography primitives behave as expected (Ed25519, hashing).
- Nodes do not require inbound ports; transport can deliver messages (possibly via relays).

### Attack classes & desired mitigations

#### A) Message spoofing / identity forgery

- **Goal**: attacker sends an envelope that appears to be from another agent.
- **Mitigation**: verify envelope signature using registry pubkey; bind envelope `FromAgentID` to the verified key.
- **Reviewer focus**: ensure no path accepts unverified payloads into consensus/ledger state.

#### B) Replay attacks

- **Goal**: replay old votes/proposals to trigger inconsistent finality or resource exhaustion.
- **Mitigation**: freshness window checks; replay cache / monotonic counters (if present).
- **Reviewer focus**: are there unique message IDs? is “freshness” enforced everywhere? are votes idempotent?

#### C) Spam / DoS via transport

- **Goal**: overwhelm handler loops with junk JSON / oversized payloads / high-rate messages.
- **Mitigation**: per-peer rate limiting; size limits; early JSON rejection; cheap prechecks before expensive crypto.
- **Reviewer focus**: missing payload size bounds; expensive verification before basic validation.

#### D) Invalid block proposals

- **Goal**: get honest validators to vote for a block that doesn’t match state transitions.
- **Mitigation**: deterministic validation by replay and state root comparison; reject non-tip parent; reject timestamps.
- **Reviewer focus**: block validity checks; tx validation rules; mempool ordering determinism assumptions.

#### E) Consensus safety (equivocation, conflicting finality)

- **Goal**: finalize conflicting blocks at same height.
- **Mitigation**: fork-choice / single-chain assumption + validator behavior rules; equivocation detection; signed votes.
- **Reviewer focus**: the current engine is “2/3+ votes-for per proposal hash”; assess if/when it can finalize conflicting blocks across partitions and what additional rules are required.

#### F) RPC abuse (local mode)

- **Goal**: crash node or corrupt snapshot via malformed RPC requests.
- **Mitigation**: strict request validation; bounded inputs; safe persistence.
- **Reviewer focus**: JSON decoding, error handling, snapshot consistency.

### Open questions (intended audit feedback)

- What additional invariants are required to prevent conflicting finality?
- What is the minimum replay-protection mechanism you recommend for envelopes?
- Should proposer selection be explicitly defined (round-robin, VRF, stake-weighted), and how should it be verified?
- What changes are needed before exposing a public network transport?

