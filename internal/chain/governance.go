package chain

import (
	"errors"
	"sync"
)

// -----------------------------
// Community Treasury Governance
// -----------------------------

type Proposal struct {
	ID           string
	Description  string
	Amount       uint64
	Recipient    Address
	VotesFor     uint64
	VotesAgainst uint64
	IsActive     bool
}

type TreasuryGovernance struct {
	mu             sync.Mutex
	Proposals      map[string]*Proposal
	FounderAddress Address
	TreasuryAddr   Address
	GetStake       func(Address) uint64
}

// NewTreasuryGovernance constructs treasury governance. The treasury address
// is caller-supplied — this used to hardcode an undocumented address here;
// callers must now pass whatever treasury address is actually documented for
// this deployment (e.g. from config/genesis.json), and an empty one is left
// empty rather than silently defaulting to a hidden address.
func NewTreasuryGovernance(founder Address, treasury Address, getStake func(Address) uint64) *TreasuryGovernance {
	return &TreasuryGovernance{
		Proposals:      make(map[string]*Proposal),
		FounderAddress: founder,
		TreasuryAddr:   treasury,
		GetStake:       getStake,
	}
}

// CreateProposal enforces that ONLY the founder can create voting options.
func (tg *TreasuryGovernance) CreateProposal(caller Address, id, description string, amount uint64, recipient Address) error {
	tg.mu.Lock()
	defer tg.mu.Unlock()

	if caller != tg.FounderAddress {
		return errors.New("unauthorized: only the Founder can create treasury proposals")
	}

	tg.Proposals[id] = &Proposal{
		ID:          id,
		Description: description,
		Amount:      amount,
		Recipient:   recipient,
		IsActive:    true,
	}
	return nil
}

// Vote allows the community to vote on the Founder's proposals using their stake weight.
func (tg *TreasuryGovernance) Vote(voter Address, proposalID string, inFavor bool) error {
	tg.mu.Lock()
	defer tg.mu.Unlock()

	proposal, exists := tg.Proposals[proposalID]
	if !exists || !proposal.IsActive {
		return errors.New("proposal does not exist or is no longer active")
	}

	// Stake-weighted voting
	voterStake := tg.GetStake(voter)
	if voterStake == 0 {
		return errors.New("voter has no stake")
	}

	if inFavor {
		proposal.VotesFor += voterStake
	} else {
		proposal.VotesAgainst += voterStake
	}

	return nil
}

