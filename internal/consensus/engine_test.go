package consensus

import (
	"errors"
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
	e.SetValidators([]string{"v1", "v2", "v3", "v4"})
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

func TestEngine_OnVote_RejectsUnknownValidator(t *testing.T) {
	e := NewEngine(2)
	e.SetValidators([]string{"v1", "v2"})

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

func TestEngine_SetValidators_UsesUniqueNonEmptyIDs(t *testing.T) {
	e := NewEngine(99)
	e.SetValidators([]string{"v1", "", "v1", "v2"})

	if got := e.RequiredForFinality(); got != 2 {
		t.Fatalf("expected finality threshold for 2 unique validators, got %d", got)
	}
}
