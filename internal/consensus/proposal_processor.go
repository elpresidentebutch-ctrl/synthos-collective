package consensus

import (
	"errors"
	"fmt"
	"time"

	"synthos-collective/internal/chain"
)

// ProposalProcessor validates proposed blocks against local consensus invariants
// before a validator votes. Gleaned from CometBFT ABCI++ ProcessProposal specification.
type ProposalProcessor struct {
	MaxFutureDrift time.Duration
}

// NewProposalProcessor constructs a new proposal invariant checker with specified drift tolerance.
func NewProposalProcessor(maxFutureDrift time.Duration) *ProposalProcessor {
	if maxFutureDrift <= 0 {
		maxFutureDrift = 15 * time.Second
	}
	return &ProposalProcessor{MaxFutureDrift: maxFutureDrift}
}

// ProcessProposal validates structural, temporal, and consensus continuity invariants.
// Any invariant failure indicates an illegitimate proposal that must not receive a vote.
func (p *ProposalProcessor) ProcessProposal(b *chain.Block, currentHeight uint64, currentTipHash string, now time.Time) error {
	if b == nil {
		return errors.New("nil proposal block")
	}
	if b.Hash == "" {
		return errors.New("empty proposal block hash")
	}

	expectedHeight := currentHeight + 1
	if b.Header.Height != expectedHeight {
		return fmt.Errorf("proposal height mismatch: got %d, expected %d", b.Header.Height, expectedHeight)
	}

	if currentHeight > 0 && currentTipHash != "" {
		if b.Header.ParentHash != currentTipHash {
			return fmt.Errorf("parent hash continuity broken: proposal parent %s does not match tip %s",
				b.Header.ParentHash, currentTipHash)
		}
	}

	if b.Header.ProposerID == "" {
		return errors.New("proposal missing proposer ID")
	}

	// Monotonic BFT timestamp invariant: block time must not be in the distant future.
	if !b.Header.Timestamp.IsZero() {
		if b.Header.Timestamp.After(now.Add(p.MaxFutureDrift)) {
			return fmt.Errorf("proposal timestamp %v exceeds maximum allowable future drift (%v)",
				b.Header.Timestamp, p.MaxFutureDrift)
		}
	}

	return nil
}
