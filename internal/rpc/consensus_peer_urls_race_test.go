package rpc

import (
	"sync"
	"testing"
)

// TestConsensusPeerURLs_ConcurrentSetAndSnapshotDoNotRace guards against a
// real regression class: before the validator-roster refresh loop existed
// (see cmd/synthosd/main.go's startValidatorRosterSync and
// docs/VALIDATOR_ONBOARDING.md's Phase 2), SetConsensusPeerURLs was only
// ever called once, synchronously, before the block-producer's
// long-running goroutine started reading ConsensusPeerURLs in
// collectConsensusVotes -- so no concurrent access was possible. The
// refresh loop calls SetConsensusPeerURLs periodically from a separate
// goroutine specifically to add newly-approved validators without a
// redeploy, which turns that former non-issue into a genuine data race
// unless both the writer (SetConsensusPeerURLs) and the reader
// (collectConsensusVotes, via ConsensusPeerURLsSnapshot) go through
// consensusPeerURLsMu. Run with -race; this test only means something
// under that flag.
func TestConsensusPeerURLs_ConcurrentSetAndSnapshotDoNotRace(t *testing.T) {
	s := &Server{}
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			s.SetConsensusPeerURLs([]string{"https://a.example.com", "https://b.example.com"})
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = s.ConsensusPeerURLsSnapshot()
		}
	}()

	wg.Wait()
}

// TestConsensusPeerURLsSnapshot_ReturnsAnIndependentCopy proves the
// snapshot isn't just a reference to the live slice -- collectConsensusVotes
// must be able to iterate its snapshot even if SetConsensusPeerURLs is
// called again (from the refresh loop) while that round is still in
// flight, without the round's peer list silently changing underneath it.
func TestConsensusPeerURLsSnapshot_ReturnsAnIndependentCopy(t *testing.T) {
	s := &Server{}
	s.SetConsensusPeerURLs([]string{"https://a.example.com"})

	snap := s.ConsensusPeerURLsSnapshot()
	if len(snap) != 1 || snap[0] != "https://a.example.com" {
		t.Fatalf("unexpected snapshot: %v", snap)
	}

	// A later update (simulating the refresh loop adding a newly-approved
	// validator) must not retroactively change the already-taken snapshot.
	s.SetConsensusPeerURLs([]string{"https://a.example.com", "https://c.example.com"})
	if len(snap) != 1 || snap[0] != "https://a.example.com" {
		t.Fatalf("earlier snapshot was mutated by a later SetConsensusPeerURLs call: %v", snap)
	}

	fresh := s.ConsensusPeerURLsSnapshot()
	if len(fresh) != 2 {
		t.Fatalf("expected the new snapshot to reflect the update, got %v", fresh)
	}
}
