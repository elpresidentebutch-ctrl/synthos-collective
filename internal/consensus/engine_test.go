package consensus

import (
	"fmt"
	"testing"
)

func TestEngine_RequiredForFinality(t *testing.T) {
	tests := []struct {
		n        int
		required int
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 2},
		{4, 3},
		{10, 7},
	}
	for _, tc := range tests {
		e := NewEngine(tc.n)
		if got := e.RequiredForFinality(); got != tc.required {
			t.Fatalf("N=%d: want required %d, got %d", tc.n, tc.required, got)
		}
	}
}

func TestEngine_OnVote_Finality(t *testing.T) {
	e := NewEngine(4) // required = 3
	hash := "0xabc"
	h := uint64(1)

	var finalized bool
	var err error
	for i := 1; i <= 3; i++ {
		finalized, _, _, err = e.OnVote(BlockVote{
			BlockHash: hash,
			Height:    h,
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

	// duplicate voter should not double-count
	finalized, votesFor, _, err := e.OnVote(BlockVote{
		BlockHash: hash,
		Height:    h,
		VoterID:   "v2",
		Vote:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if votesFor != 3 {
		t.Fatalf("duplicate vote should not increase count: got %d", votesFor)
	}
	if !finalized {
		t.Fatal("should remain finalized")
	}
}
