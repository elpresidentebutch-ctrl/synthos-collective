## SYNTHOS Collective — Architecture (Go implementation)

### Components (code map)

- **Ledger / state machine**: `internal/chain`
  - `Chain`: blocks + mempool + state
  - `State`: account balances, nonce, root hashing
  - `Tx`: signed transfer-like transaction
  - `Block`: header + tx list + computed hash + state root
  - `Genesis`: deterministic chain initialization

- **Consensus finality engine**: `internal/consensus`
  - `Engine`: tracks proposals by hash and votes by (blockHash, voterID)
  - Finality: \( \lceil 2N/3 \rceil \) votes-for where \(N\) is total validators configured

- **Agent identity + envelope**: `internal/agent`, `internal/crypto`, `internal/network`
  - Agents produce signed envelopes containing typed payloads.
  - Nodes verify envelopes using a peer registry (agentID → public key).
  - Message freshness is enforced via time windows.

- **Node orchestration**: `internal/node`
  - `Node` binds: Agent + Chain + Consensus + Transport.
  - Validator gating: only configured validator agent IDs can propose/vote.
  - Inbound abuse resistance: per-peer rate limiting + basic “hardware hash consistency” checks.

- **Transports**: `internal/network`
  - `MemoryTransport`: in-process transport used by `cmd/devnet`
  - `RelayTransport`: placeholder for relay-based messaging (outbound-only nodes)

- **RPC + persistence**: `internal/rpc`, `internal/storage`
  - `cmd/rpcnode` exposes status/balance/mempool/submitTx and persists snapshots.

### Primary data flows

#### 1) Transaction submission → block inclusion

1. A tx is created and signed (`internal/chain/tx.go`).
2. Submitted to chain mempool (`Chain.SubmitTx`).
3. A validator proposes a block by building a deterministic candidate from mempool (`Chain.BuildBlock`).
4. Peers validate the proposed block by replaying txs to recompute state root (`Chain.ValidateBlock`).
5. Validators vote; when votes-for reaches \( \lceil 2N/3 \rceil \), the block can be finalized locally (`Chain.FinalizeBlock`).

#### 2) Block proposal broadcast → finality

1. Validator calls `Node.ProposeBlock()` / `Node.ProposeBlockHash()`.
2. Node stores proposal in consensus engine and broadcasts a signed envelope of type `block_proposal` on `consensus/proposals`.
3. Receivers:
   - rate-limit and JSON-decode the envelope,
   - lookup peer pubkey from registry,
   - verify signature + freshness,
   - validate proposed block against local tip/state,
   - record proposal, then (if validator) emit `block_vote`.
4. Receivers of votes:
   - verify envelope + freshness,
   - ensure voterID == envelope sender,
   - record vote in engine; if finalized, call `TryFinalize()`.

### What is “decentralization” in this architecture?

At the code level, decentralization emerges when:

- Multiple independent nodes run the same genesis and exchange signed envelopes over an open transport.
- The validator set is not controlled by a single operator.

In the current implementation, validator membership is configured out-of-band (`Node.SetValidators`). A production design typically moves validator set updates onchain and makes proposer selection deterministic and verifiable.

---

## Legal notice

SYNTHOS Collective, SYNTHOS, and related names, marks, documentation, and technical materials in this document are the **exclusive property of James G. Isham Williams, Sr.** Unauthorized reproduction, distribution, or commercial use without express written permission is prohibited except as allowed under applicable open-source licenses for identified files. No rights are waived.

This document is informational only and is not legal, financial, or investment advice. The canonical legal notice is in **docs/LEGAL_NOTICE.md** in the SYNTHOS Collective repository.
