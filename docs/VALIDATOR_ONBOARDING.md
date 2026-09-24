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

## Phase 2 -- live validator-set refresh (shipped)

Every `synthosd` process used to resolve its trusted validator set exactly
once, at startup, from static config (`cmd/synthosd/main.go`'s
`resolveConsensusValidators` / `resolveChainQuorum` /
`chain.Chain.SetValidatorSet`). That startup-only path still exists and is
still what a fresh process boots from; what's new is a background loop that
keeps it current afterward, so an approval from Phase 1's queue actually
takes effect on an already-running network without a redeploy.

`cmd/synthosd/main.go`'s `startValidatorRosterSync` runs once per minute
(mirroring the existing 15-second `StartPeerSync` catch-up loop's cadence
and style, just slower since validator-set changes are rarer and higher
stakes than ordinary catch-up gossip) and:

1. Fetches `GET /api/validators/roster` from the registry.
2. `mergeValidatorRoster` computes the union of the node's static
   `trusted_validators`/`peer_keys` and the dynamic roster -- separately
   for the Chain's enforced roster and the Engine's local quorum roster,
   preserving `resolveChainQuorum`'s documented distinction between the
   two rather than assuming they're always identical.
3. Recomputes the required quorum for the new total (2/3-plus-one of the
   combined set, via `consensus.Engine.RequiredForFinality`, not a fixed
   number), registers any newly-approved validator's key via
   `node.Node.AddPeer`, and re-calls `chain.SetValidatorSet` and
   `node.Node.SetValidators` with the updated set.
4. Adds a newly-approved validator's `public_url` to
   `rpc.Server.ConsensusPeerURLs` (via `SetConsensusPeerURLs`) only when
   the registry marks it `reachable` -- an operator with no reachable URL
   (the home-PC case) becomes a trusted signer but isn't yet asked to
   vote directly; see Phase 3.
5. Fails soft: if the registry is unreachable, the tick is skipped and the
   last-known set is kept rather than shrinking back to just the static
   config (a registry outage must never be able to strand or halt the
   existing three-validator network).
6. Is idempotent: calling `mergeValidatorRoster` twice with the same
   roster is a no-op, and it produces the identical result whether a
   validator was in the set from process start or added on a later
   refresh.

This also required a real concurrency fix, found and closed as part of
shipping this: `rpc.Server.ConsensusPeerURLs` used to be safe to read
directly from the block-producer's long-running goroutine only because it
was set exactly once, synchronously, before that goroutine ever started --
this refresh loop turns that into a genuine second concurrent writer, so
`ConsensusPeerURLs` is now guarded by its own mutex
(`consensusPeerURLsMu`), with `ConsensusPeerURLsSnapshot()` as the safe way
to read it from outside the package. A second, subtler instance of the same
class of bug was caught by this feature's own integration test (run under
`-race`, with concurrent Engine reads deliberately hammering it the whole
time): `consensus.Engine.RequiredForFinality()` read `totalValidators` with
no lock at all, safe for the same "only ever written once at startup"
reason -- SetValidators now has a second, periodic caller too, so
`RequiredForFinality()` takes `e.mu` itself, with an unexported
`requiredForFinalityLocked` for the call sites that already hold it.

Tests: `cmd/synthosd/validator_roster_sync_test.go` covers
`mergeValidatorRoster` (union/dedup/self-exclusion/invalid-key-handling/
idempotency/empty-roster-reproduces-baseline), `fetchValidatorRoster`
(success/non-2xx/malformed-body), and an end-to-end
`startValidatorRosterSync` test against a real `Chain`/`Engine`/`Node`/
`Server` stack and a fake registry, proving the live quorum genuinely
grows on approval and shrinks back on revocation, plus the reachable-vs-
not distinction for `ConsensusPeerURLs`. `internal/rpc/
consensus_peer_urls_race_test.go` covers the `ConsensusPeerURLs` fix in
isolation.

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

With Phase 2 shipped, "verified uptime -> appears in the pending queue ->
project owner approves -> the node is a real trusted signer within a
minute, without a redeploy" is now genuinely true -- for an operator with a
reachable `public_url` (approved with one, e.g. a real server). For a
home-PC operator (no `public_url`, the common case for the public
push-button download), approval makes them trusted but they still can't be
asked to vote directly -- nothing can reach them -- until Phase 3's mailbox
relay ships. So the node page should still describe this accurately today:
uptime is paid; validator promotion is a reviewed, manual step, not
automatic; and it takes effect immediately for a directly reachable
operator, but a home-PC operator's approval doesn't yet mean their node is
actually voting. Once Phase 3 ships, that last gap closes too and the
original "run a node for a month and get approved" promise becomes true
end to end for every operator, home PC included.
