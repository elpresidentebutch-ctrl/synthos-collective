# Validator Onboarding: from candidate to real validator

This document specifies how an outside operator goes from downloading the
node software to actually voting in consensus, and what's built versus
still designed. It exists because the node onboarding page previously
implied this happened automatically after "one verified month" -- it
doesn't, and this doc is both the honest explanation of why, and the real
plan to make it true.

## The two things that have to be true

Today, "the validator set" means exactly this: three Render services
(`synthos-validator-12`, `synthos-validator-13`, `synthos-rpc`) whose keys
are hand-typed into `config/render-*.json` (`trusted_validators`,
`peer_keys`), redeployed by the project owner whenever it changes. For a
fourth, outside-operated node to become a real, participating validator,
two independent things both have to happen:

1. **Someone has to decide to trust it.** Adding a key to the validator set
   is a consensus-safety decision, not a rewards one -- a forged or
   careless addition can let a malicious signer vote on blocks. This should
   stay a deliberate act by the project owner, not something that flips on
   automatically no matter how long a candidate has been running.
2. **The other validators have to be able to reach it.** Real votes are
   gathered by validator-to-validator HTTPS calls (`POST /consensus/
   propose`, see `internal/rpc/server.go`'s `ProposeBlockWithConsensus`).
   The public "push the button" download (`cmd/silentnode`) is deliberately
   outbound-only -- no listening port, so a home/laptop operator never
   needs to open a firewall port or have a static IP. But that also means
   nothing can call it. Being trusted and being reachable are separate
   problems, and both must be solved before a home-PC operator can actually
   vote.

## Phase 1 -- the approval queue (shipped)

`cmd/cloudless-registry/main.go` now tracks validator promotion as an
explicit field on each peer (`ValidatorApproved`, `ValidatorApprovedAt`,
`ValidatorPublicURL`), separate from `reward eligible` (which already
existed and just means "has a real, verified month of uptime and is owed a
payout" -- see `nodeStatus`'s `accepted`/`rewardEligible` computation).

New endpoints:

- `GET /api/admin/validators/pending` (requires `X-Registry-Secret` when
  `REGISTRY_SECRET` is set) -- lists every `validator_candidate` that has
  earned a full verified month of real uptime and isn't approved yet. This
  is the "who's ready" queue: check it instead of manually reading through
  candidate uptime numbers.
- `POST /api/admin/validators/{id}/approve` (same auth) -- grants
  `ValidatorApproved`. Requires the candidate to already have a registered
  Ed25519 public key (from a real signed heartbeat) -- there's nothing to
  trust as a signer without one. Does **not** require the candidate to be
  on the pending list: the project owner may reasonably approve someone
  before a full month elapses (a known operator, a second machine, a test).
  Optional body `{"public_url": "https://..."}` records a directly
  reachable endpoint, for an operator running on a real server instead of a
  home PC.
- `POST /api/admin/validators/{id}/revoke` (same auth) -- the undo button.
- `GET /api/validators/roster` (public, no auth) -- lists every approved
  validator's ID, public key, and whether it has a direct URL
  (`reachable: true/false`).

This closes the "trust" half of the problem: approving someone is now one
API call instead of hand-editing a Render config file and redeploying. It
does **not** yet close the "reachability" half, and it does not yet make a
live `synthosd` process pick up a newly approved validator automatically
-- see Phases 2 and 3.

## Phase 2 -- live validator-set refresh (designed, not yet built)

Every `synthosd` process resolves its trusted validator set exactly once,
at startup, from static config (`cmd/synthosd/main.go`'s
`resolveConsensusValidators` / `resolveChainQuorum` /
`chain.Chain.SetValidatorSet`). This was deliberately built that way, with
extensive rollout-safety gating (see that file's doc comments), so a bug
here is exactly the class of incident this project has already hit twice
(the false-self-slashing bugs both came from consensus code that was
under-tested for exactly this kind of edge case).

The plan: add a periodic refresh (mirroring the existing 15-second
`StartPeerSync` catch-up loop's cadence and style) that:

1. Fetches `GET /api/validators/roster` from the registry.
2. Computes the union of the node's static `trusted_validators`/
   `peer_keys` and the dynamic roster.
3. Recomputes the required quorum for the new total (2/3-plus-one of the
   combined set, not a fixed number), and re-calls `chain.SetValidatorSet`
   with the updated key map and quorum.
4. Fails soft: if the registry is unreachable, keep the last-known set
   rather than shrinking back to just the static config (a registry outage
   must never be able to strand or halt the existing three-validator
   network).
5. Is idempotent: calling it twice with the same roster must be a no-op,
   and it must produce the identical result whether a validator was in the
   set from process start or added on a later refresh.

This is the piece that makes an approval in Phase 1 actually take effect on
a running network without a redeploy. It should ship as its own commit with
its own dedicated test suite (constructing a live `Chain`/`Engine`/`Node`
trio, proving the union/quorum math, the fail-soft behavior, and the
idempotency, the same rigor as every other consensus change in this repo's
history) before Phase 3 builds on top of it.

## Phase 3 -- reaching a home-PC validator (designed, not yet built)

A newly approved validator with no `ValidatorPublicURL` still can't be
called. The plan reuses a piece of infrastructure that already exists and
is already deployed: the registry's generic mailbox
(`POST /mailbox`, `GET /mailbox?name=NODE`, in `cmd/cloudless-registry/
main.go`'s `handleMailbox`) -- a named, persisted, pull-based message queue
originally built for the "cloudless" agent-messaging design, and a natural
fit here:

1. When a producer's `ProposeBlockWithConsensus` reaches a consensus peer
   with no known reachable URL, instead of `POST <url>/consensus/propose`
   it does `POST /mailbox {"to": "<peerID>", "type": "consensus_proposal",
   "payload": <the signed proposal>}`.
2. The relay-only validator's process (not `cmd/silentnode` -- it needs the
   real proposal-validation and vote-signing logic in `internal/node`, just
   driven by polling instead of an inbound listener) polls
   `GET /mailbox?name=<its own ID>` every couple of seconds -- comfortably
   inside the existing round window (`SYNTHOS_PRODUCER_ROUND_SECONDS`,
   default 3x the block interval) -- and still purely outbound, so it still
   runs fine behind a home router.
3. On receiving a proposal, it runs the exact same validation
   (`ValidateProposal`) and self-signing (`SelfVote`) logic every validator
   already runs today, then posts its signed vote back via
   `POST /mailbox {"to": "<producer ID>", "type": "consensus_vote", ...}`.
4. The producer, which is directly reachable, polls its own mailbox for
   matching votes while a round is open and feeds them into the existing
   vote-counting path exactly as a directly-received vote would be.

**Why this doesn't weaken consensus safety:** the mailbox is a dumb,
unauthenticated (today; `REGISTRY_SECRET` is unset in production, so
`/mailbox` POSTs are already open to anyone) message queue. It never needs
to be trusted, because every proposal and vote it carries is independently
signed and independently re-verified by the receiving side exactly as it
is today -- `chain.Chain`'s own authorization check re-verifies every
proposer and quorum signature "regardless of what this orchestration code
believes" (existing doc comment on the consensus-wiring commit). A
compromised or malicious registry can delay or drop relay messages -- at
worst causing a missed vote or a failed round, which the existing rotation/
failover logic already handles -- but it cannot forge a vote or finalize an
unearned block. This preserves the project's existing trust-minimization
property rather than introducing a new one.

## What this means for the website promise

Once Phases 2 and 3 ship, "run a node for a month and get approved" can
become genuinely true end to end: verified uptime -> appears in the
pending queue -> project owner approves -> the node starts actually
receiving proposals and voting within a couple of poll cycles, all without
a redeploy, and without requiring the operator to run anything other than
the existing push-button download. Until then, the node page should keep
describing this accurately: uptime is paid, and validator promotion is a
reviewed, manual step -- not an automatic one.
