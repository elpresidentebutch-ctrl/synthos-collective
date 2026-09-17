package consensus

import (
	"testing"

	"synthos-collective/internal/chain"
)

// TestEngineDetectsRealDoubleSigning proves that when a proposer submits two
// different blocks at the same height through the real Engine.OnProposal
// path, a real slashing event gets recorded -- not just logged, actually
// recorded in the tracker's history and reflected in SlashCount.
func TestEngineDetectsRealDoubleSigning(t *testing.T) {
	tracker := NewSlashingTracker(SlashingParams{DoubleSignPenalty: 500})
	eng := NewEngine(1)
	eng.SetSlashingTracker(tracker)

	b1 := &chain.Block{Hash: "hashA", Header: chain.BlockHeader{Height: 5, ProposerID: "validator-1"}}
	b2 := &chain.Block{Hash: "hashB", Header: chain.BlockHeader{Height: 5, ProposerID: "validator-1"}}

	eng.OnProposal(b1)
	if got := tracker.SlashCount("validator-1"); got != 0 {
		t.Fatalf("expected no slash after a single proposal, got %d", got)
	}

	eng.OnProposal(b2)
	if got := tracker.SlashCount("validator-1"); got != 1 {
		t.Fatalf("expected exactly 1 slash event after a conflicting second proposal at the same height, got %d", got)
	}
	if !tracker.IsJailed("validator-1") {
		t.Fatal("expected validator-1 to be jailed after double-signing")
	}

	history := tracker.HistoryFor("validator-1")
	if len(history) != 1 || history[0].EventType != DoubleSigning {
		t.Fatalf("expected a DoubleSigning event in history, got %+v", history)
	}
}

// TestEngineDetectsRealEquivocation proves that a validator approving two
// different block hashes at the same height gets slashed for equivocation
// through the real Engine.OnVote path.
func TestEngineDetectsRealEquivocation(t *testing.T) {
	tracker := NewSlashingTracker(SlashingParams{DoubleSignPenalty: 500})
	eng := NewEngine(3)
	eng.SetValidators([]string{"v1", "v2", "v3"})
	eng.SetSlashingTracker(tracker)

	b1 := &chain.Block{Hash: "hashA", Header: chain.BlockHeader{Height: 1, ProposerID: "v1"}}
	b2 := &chain.Block{Hash: "hashB", Header: chain.BlockHeader{Height: 1, ProposerID: "v1"}}
	eng.OnProposal(b1)
	// Manually register b2 into the same-height proposal map so a vote for
	// it is recognized as a known (if non-canonical) candidate. In a real
	// network this would arrive as its own proposal broadcast; here we just
	// need OnVote to be able to look it up.
	eng.proposalsByHeight[1] = b1 // keep b1 canonical for the finality check

	// v2 votes for b1 -- fine.
	if _, _, _, err := eng.OnVote(BlockVote{BlockHash: "hashA", Height: 1, VoterID: "v2", Vote: 1}); err != nil {
		t.Fatalf("unexpected error voting for canonical block: %v", err)
	}
	if got := tracker.SlashCount("v2"); got != 0 {
		t.Fatalf("expected no slash after a single honest vote, got %d", got)
	}

	// v2 now equivocates by approving a different hash at the same height.
	// OnVote only accepts votes matching the recorded canonical hash for
	// finality bookkeeping, but RecordEquivocation runs on the vote's own
	// claimed hash regardless, which is what actually matters for catching
	// the safety violation.
	_, _, _, _ = eng.OnVote(BlockVote{BlockHash: "hashB", Height: 1, VoterID: "v2", Vote: 1})
	_ = b2

	if got := tracker.SlashCount("v2"); got != 1 {
		t.Fatalf("expected v2 to be slashed once for equivocation, got %d slash events", got)
	}
}
