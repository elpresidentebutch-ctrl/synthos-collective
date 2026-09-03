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
