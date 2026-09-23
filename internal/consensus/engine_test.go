package consensus

import (
	"errors"
	"fmt"
	"testing"

	"synthos-collective/internal/chain"
)

func TestEngine_RequiredForFinality(t *testing.T) {
	tests := []struct {
		n        int
		required int
	}{
		{0, 1}, {1, 1}, {2, 2}, {3, 2}, {4, 3}, {10, 7},
	}
	for _, tc := range tests {
		e := NewEngine(tc.n)
		if got := e.RequiredForFinality(); got != tc.required {
			t.Fatalf("N=%d: want required %d, got %d", tc.n, tc.required, got)
		}
	}
}

func TestEngine_OnVote_Finality(t *testing.T) {
	e := NewEngine(4)
	e.SetValidators([]string{"v1", "v2", "v3", "v4"})
	block := &chain.Block{Header: chain.BlockHeader{Height: 1}, Hash: "0xabc"}
	e.OnProposal(block)

	var finalized bool
	var err error
	for i := 1; i <= 3; i++ {
		finalized, _, _, err = e.OnVote(BlockVote{
			BlockHash: block.Hash,
			Height:    1,
			VoterID:   fmt.Sprintf("v%d", i),
			Vote:      1,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !finalized {
		t.Fatal("expected finality after 3 of 4 votes")
	}

	finalized, votesFor, _, err := e.OnVote(BlockVote{
		BlockHash: block.Hash,
		Height:    1,
		VoterID:   "v2",
		Vote:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if votesFor != 3 || !finalized {
		t.Fatalf("duplicate vote changed result: finalized=%v votes=%d", finalized, votesFor)
	}
}

// TestEngine_RecordOwnProposal_AlwaysSupersedesRegardlessOfHash reproduces
// the exact livelock found live in production: a producer retries an
// HTTP-consensus round that didn't reach quorum, building a fresh
// candidate whose hash does NOT happen to sort below the stale one already
// registered for that height. With the old OnProposal-based path, the
// stale entry would never be replaced and a subsequent OnVote for the new
// candidate would fail with ErrUnknownProposal forever. RecordOwnProposal
// must unconditionally replace it instead.
func TestEngine_RecordOwnProposal_AlwaysSupersedesRegardlessOfHash(t *testing.T) {
	e := NewEngine(1)
	e.SetValidators([]string{"producer"})

	stale := &chain.Block{Header: chain.BlockHeader{Height: 5, ProposerID: "producer"}, Hash: "0xAAA_this_sorts_first"}
	e.RecordOwnProposal(stale)

	fresh := &chain.Block{Header: chain.BlockHeader{Height: 5, ProposerID: "producer"}, Hash: "0xZZZ_this_sorts_last"}
	e.RecordOwnProposal(fresh)

	got, ok := e.Proposal(fresh.Hash)
	if !ok || got != fresh {
		t.Fatalf("expected the fresh retry to become canonical even though its hash sorts after the stale one")
	}

	// The self-vote that would have failed live in production.
	finalized, votesFor, _, err := e.OnVote(BlockVote{
		BlockHash: fresh.Hash,
		Height:    5,
		VoterID:   "producer",
		Vote:      1,
	})
	if err != nil {
		t.Fatalf("self-vote for the freshest own proposal must succeed, got: %v", err)
	}
	if votesFor != 1 || !finalized {
		t.Fatalf("expected self-vote to count toward the fresh candidate: votesFor=%d finalized=%v", votesFor, finalized)
	}
}

// TestEngine_RecordOwnProposal_ClearsStaleVotes proves a retry doesn't let
// votes cast for the abandoned candidate silently carry over and count
// toward the new one.
func TestEngine_RecordOwnProposal_ClearsStaleVotes(t *testing.T) {
	e := NewEngine(3)
	e.SetValidators([]string{"producer", "v2", "v3"})

	stale := &chain.Block{Header: chain.BlockHeader{Height: 9, ProposerID: "producer"}, Hash: "0xstale"}
	e.RecordOwnProposal(stale)
	if _, _, _, err := e.OnVote(BlockVote{BlockHash: stale.Hash, Height: 9, VoterID: "v2", Vote: 1}); err != nil {
		t.Fatalf("vote on stale candidate: %v", err)
	}

	fresh := &chain.Block{Header: chain.BlockHeader{Height: 9, ProposerID: "producer"}, Hash: "0xfresh"}
	e.RecordOwnProposal(fresh)

	finalized, votesFor, required, ok := e.FinalityStatus(fresh.Hash)
	if !ok {
		t.Fatal("expected the fresh candidate to be known")
	}
	if votesFor != 0 || finalized {
		t.Fatalf("v2's vote for the abandoned stale candidate must not carry over: votesFor=%d finalized=%v required=%d", votesFor, finalized, required)
	}
}

// TestEngine_RecordOwnProposal_NeverFalselySlashesOnRetry reproduces the
// second half of the live production bug: OnProposal reported every
// re-registration to the SlashingTracker as a double-sign candidate, so a
// producer retrying its own unfinalized round was slashing its own real
// balance on every single retry. RecordOwnProposal must never do that.
func TestEngine_RecordOwnProposal_NeverFalselySlashesOnRetry(t *testing.T) {
	e := NewEngine(1)
	e.SetValidators([]string{"producer"})
	tracker := NewSlashingTracker(SlashingParams{DoubleSignPenalty: 1000})
	slashed := false
	tracker.SetExecuteSlash(func(validatorID string, penalty uint64) { slashed = true })
	e.SetSlashingTracker(tracker)

	for i := 0; i < 5; i++ {
		b := &chain.Block{
			Header: chain.BlockHeader{Height: 12, ProposerID: "producer"},
			Hash:   fmt.Sprintf("0xretry%d", i),
		}
		e.RecordOwnProposal(b)
	}

	if slashed {
		t.Fatal("a producer retrying its own unfinalized round must never be slashed for double-signing")
	}
	if n := tracker.TotalSlashEvents(); n != 0 {
		t.Fatalf("expected zero slashing events from own-proposal retries, got %d", n)
	}
}

// TestEngine_RecordOwnProposal_NeverFalselySlashesSelfVoteOnRetry
// reproduces a second, separate false-slashing bug in the same family as
// TestEngine_RecordOwnProposal_NeverFalselySlashesOnRetry above, but on the
// OnVote/RecordEquivocation path instead of OnProposal/DetectDoubleSigning.
//
// ProposeBlockWithConsensus calls SelfVote (-> Engine.OnVote) on every
// retry, for whatever fresh candidate that retry just built. Before this
// fix, RecordEquivocation's own height-keyed memory of "the first hash
// this validator voted for" was never cleared by RecordOwnProposal, so a
// producer's second retry -- voting for its own second, different
// candidate at the still-unfinalized height -- looked identical to a real
// safety violation and triggered a real balance penalty. This was dormant
// only because no real transaction activity had yet made two retries at
// one height actually build different-hash blocks.
func TestEngine_RecordOwnProposal_NeverFalselySlashesSelfVoteOnRetry(t *testing.T) {
	e := NewEngine(1)
	e.SetValidators([]string{"producer"})
	tracker := NewSlashingTracker(SlashingParams{DoubleSignPenalty: 1000})
	slashed := false
	tracker.SetExecuteSlash(func(validatorID string, penalty uint64) { slashed = true })
	e.SetSlashingTracker(tracker)

	for i := 0; i < 5; i++ {
		b := &chain.Block{
			Header: chain.BlockHeader{Height: 12, ProposerID: "producer"},
			Hash:   fmt.Sprintf("0xretry%d", i),
		}
		e.RecordOwnProposal(b)
		if _, _, _, err := e.OnVote(BlockVote{BlockHash: b.Hash, Height: 12, VoterID: "producer", Vote: 1}); err != nil {
			t.Fatalf("retry %d: self-vote for the freshest own proposal must succeed, got: %v", i, err)
		}
	}

	if slashed {
		t.Fatal("a producer self-voting on its own retried candidates must never be slashed for equivocation")
	}
	if n := tracker.TotalSlashEvents(); n != 0 {
		t.Fatalf("expected zero slashing events from own-proposal retry self-votes, got %d", n)
	}
}

// TestEngine_RecordOwnProposal_PeerVotesAcrossRetriesNeverFalselySlashPeer
// proves the same fix also protects a PEER's vote history, not just the
// producer's own: a follower asked to vote on retry after retry (each a
// fresh candidate for the same still-unfinalized height) must never be
// flagged as equivocating just because the producer kept abandoning and
// replacing its own candidate.
func TestEngine_RecordOwnProposal_PeerVotesAcrossRetriesNeverFalselySlashPeer(t *testing.T) {
	e := NewEngine(2)
	e.SetValidators([]string{"producer", "peer"})
	tracker := NewSlashingTracker(SlashingParams{DoubleSignPenalty: 1000})
	slashed := false
	tracker.SetExecuteSlash(func(validatorID string, penalty uint64) { slashed = true })
	e.SetSlashingTracker(tracker)

	for i := 0; i < 3; i++ {
		b := &chain.Block{
			Header: chain.BlockHeader{Height: 20, ProposerID: "producer"},
			Hash:   fmt.Sprintf("0xround%d", i),
		}
		e.RecordOwnProposal(b)
		if _, _, _, err := e.OnVote(BlockVote{BlockHash: b.Hash, Height: 20, VoterID: "peer", Vote: 1}); err != nil {
			t.Fatalf("round %d: peer's vote on the current candidate must succeed, got: %v", i, err)
		}
	}

	if slashed {
		t.Fatal("a peer honestly voting on each retry's fresh candidate must never be slashed for equivocation")
	}
	if n := tracker.TotalSlashEvents(); n != 0 {
		t.Fatalf("expected zero slashing events, got %d", n)
	}
}

// TestEngine_ForgetVotesAtHeight_StillDetectsRealEquivocationAfterAReset is
// a guard rail on the fix above: ForgetVotesAtHeight must only forgive a
// vote-hash change that happens because of a genuine round reset
// (RecordOwnProposal/RecordReceivedProposal), not open a permanent hole --
// a peer that votes for the CURRENT candidate and then, with no further
// reset, also votes for a DIFFERENT hash at the same height is still
// equivocating and must still be caught.
func TestEngine_ForgetVotesAtHeight_StillDetectsRealEquivocationAfterAReset(t *testing.T) {
	e := NewEngine(2)
	e.SetValidators([]string{"producer", "peer"})
	tracker := NewSlashingTracker(SlashingParams{DoubleSignPenalty: 1000})
	var slashedValidator string
	tracker.SetExecuteSlash(func(validatorID string, penalty uint64) { slashedValidator = validatorID })
	e.SetSlashingTracker(tracker)

	b1 := &chain.Block{Header: chain.BlockHeader{Height: 30, ProposerID: "producer"}, Hash: "0xretryA"}
	e.RecordOwnProposal(b1)
	if _, _, _, err := e.OnVote(BlockVote{BlockHash: b1.Hash, Height: 30, VoterID: "peer", Vote: 1}); err != nil {
		t.Fatalf("first vote on the current candidate: %v", err)
	}
	if n := tracker.TotalSlashEvents(); n != 0 {
		t.Fatalf("no reset happened yet, expected zero slash events, got %d", n)
	}

	// No RecordOwnProposal/RecordReceivedProposal reset here -- peer now
	// equivocates for real, voting for a second, different hash at the
	// same still-canonical height.
	_, _, _, _ = e.OnVote(BlockVote{BlockHash: "0xsomethingElse", Height: 30, VoterID: "peer", Vote: 1})

	if slashedValidator != "peer" {
		t.Fatalf("expected peer to still be slashed for a real equivocation with no intervening reset, got slashedValidator=%q", slashedValidator)
	}
	if n := tracker.TotalSlashEvents(); n != 1 {
		t.Fatalf("expected exactly 1 slash event, got %d", n)
	}
}

// TestEngine_OnProposal_StillDetectsRealDoubleSigning is a guard rail: the
// fix above must not weaken OnProposal itself. A validator whose proposals
// arrive via the network path (HandleProposal, observing what OTHER nodes
// broadcast) genuinely proposing two different blocks at one height is
// still real equivocation and must still be reported.
func TestEngine_OnProposal_StillDetectsRealDoubleSigning(t *testing.T) {
	e := NewEngine(1)
	e.SetValidators([]string{"attacker"})
	tracker := NewSlashingTracker(SlashingParams{DoubleSignPenalty: 1000})
	var slashedValidator string
	var slashedPenalty uint64
	tracker.SetExecuteSlash(func(validatorID string, penalty uint64) {
		slashedValidator = validatorID
		slashedPenalty = penalty
	})
	e.SetSlashingTracker(tracker)

	e.OnProposal(&chain.Block{Header: chain.BlockHeader{Height: 7, ProposerID: "attacker"}, Hash: "0xone"})
	e.OnProposal(&chain.Block{Header: chain.BlockHeader{Height: 7, ProposerID: "attacker"}, Hash: "0xtwo"})

	// executeSlash is now called synchronously (see slashing.go), so this
	// is safe to check immediately with no synchronization needed.
	if slashedValidator != "attacker" || slashedPenalty != 1000 {
		t.Fatalf("a validator proposing two different blocks at the same height via the network path must still be detected as double-signing: got validator=%q penalty=%d", slashedValidator, slashedPenalty)
	}
	if n := tracker.TotalSlashEvents(); n != 1 {
		t.Fatalf("expected exactly 1 slashing event, got %d", n)
	}
}

func TestEngine_OnVote_RejectsUnknownValidator(t *testing.T) {
	e := NewEngine(2)
	e.SetValidators([]string{"v1", "v2"})
	e.OnProposal(&chain.Block{Header: chain.BlockHeader{Height: 1}, Hash: "0xabc"})

	finalized, votesFor, required, err := e.OnVote(BlockVote{
		BlockHash: "0xabc",
		Height:    1,
		VoterID:   "attacker",
		Vote:      1,
	})
	if !errors.Is(err, ErrUnknownValidator) {
		t.Fatalf("expected ErrUnknownValidator, got %v", err)
	}
	if finalized || votesFor != 0 || required != 2 {
		t.Fatalf("rejected vote changed finality: finalized=%v votes=%d required=%d", finalized, votesFor, required)
	}
}

func TestEngine_DeterministicCompetingProposalChoice(t *testing.T) {
	a := &chain.Block{Header: chain.BlockHeader{Height: 7}, Hash: "0xbbb"}
	b := &chain.Block{Header: chain.BlockHeader{Height: 7}, Hash: "0xaaa"}

	first := NewEngine(3)
	first.OnProposal(a)
	first.OnProposal(b)

	second := NewEngine(3)
	second.OnProposal(b)
	second.OnProposal(a)

	for _, e := range []*Engine{first, second} {
		if _, ok := e.Proposal("0xaaa"); !ok {
			t.Fatal("expected lexicographically smallest block hash to win")
		}
		if _, ok := e.Proposal("0xbbb"); ok {
			t.Fatal("non-canonical competing proposal remained selectable")
		}
	}
}

func TestEngine_SetValidators_UsesUniqueNonEmptyIDs(t *testing.T) {
	e := NewEngine(99)
	e.SetValidators([]string{"v1", "", "v1", "v2"})
	if got := e.RequiredForFinality(); got != 2 {
		t.Fatalf("expected threshold for 2 unique validators, got %d", got)
	}
}
