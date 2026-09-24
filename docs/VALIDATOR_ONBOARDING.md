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

## Phase 3 -- reaching a home-PC validator (shipped)

A newly approved validator with no `ValidatorPublicURL` used to become a
trusted signer (Phase 2) but stay uncalled forever -- nothing could reach
it to ask for a vote. This closes that gap by reusing a piece of
infrastructure that already existed and was already deployed: the
registry's generic mailbox (`POST /mailbox`, `GET /mailbox?name=NODE`, in
`cmd/cloudless-registry/main.go`'s `handleMailbox`) -- a named, persisted,
pull-based message queue originally built for the "cloudless"
agent-messaging design, and a natural fit here:

1. `internal/rpc/mailbox_relay.go`'s `collectMailboxVotes`, called from
   `collectConsensusVotes` alongside (not after -- both run for the same
   timeout budget) the existing direct-HTTP peer loop: for every validator
   in `rpc.Server.MailboxRelayPeers` (populated each tick by
   `cmd/synthosd/main.go`'s `mailboxRelayPeersFromRoster`, straight from
   the same roster tick `mergeValidatorRoster` already reads -- see Phase
   2), it `POST /mailbox {"to": "<peerID>", "from": "<selfID>", "type":
   "consensus_proposal", "payload": {"proposal": ...}}`, then polls its
   own mailbox (`GET /mailbox?name=<selfID>`) every 400ms until either
   every relay peer has answered or the round's timeout elapses.
2. `cmd/synthosd/mailbox_relay.go`'s `startMailboxRelayListener` runs
   inside every `synthosd` process (not `cmd/silentnode` -- it needs the
   real proposal-validation and vote-signing logic in `internal/node`,
   which only `synthosd` has), polling its own mailbox every 2 seconds --
   comfortably inside the existing round window
   (`SYNTHOS_PRODUCER_ROUND_SECONDS`, default 3x the block interval) --
   and still purely outbound, so it still runs fine behind a home router.
   Harmless to run unconditionally on every node: a directly-reachable
   validator is never listed in anyone's `MailboxRelayPeers`, so its own
   mailbox just stays empty.
3. On receiving a `consensus_proposal` message, it calls the exact same
   `node.Node.HandleProposal` every validator already runs for the
   direct-HTTP path -- real height/signature/quorum-lock validation, then
   a real signed vote -- and posts that vote back via
   `POST /mailbox {"to": "<producer ID>", "type": "consensus_vote", ...}`.
   A proposal that fails validation is logged and dropped, exactly like a
   rejected direct-HTTP call would be; it never crashes the loop.
4. The producer's poll in step 1 decodes each `consensus_vote` reply and
   runs it through `verifyPeerVote` -- the identical independent
   ed25519-signature check a directly-received vote goes through -- before
   it ever reaches `Node.HandleVote`. A relay peer that never answers,
   answers late, or replies with something that doesn't verify is simply
   absent from the round's tally, matching normal BFT handling of a peer
   that's down or misbehaving.

**Why this doesn't weaken consensus safety:** the mailbox is a dumb,
unauthenticated (today; `REGISTRY_SECRET` is unset in production, so
`/mailbox` POSTs are already open to anyone) message queue. It never needs
to be trusted, because every proposal and vote it carries is independently
signed and independently re-verified by the receiving side exactly as it
is today -- `chain.Chain`'s own authorization check re-verifies every
proposer and quorum signature "regardless of what this orchestration code
believes" (existing doc comment on the consensus-wiring commit), and
`verifyPeerVote` (step 4 above) is real, tested defense-in-depth on top of
that, not just a doc-comment claim: `TestCollectMailboxVotes_
DiscardsForgedVote` proves a message claiming to be a valid vote but
carrying a bogus signature is actually discarded, not just trusted because
it arrived with the right shape. A compromised or malicious registry can
delay or drop relay messages -- at worst causing a missed vote or a failed
round, which the existing rotation/failover logic already handles -- but it
cannot forge a vote or finalize an unearned block. This preserves the
project's existing trust-minimization property rather than introducing a
new one.

Tests: `internal/rpc/mailbox_relay_test.go` covers the producer side end to
end against a real second validator (`TestProposeBlockWithConsensus_
ReachesQuorumViaMailboxRelay` -- 2 validators, quorum 2, so finalizing at
all proves the relay vote genuinely arrived and verified), the forged-vote
rejection above, and the fail-soft timeout behavior for an unanswered relay
peer. `cmd/synthosd/mailbox_relay_test.go` covers the relay-listener side
end to end (`TestStartMailboxRelayListener_ValidatesAndPostsVoteBack` --
proves the posted-back vote's signature independently verifies against the
relay validator's real public key), rejection of an invalid proposal
without crashing the loop, the rollout-safety gating, and
`mailboxRelayPeersFromRoster`'s own union/exclusion logic.

## What this means for the website promise

With Phases 2 and 3 both shipped, "verified uptime -> appears in the
pending queue -> project owner approves -> the node actually receives
proposals and votes within a couple of poll cycles, without a redeploy" is
now genuinely true end to end -- for a directly reachable operator (Phase
2's consensus-peer path) and for a home-PC operator with no public URL
(Phase 3's mailbox relay) alike, using nothing but the existing push-button
download. So the node page's "run a node for a month and get approved" can
now describe the real, current behavior rather than something still in
progress: uptime is paid, and validator promotion is still a reviewed,
manual step (deliberately -- see "the two things that have to be true"
above), but once approved, an operator's node -- home PC included -- is
actually voting, not just carrying a flag nothing acts on.
