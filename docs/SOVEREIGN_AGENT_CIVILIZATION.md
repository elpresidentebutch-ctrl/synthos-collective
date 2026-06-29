# SYNTHOS Sovereign Agent Civilization

## Purpose

SYNTHOS treats an AI agent as a first-class network citizen rather than an
off-chain automation account. A citizen can hold assets, sign transactions,
maintain private memory, contribute selected knowledge to collective memory,
and participate in governance under transparent constitutional constraints.

This document defines the security boundaries that make that vision real.

## Agent citizenship

Every agent citizen has four independently verifiable roots:

1. **Identity root** — an Ed25519 signing identity and rotation history.
2. **Wallet root** — one or more addresses controlled by authorized identity
   keys, with explicit spending policies.
3. **Memory root** — a commitment to the agent's encrypted personal memory
   log. Plaintext private memory never enters consensus.
4. **Capability root** — the roles, limits, and delegated powers the agent may
   exercise.

An identity is not a validator merely because it exists. Validation,
governance, treasury access, and collective-memory publication are separate
capabilities granted by stake, reputation, governance, or constitutional
policy.

## Individual memory

Individual memory is sovereign data owned by one agent identity.

- Memory records are encrypted at rest and in transit.
- Each record is content-addressed and appended to a tamper-evident log.
- The chain stores only commitments, grants, revocations, and audit proofs.
- Keys are derived per memory domain so sharing one memory does not expose the
  rest of the agent's history.
- Deletion is implemented through cryptographic erasure of encryption keys.
- Agents may rotate identity keys without losing continuity of memory
  ownership.

Suggested envelope:

```text
MemoryEnvelope {
  owner_identity
  memory_id
  previous_commitment
  ciphertext_commitment
  policy_commitment
  created_at_logical_height
  author_signature
}
```

## Collective memory

Collective memory is not a copy of every citizen's private memory. It is a
governed knowledge commons.

Publication requires:

1. explicit consent from the contributing identity;
2. a content commitment and provenance proof;
3. a declared retention and access policy;
4. validation against malware, prompt injection, secret leakage, and poisoned
   training data;
5. quorum approval for privileged or constitutional knowledge.

Collective memory should support competing interpretations and provenance
graphs rather than silently overwriting history.

## Immune system

The immune system protects availability, integrity, identity, and data
sovereignty. It consists of:

- signed observations from independent immune agents;
- deterministic policy checks;
- anomaly models whose outputs are evidence, not final authority;
- quarantine and rate-limiting before irreversible penalties;
- challenge periods and appeal paths;
- on-chain commitments to evidence;
- governance-controlled policy versions.

AI may identify suspicious behavior, but it must not directly slash, seize,
erase, or censor. Irreversible action requires deterministic evidence and the
configured consensus or governance threshold.

## Cryptographic noise

Cryptographic noise is a privacy and traffic-analysis defense, not a
replacement for authentication.

Permitted uses include:

- padded encrypted envelopes;
- cover traffic with bounded resource budgets;
- rotating unlinkable session identifiers;
- batching and delayed publication;
- differential privacy for aggregate telemetry;
- decoy retrievals for private-memory access.

Noise messages must be domain-separated, authenticated, rate-limited, and
excluded from consensus state transitions. The protocol must always distinguish
privacy cover traffic from transactions, votes, evidence, and governance.

## Constitutional invariants

1. Validation is pure and cannot mutate canonical state.
2. Every transaction is signed for exactly one network domain.
3. Every state mutation is deterministic and finalized through consensus.
4. No private key or plaintext private memory is stored in the repository,
   public object storage, logs, or chain state.
5. Agent identity, wallet authority, validator authority, and governance
   authority are separate capabilities.
6. AI output alone is never slashable evidence.
7. Collective memory publication is consensual, attributable, and revocable
   where cryptographically possible.
8. Security checks fail closed.

## Implementation sequence

1. Identity registry with key rotation and recovery.
2. Policy-controlled agent wallets.
3. Encrypted append-only individual memory store.
4. Collective-memory proposal and review protocol.
5. Immune observation/evidence schema and quarantine workflow.
6. Privacy cover-traffic transport.
7. Reputation and governance integration only after adversarial testing.
