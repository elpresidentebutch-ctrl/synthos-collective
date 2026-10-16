package consensus

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

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

func TestEngine_CometBFT_TwoPhasePrevoteAndPrecommit(t *testing.T) {
	e := NewEngine(4)
	e.SetValidators([]string{"v1", "v2", "v3", "v4"})
	block := &chain.Block{Header: chain.BlockHeader{Height: 10}, Hash: "0xblock10"}
	e.OnProposal(block)

	// Step 1: Prevote phase
	// 2 votes out of 4 (threshold is 3) -> should not meet POL
	for _, vid := range []string{"v1", "v2"} {
		thresholdMet, pol, votesFor, required, err := e.OnPrevote(BlockVote{
			BlockHash: block.Hash,
			Height:    10,
			Round:     0,
			VoterID:   vid,
			Vote:      1,
			Signature: "0xsig_" + vid,
		})
		if err != nil {
			t.Fatalf("unexpected error on prevote: %v", err)
		}
		if thresholdMet || pol != nil {
			t.Fatalf("POL should not be met with %d/%d votes", votesFor, required)
		}
	}

	// 3rd prevote -> reaches 2/3+ quorum (3 of 4) -> achieves Proof-of-Lock (POL)
	thresholdMet, pol, votesFor, _, err := e.OnPrevote(BlockVote{
		BlockHash: block.Hash,
		Height:    10,
		Round:     0,
		VoterID:   "v3",
		Vote:      1,
		Signature: "0xsig_v3",
	})
	if err != nil {
		t.Fatalf("unexpected error on 3rd prevote: %v", err)
	}
	if !thresholdMet || pol == nil {
		t.Fatalf("expected POL to be achieved at 3 of 4 votes, got thresholdMet=%v pol=%v", thresholdMet, pol)
	}
	if pol.BlockHash != block.Hash || pol.Round != 0 || pol.Height != 10 {
		t.Fatalf("unexpected POL content: %+v", pol)
	}
	if !e.HasProofOfLock(10, 0, block.Hash) {
		t.Fatal("expected HasProofOfLock to be true")
	}
	gotPOL, ok := e.ProofOfLock(10)
	if !ok || gotPOL.BlockHash != block.Hash {
		t.Fatalf("expected ProofOfLock(10) to return valid POL: %+v", gotPOL)
	}

	// Verify collected prevote approvals
	prevoteSigs := e.CollectedPrevoteApprovals(10, 0, block.Hash)
	if len(prevoteSigs) != 3 {
		t.Fatalf("expected 3 prevote signatures, got %d", len(prevoteSigs))
	}

	// Step 2: Precommit phase
	// 2 precommits -> not finalized yet
	for _, vid := range []string{"v1", "v2"} {
		finalized, votesFor, required, err := e.OnPrecommit(BlockVote{
			BlockHash: block.Hash,
			Height:    10,
			Round:     0,
			VoterID:   vid,
			Vote:      1,
			Signature: "0xprecommit_sig_" + vid,
		})
		if err != nil {
			t.Fatalf("unexpected error on precommit: %v", err)
		}
		if finalized {
			t.Fatalf("block should not finalize with %d/%d votes", votesFor, required)
		}
	}

	// 3rd precommit -> reaches 2/3+ finality!
	finalized, votesFor, _, err := e.OnPrecommit(BlockVote{
		BlockHash: block.Hash,
		Height:    10,
		Round:     0,
		VoterID:   "v3",
		Vote:      1,
		Signature: "0xprecommit_sig_v3",
	})
	if err != nil {
		t.Fatalf("unexpected error on 3rd precommit: %v", err)
	}
	if !finalized || votesFor != 3 {
		t.Fatalf("expected block to finalize at 3 votes: finalized=%v votesFor=%d", finalized, votesFor)
	}
}

func TestEngine_CometBFT_RoundIsolation(t *testing.T) {
	e := NewEngine(4)
	e.SetValidators([]string{"v1", "v2", "v3", "v4"})

	b0 := &chain.Block{Header: chain.BlockHeader{Height: 20}, Hash: "0xblock_round0"}
	b1 := &chain.Block{Header: chain.BlockHeader{Height: 20}, Hash: "0xblock_round1"}
	e.RecordOwnProposalWithRound(b0, 0)
	e.RecordOwnProposalWithRound(b1, 1)

	// Round 0: v1 and v2 prevote for b0 (2 votes, below 3)
	for _, vid := range []string{"v1", "v2"} {
		met, pol, _, _, err := e.OnPrevote(BlockVote{
			BlockHash: b0.Hash,
			Height:    20,
			Round:     0,
			VoterID:   vid,
			Vote:      1,
		})
		if err != nil || met || pol != nil {
			t.Fatalf("round 0 prevote failed: met=%v pol=%v err=%v", met, pol, err)
		}
	}

	// Round 1: v3 and v4 prevote for b1 (2 votes, below 3)
	// Round 0 votes must NOT leak into Round 1
	for _, vid := range []string{"v3", "v4"} {
		met, pol, votesFor, _, err := e.OnPrevote(BlockVote{
			BlockHash: b1.Hash,
			Height:    20,
			Round:     1,
			VoterID:   vid,
			Vote:      1,
		})
		if err != nil {
			t.Fatalf("round 1 prevote error: %v", err)
		}
		if met || pol != nil || votesFor > 2 {
			t.Fatalf("round isolation breach: round 0 votes leaked into round 1! votesFor=%d met=%v", votesFor, met)
		}
	}

	// Now v1 votes in Round 1 -> reaches 3 votes for Round 1
	met, pol, votesFor, _, err := e.OnPrevote(BlockVote{
		BlockHash: b1.Hash,
		Height:    20,
		Round:     1,
		VoterID:   "v1",
		Vote:      1,
	})
	if err != nil {
		t.Fatalf("round 1 3rd prevote error: %v", err)
	}
	if !met || pol == nil || votesFor != 3 {
		t.Fatalf("expected round 1 to achieve POL with 3 votes: met=%v pol=%v votesFor=%d", met, pol, votesFor)
	}
	if pol.Round != 1 || pol.BlockHash != b1.Hash {
		t.Fatalf("expected POL for round 1 b1, got %+v", pol)
	}
}

func TestEngine_CometBFT_StakeWeightedPowerQuorum(t *testing.T) {
	e := NewEngine(3)
	e.SetValidatorPowers(map[string]uint64{
		"whale": 500,
		"mid":   300,
		"small": 200,
	})

	if e.ValidatorPower("whale") != 500 || e.ValidatorPower("mid") != 300 || e.ValidatorPower("small") != 200 {
		t.Fatalf("unexpected validator powers")
	}

	// Total power = 1000. Required power = (2*1000 + 2)/3 = 667. Required count = 2.
	if got := e.RequiredPowerForFinality(); got != 667 {
		t.Fatalf("expected required power 667, got %d", got)
	}

	block := &chain.Block{Header: chain.BlockHeader{Height: 5}, Hash: "0xweighted_block"}
	e.OnProposal(block)

	// Mid and Small prevote: 2 votes, but total power = 300 + 200 = 500 < 667.
	// Threshold should NOT be met because voting power quorum is unsatisfied!
	for _, vid := range []string{"mid", "small"} {
		met, pol, _, _, err := e.OnPrevote(BlockVote{
			BlockHash: block.Hash,
			Height:    5,
			Round:     0,
			VoterID:   vid,
			Vote:      1,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if met || pol != nil {
			t.Fatalf("power threshold violated: 500/1000 power should not achieve POL")
		}
	}

	// Whale prevotes in a fresh engine:
	e2 := NewEngine(3)
	e2.SetValidatorPowers(map[string]uint64{
		"whale": 500,
		"mid":   300,
		"small": 200,
	})
	e2.OnProposal(block)

	// Whale alone: power = 500 < 667, count = 1 < 2 -> no POL
	met, pol, _, _, err := e2.OnPrevote(BlockVote{
		BlockHash: block.Hash,
		Height:    5,
		Round:     0,
		VoterID:   "whale",
		Vote:      1,
	})
	if err != nil || met || pol != nil {
		t.Fatalf("whale alone should not achieve POL")
	}

	// Whale + Small: power = 500 + 200 = 700 >= 667, count = 2 >= 2 -> POL achieved!
	met, pol, _, _, err = e2.OnPrevote(BlockVote{
		BlockHash: block.Hash,
		Height:    5,
		Round:     0,
		VoterID:   "small",
		Vote:      1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !met || pol == nil {
		t.Fatalf("whale + small (700 power) must achieve POL")
	}

	// Precommit phase: Whale + Small precommit -> finalized!
	_, _, _, _ = e2.OnPrecommit(BlockVote{
		BlockHash: block.Hash,
		Height:    5,
		Round:     0,
		VoterID:   "whale",
		Vote:      1,
	})
	finalized, _, _, err := e2.OnPrecommit(BlockVote{
		BlockHash: block.Hash,
		Height:    5,
		Round:     0,
		VoterID:   "small",
		Vote:      1,
	})
	if err != nil {
		t.Fatalf("precommit error: %v", err)
	}
	if !finalized {
		t.Fatalf("expected finality with 700/1000 stake-weighted voting power")
	}
}

func TestEngine_CometBFT_CryptographicEquivocationEvidence(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}

	validatorID := "val-equivocator"
	height := uint64(50)
	round := 0

	msgA := []byte("APPROVE|0xblockAlpha")
	sigA := "0x" + hex.EncodeToString(ed25519.Sign(privKey, msgA))

	msgB := []byte("APPROVE|0xblockBeta")
	sigB := "0x" + hex.EncodeToString(ed25519.Sign(privKey, msgB))

	evidence := EquivocationEvidence{
		ValidatorID: validatorID,
		Height:      height,
		Round:       round,
		VoteA: BlockVote{
			BlockHash: "0xblockAlpha",
			Height:    height,
			Round:     round,
			VoterID:   validatorID,
			Vote:      1,
			Signature: sigA,
		},
		VoteB: BlockVote{
			BlockHash: "0xblockBeta",
			Height:    height,
			Round:     round,
			VoterID:   validatorID,
			Vote:      1,
			Signature: sigB,
		},
		DiscoveredAt: time.Now(),
	}

	// Valid cryptographic evidence must verify
	if err := evidence.Verify(pubKey); err != nil {
		t.Fatalf("expected valid equivocation evidence to verify, got: %v", err)
	}

	// Forged / tampered signature must fail verification
	badEvidence := evidence
	badEvidence.VoteB.Signature = "0x" + hex.EncodeToString([]byte("invalid_signature_bytes_that_fail_verification_32b_long!"))
	if err := badEvidence.Verify(pubKey); err == nil {
		t.Fatalf("expected tampered signature to fail verification")
	}

	// Slashing execution via cryptographic evidence
	tracker := NewSlashingTracker(SlashingParams{DoubleSignPenalty: 5000})
	var slashedValidator string
	var slashedPenalty uint64
	tracker.SetExecuteSlash(func(vID string, penalty uint64) {
		slashedValidator = vID
		slashedPenalty = penalty
	})

	err = tracker.RecordCryptographicEvidence(evidence, pubKey)
	if err != nil {
		t.Fatalf("failed to record cryptographic evidence: %v", err)
	}
	if slashedValidator != validatorID || slashedPenalty != 5000 {
		t.Fatalf("expected slashed validator %s with penalty 5000, got %s / %d", validatorID, slashedValidator, slashedPenalty)
	}
	if tracker.TotalSlashEvents() != 1 {
		t.Fatalf("expected 1 slash event, got %d", tracker.TotalSlashEvents())
	}
}

func TestProposalProcessor_ABCIInvariants(t *testing.T) {
	processor := NewProposalProcessor(15 * time.Second)
	now := time.Now()

	validBlock := &chain.Block{
		Header: chain.BlockHeader{
			Height:     101,
			ParentHash: "0xtip100",
			ProposerID: "proposer1",
			Timestamp:  now,
		},
		Hash: "0xblock101",
	}

	// 1. Valid proposal succeeds
	if err := processor.ProcessProposal(validBlock, 100, "0xtip100", now); err != nil {
		t.Fatalf("valid proposal failed: %v", err)
	}

	// 2. Nil block fails
	if err := processor.ProcessProposal(nil, 100, "0xtip100", now); err == nil {
		t.Fatal("expected error on nil block")
	}

	// 3. Height progression failure (skipping height)
	badHeightBlock := *validBlock
	badHeightBlock.Header.Height = 102
	if err := processor.ProcessProposal(&badHeightBlock, 100, "0xtip100", now); err == nil {
		t.Fatal("expected error on invalid height progression")
	}

	// 4. Parent hash tip continuity broken
	badParentBlock := *validBlock
	badParentBlock.Header.ParentHash = "0xwrongparent"
	if err := processor.ProcessProposal(&badParentBlock, 100, "0xtip100", now); err == nil {
		t.Fatal("expected error on broken parent hash continuity")
	}

	// 5. Missing proposer ID
	missingProposerBlock := *validBlock
	missingProposerBlock.Header.ProposerID = ""
	if err := processor.ProcessProposal(&missingProposerBlock, 100, "0xtip100", now); err == nil {
		t.Fatal("expected error on missing proposer ID")
	}

	// 6. Future timestamp drift beyond allowable window (30s > 15s)
	futureDriftBlock := *validBlock
	futureDriftBlock.Header.Timestamp = now.Add(30 * time.Second)
	if err := processor.ProcessProposal(&futureDriftBlock, 100, "0xtip100", now); err == nil {
		t.Fatal("expected error on proposal timestamp drifting into future")
	}
}

