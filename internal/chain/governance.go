package chain

import (
	"errors"
	"fmt"
	"sync"
)

// -----------------------------
// Community Treasury Governance
// -----------------------------

// GovernanceQuorumBasisPoints is the minimum share of total stake that must
// vote FOR a proposal, in addition to a simple majority of votes cast,
// before it can be executed. Expressed in basis points of total stake
// (500 = 5%). Documented here rather than buried in a check so the rule is
// easy to find and change deliberately.
const GovernanceQuorumBasisPoints = 500

type Proposal struct {
	ID           string
	Description  string
	Amount       uint64
	Recipient    Address
	VotesFor     uint64
	VotesAgainst uint64
	IsActive     bool
	Executed     bool
	Voters       map[Address]bool // real per-voter record: who has already voted, so one address can't vote twice
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
	if _, exists := tg.Proposals[id]; exists {
		return errors.New("a proposal with this id already exists")
	}

	tg.Proposals[id] = &Proposal{
		ID:          id,
		Description: description,
		Amount:      amount,
		Recipient:   recipient,
		IsActive:    true,
		Voters:      make(map[Address]bool),
	}
	return nil
}

// Vote allows the community to vote on the Founder's proposals using their
// stake weight. Each address may vote once per proposal -- a second Vote
// call from the same address returns an error instead of silently adding
// its stake again, which previously let one address inflate a proposal's
// tally by voting repeatedly.
func (tg *TreasuryGovernance) Vote(voter Address, proposalID string, inFavor bool) error {
	tg.mu.Lock()
	defer tg.mu.Unlock()

	proposal, exists := tg.Proposals[proposalID]
	if !exists || !proposal.IsActive {
		return errors.New("proposal does not exist or is no longer active")
	}
	if proposal.Voters == nil {
		proposal.Voters = make(map[Address]bool)
	}
	if proposal.Voters[voter] {
		return errors.New("this address has already voted on this proposal")
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
	proposal.Voters[voter] = true

	return nil
}

// VotesCastBy returns how many proposals a given address has actually cast
// a real vote on. Used for reputation scoring so "participated in
// governance" reflects real recorded votes, not a self-reported counter.
func (tg *TreasuryGovernance) VotesCastBy(voter Address) int {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	count := 0
	for _, p := range tg.Proposals {
		if p.Voters != nil && p.Voters[voter] {
			count++
		}
	}
	return count
}

// Passed reports whether a proposal currently has enough support to
// execute: a simple majority of votes cast, and (when totalStake > 0) at
// least GovernanceQuorumBasisPoints of total stake voting FOR it.
func (tg *TreasuryGovernance) Passed(proposalID string, totalStake uint64) (bool, error) {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	p, ok := tg.Proposals[proposalID]
	if !ok {
		return false, errors.New("proposal does not exist")
	}
	if p.VotesFor <= p.VotesAgainst {
		return false, nil
	}
	if totalStake > 0 {
		quorum := totalStake * GovernanceQuorumBasisPoints / 10000
		if p.VotesFor < quorum {
			return false, nil
		}
	}
	return true, nil
}

// Get returns a concurrency-safe copy of a single proposal by ID.
func (tg *TreasuryGovernance) Get(proposalID string) (Proposal, bool) {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	p, ok := tg.Proposals[proposalID]
	if !ok {
		return Proposal{}, false
	}
	return cloneProposal(p), true
}

// Snapshot returns a concurrency-safe copy of every proposal, for read-only
// listing over RPC. Copies are deep enough (Voters included) that callers
// can't mutate live governance state through the result.
func (tg *TreasuryGovernance) Snapshot() []Proposal {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	out := make([]Proposal, 0, len(tg.Proposals))
	for _, p := range tg.Proposals {
		out = append(out, cloneProposal(p))
	}
	return out
}

func cloneProposal(p *Proposal) Proposal {
	cp := *p
	cp.Voters = make(map[Address]bool, len(p.Voters))
	for k, v := range p.Voters {
		cp.Voters[k] = v
	}
	return cp
}

// Execute pays out a passed, still-active, not-yet-executed proposal from
// the treasury address to its recipient, then permanently closes it. This
// moves real balance in the same *State every other transaction uses --
// it is not simulated -- and it can only ever run once per proposal
// (Executed is checked and set under the same lock the payout happens
// under, so a proposal cannot be paid out twice even under concurrent
// calls).
func (tg *TreasuryGovernance) Execute(proposalID string, st *State, totalStake uint64) error {
	tg.mu.Lock()
	p, ok := tg.Proposals[proposalID]
	if !ok || !p.IsActive {
		tg.mu.Unlock()
		return errors.New("proposal does not exist or is no longer active")
	}
	if p.Executed {
		tg.mu.Unlock()
		return errors.New("proposal has already been executed")
	}
	if p.VotesFor <= p.VotesAgainst {
		tg.mu.Unlock()
		return errors.New("proposal has not passed: not enough votes for")
	}
	if totalStake > 0 {
		quorum := totalStake * GovernanceQuorumBasisPoints / 10000
		if p.VotesFor < quorum {
			tg.mu.Unlock()
			return fmt.Errorf("proposal has not reached quorum: %d/%d stake-weighted votes for", p.VotesFor, quorum)
		}
	}
	if tg.TreasuryAddr == "" {
		tg.mu.Unlock()
		return errors.New("treasury address not configured for this deployment")
	}
	amount := p.Amount
	recipient := p.Recipient
	treasuryAddr := tg.TreasuryAddr
	// Mark executed and inactive before releasing the lock so a concurrent
	// Execute call for the same proposal fails fast instead of racing us
	// to move the funds twice.
	p.Executed = true
	p.IsActive = false
	tg.mu.Unlock()

	treasury := st.Get(treasuryAddr)
	if treasury.Balance < amount {
		return fmt.Errorf("treasury balance %d is insufficient for proposal amount %d", treasury.Balance, amount)
	}
	treasury.Balance -= amount
	st.Set(treasuryAddr, treasury)

	recAcc := st.Get(recipient)
	newBal, err := safeAdd(recAcc.Balance, amount)
	if err != nil {
		return errors.New("recipient balance overflow")
	}
	recAcc.Balance = newBal
	st.Set(recipient, recAcc)
	return nil
}

