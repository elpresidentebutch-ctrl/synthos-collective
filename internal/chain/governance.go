package chain

import (
	"crypto/rand"
	"errors"
	"math/big"
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
	mu              sync.Mutex
	Proposals       map[string]*Proposal
	FounderAddress  Address
	TreasuryAddr    Address
	RaffleActive    bool
	RaffleThreshold uint64
	RaffleReward    uint64
	GetStake        func(Address) uint64
}

func NewTreasuryGovernance(founder Address, getStake func(Address) uint64) *TreasuryGovernance {
	return &TreasuryGovernance{
		Proposals:       make(map[string]*Proposal),
		FounderAddress:  founder,
		TreasuryAddr:    Address("0x4823d9af45c0e297d818eb58cb049a0860337aeb"),
		RaffleActive:    false,
		RaffleThreshold: 25_000_000_000, // Trigger at 25 Billion
		RaffleReward:    1_000_000_000,  // 1 Billion payout
		GetStake:        getStake,
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

// CheckRaffle monitors the Treasury balance and triggers a community raffle if it exceeds the threshold.
func (tg *TreasuryGovernance) CheckRaffle(treasuryBalance uint64, communityMembers []Address) *Address {
	tg.mu.Lock()
	defer tg.mu.Unlock()

	if treasuryBalance >= tg.RaffleThreshold {
		tg.RaffleActive = true

		if len(communityMembers) > 0 {
			// Select random community member for the raffle payout using a
			// cryptographically secure random source. A predictable,
			// time-seeded PRNG (math/rand) would let anyone who can guess or
			// influence the seed predict or bias who wins the payout.
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(communityMembers))))
			if err != nil {
				// Fail safe: don't pay out if we can't securely pick a winner.
				return nil
			}
			winner := communityMembers[n.Int64()]
			tg.RaffleActive = false // Reset after payout
			return &winner
		}
	}
	return nil
}
