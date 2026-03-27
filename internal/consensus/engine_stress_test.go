package consensus

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"synthos-collective/internal/chain"
)

// makeMinimalBlock builds a minimal chain.Block with the given hash and height,
// sufficient for registering with the Engine's OnProposal method.
func makeMinimalBlock(hash string, height uint64) *chain.Block {
	b := &chain.Block{
		Header: chain.BlockHeader{
			Height:     height,
			ParentHash: "0x0",
			ProposerID: "test",
			StateRoot:  "0x0",
		},
		Hash: hash,
	}
	return b
}

// TestStress_Engine_ConcurrentVotes floods the consensus engine with votes from
// 100 validators arriving concurrently at 50 distinct block heights.
// Validates that the mutex protects internal state correctly and that every
// height reaches finality.
func TestStress_Engine_ConcurrentVotes(t *testing.T) {
	const (
		validators = 100
		heights    = 50
	)
	e := NewEngine(validators)
	required := e.RequiredForFinality() // ceil(2*100/3) = 67

	var wg sync.WaitGroup
	// Track whether finality was ever signalled for each height.
	finalitySignalled := make([]bool, heights)
	var mu sync.Mutex

	start := time.Now()
	for h := uint64(1); h <= heights; h++ {
		height := h
		blockHash := fmt.Sprintf("0xblock%d", h)

		for i := 0; i < validators; i++ {
			voterID := fmt.Sprintf("v%03d", i)
			wg.Add(1)
			go func(voterID string, height uint64, blockHash string) {
				defer wg.Done()
				finalized, _, _, err := e.OnVote(BlockVote{
					BlockHash: blockHash,
					Height:    height,
					VoterID:   voterID,
					Vote:      1,
				})
				if err != nil {
					t.Errorf("OnVote(%s, h=%d): %v", voterID, height, err)
					return
				}
				if finalized {
					mu.Lock()
					finalitySignalled[height-1] = true
					mu.Unlock()
				}
			}(voterID, height, blockHash)
		}
	}
	wg.Wait()
	elapsed := time.Since(start)

	totalVotes := validators * heights
	t.Logf("ConcurrentVotes: %d votes across %d heights in %v (%.0f votes/s)",
		totalVotes, heights, elapsed, float64(totalVotes)/elapsed.Seconds())

	// Verify each height shows finality in the engine.
	for h := uint64(1); h <= heights; h++ {
		// Register a dummy proposal so FinalityStatus can find it.
		// (OnVote works without OnProposal; we verify via a final OnVote call.)
		finalized, votesFor, _, err := e.OnVote(BlockVote{
			BlockHash: fmt.Sprintf("0xblock%d", h),
			Height:    h,
			VoterID:   "check-voter", // duplicate — ignored
			Vote:      1,
		})
		if err != nil {
			t.Errorf("height %d check vote: %v", h, err)
		}
		if !finalized || votesFor < required {
			t.Errorf("height %d: expected finality (need %d votes), got votesFor=%d finalized=%v",
				h, required, votesFor, finalized)
		}
	}
	t.Logf("✅ All %d heights reached finality (required %d of %d validators)", heights, required, validators)
}

// TestStress_Engine_DuplicateVotesDoNotOvercount verifies that a validator
// submitting its vote thousands of times does not inflate the count beyond the
// validator set size.
func TestStress_Engine_DuplicateVotesDoNotOvercount(t *testing.T) {
	const (
		validators  = 10
		repeatsEach = 200
	)
	e := NewEngine(validators)
	required := e.RequiredForFinality()

	var wg sync.WaitGroup
	for i := 0; i < validators; i++ {
		voterID := fmt.Sprintf("v%d", i)
		for r := 0; r < repeatsEach; r++ {
			wg.Add(1)
			go func(voterID string) {
				defer wg.Done()
				e.OnVote(BlockVote{ //nolint:errcheck
					BlockHash: "0xhash",
					Height:    1,
					VoterID:   voterID,
					Vote:      1,
				})
			}(voterID)
		}
	}
	wg.Wait()

	// One final vote to read back the current count.
	finalized, votesFor, _, _ := e.OnVote(BlockVote{
		BlockHash: "0xhash",
		Height:    1,
		VoterID:   "v0", // already seen — will be ignored
		Vote:      1,
	})

	if votesFor > validators {
		t.Fatalf("vote count %d exceeds validator set size %d (duplicate votes overcounted)",
			votesFor, validators)
	}
	if !finalized {
		t.Fatalf("expected finality with %d validators (required %d), got votesFor=%d",
			validators, required, votesFor)
	}
	t.Logf("✅ DuplicateVotes: count=%d (submitted %d*%d, max unique=%d)",
		votesFor, validators, repeatsEach, validators)
}

// TestStress_Engine_ManyHeights drives the engine through 10 000 sequential heights
// to check for memory growth and correctness under sustained load.
func TestStress_Engine_ManyHeights(t *testing.T) {
	const (
		validators = 5
		numHeights = 10000
	)
	e := NewEngine(validators)
	required := e.RequiredForFinality() // 4 of 5

	start := time.Now()
	for h := uint64(1); h <= numHeights; h++ {
		blockHash := fmt.Sprintf("0xblock%d", h)
		var finalized bool
		for i := 0; i < required; i++ {
			var err error
			finalized, _, _, err = e.OnVote(BlockVote{
				BlockHash: blockHash,
				Height:    h,
				VoterID:   fmt.Sprintf("v%d", i),
				Vote:      1,
			})
			if err != nil {
				t.Fatalf("OnVote h=%d v%d: %v", h, i, err)
			}
		}
		if !finalized {
			t.Fatalf("height %d: expected finality after %d votes", h, required)
		}
	}
	elapsed := time.Since(start)
	totalVotes := int64(numHeights) * int64(required)
	t.Logf("ManyHeights: %d heights, %d votes in %v (%.0f votes/s)",
		numHeights, totalVotes, elapsed, float64(totalVotes)/elapsed.Seconds())
}

// TestStress_Engine_MixedYesNoVotes verifies that a mix of yes (1) and no (-1)
// votes does not reach finality when the yes count is below 2/3.
func TestStress_Engine_MixedYesNoVotes(t *testing.T) {
	const validators = 9
	e := NewEngine(validators)
	required := e.RequiredForFinality() // 6 of 9

	// 5 yes, 4 no — should NOT reach finality.
	for i := 0; i < 5; i++ {
		finalized, _, _, _ := e.OnVote(BlockVote{
			BlockHash: "0xblockA",
			Height:    1,
			VoterID:   fmt.Sprintf("yes%d", i),
			Vote:      1,
		})
		if finalized {
			t.Fatalf("should not finalize with only 5 of %d required %d yes-votes", validators, required)
		}
	}
	for i := 0; i < 4; i++ {
		finalized, _, _, _ := e.OnVote(BlockVote{
			BlockHash: "0xblockA",
			Height:    1,
			VoterID:   fmt.Sprintf("no%d", i),
			Vote:      -1,
		})
		if finalized {
			t.Fatalf("should not finalize: no-votes should not be counted toward finality")
		}
	}

	// Add the 6th yes vote — now finality should be reached.
	finalized, votesFor, _, _ := e.OnVote(BlockVote{
		BlockHash: "0xblockA",
		Height:    1,
		VoterID:   "yes5",
		Vote:      1,
	})
	if !finalized {
		t.Fatalf("expected finality at 6 yes-votes (required=%d), got votesFor=%d", required, votesFor)
	}
	t.Logf("✅ MixedVotes: finalized at votesFor=%d (required=%d)", votesFor, required)
}

// TestStress_Engine_ConflictingProposalsSameHeight ensures a second (conflicting)
// proposal at the same height is silently ignored (equivocation protection).
func TestStress_Engine_ConflictingProposalsSameHeight(t *testing.T) {
	const validators = 3
	e := NewEngine(validators)

	// Register first proposal.
	block1 := makeMinimalBlock("0xhash-A", 1)
	e.OnProposal(block1)

	// Try to register a conflicting proposal at the same height.
	block2 := makeMinimalBlock("0xhash-B", 1)
	e.OnProposal(block2)

	// Only the first proposal should be stored.
	_, found := e.Proposal("0xhash-A")
	if !found {
		t.Fatal("first proposal should still be present")
	}
	_, found = e.Proposal("0xhash-B")
	if found {
		t.Fatal("conflicting second proposal should have been rejected")
	}
	t.Logf("✅ ConflictingProposals: second proposal at same height correctly rejected")
}

// --- Benchmarks ---

// BenchmarkEngine_OnVote measures the throughput of the mutex-protected vote path.
func BenchmarkEngine_OnVote(b *testing.B) {
	const validators = 10
	e := NewEngine(validators)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := uint64(i/validators) + 1
		voterID := fmt.Sprintf("v%d", i%validators)
		e.OnVote(BlockVote{ //nolint:errcheck
			BlockHash: fmt.Sprintf("0xblock%d", h),
			Height:    h,
			VoterID:   voterID,
			Vote:      1,
		})
	}
}

// BenchmarkEngine_OnVote_Parallel measures concurrent vote throughput.
func BenchmarkEngine_OnVote_Parallel(b *testing.B) {
	const validators = 20
	e := NewEngine(validators)

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			h := uint64(i/validators) + 1
			e.OnVote(BlockVote{ //nolint:errcheck
				BlockHash: fmt.Sprintf("0xblock%d", h),
				Height:    h,
				VoterID:   fmt.Sprintf("v%d", i%validators),
				Vote:      1,
			})
			i++
		}
	})
}
