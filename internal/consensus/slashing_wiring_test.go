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

// TestDowntimeSlashingNeverExecutesRealBalancePenalty guards the fourth
// instance of this session's false-self-slashing bug class -- found by
// audit, not yet tripped live, but real and currently armed:
// RecordMissedBlock's only production caller (node.Node.NoteMissedSlot, fed
// by cmd/synthosd's block-producer loop) decides a slot was "missed" from
// this node's own local, unsynchronized clock racing a round-robin
// schedule -- not from anything every honest validator is guaranteed to
// derive identically. SYNTHOS_PRODUCER_ROTATION is on in live production
// right now, so this fires continuously, not just on some rare edge case.
// If it were still wired to a real balance debit, two honest validators
// disagreeing (via ordinary clock skew or network jitter) about exactly
// when a round timed out would silently apply that debit asymmetrically --
// the identical mechanism already fixed three other ways this session
// (b2376d3, 8843ac9, chain.ErrStateRootMismatch).
//
// This does not assert RecordMissedBlock is inert: the event, the internal
// stake ledger, and jailing -- all local-only, informational, and safe
// regardless of asymmetric observation -- must still update exactly as
// before. Only the real chain-state balance effect must never fire for a
// Downtime event.
func TestDowntimeSlashingNeverExecutesRealBalancePenalty(t *testing.T) {
	tracker := NewSlashingTracker(SlashingParams{DowntimePenalty: 50})
	var executed []string
	tracker.SetExecuteSlash(func(validatorID string, penalty uint64) {
		executed = append(executed, validatorID)
	})

	// Threshold is missedBlocks > 10, so the 11th call is the one that
	// crosses it -- exactly what node.Node.NoteMissedSlot would trigger
	// after 11 ticks where this node's own clock decided validator-12's
	// slot had passed.
	for i := 0; i < 11; i++ {
		_ = tracker.RecordMissedBlock("validator-12")
	}

	if len(executed) != 0 {
		t.Fatalf("downtime crossing threshold must never execute a real balance penalty, but executeSlash was called for: %v", executed)
	}
	// The rest of the tracker's bookkeeping -- useful, local-only signals
	// -- must still work exactly as before.
	if got := tracker.SlashCount("validator-12"); got != 1 {
		t.Fatalf("expected 1 recorded downtime event, got %d", got)
	}
	if !tracker.IsJailed("validator-12") {
		t.Fatal("expected validator-12 to still be (locally, informationally) jailed after crossing the downtime threshold")
	}
	history := tracker.HistoryFor("validator-12")
	if len(history) != 1 || history[0].EventType != Downtime {
		t.Fatalf("expected a Downtime event in history, got %+v", history)
	}
}
