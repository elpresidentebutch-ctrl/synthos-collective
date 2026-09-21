package chain

import (
	"errors"
	"fmt"
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

// -----------------------------------------------------------------------
// Everything below used to live on a standalone *TreasuryGovernance held
// only by each Node, entirely separate from *State: its Proposals map was
// never part of consensus state, never included in Root(), never applied
// through a block, and CreateProposal/Vote/Execute mutated it (and, for
// Execute, real account balances) the instant an RPC handler called them.
//
// On a single node that's merely wrong bookkeeping. Across the multi-node
// deployment this chain actually runs as, it was a live, standing bug: two
// nodes given the identical genesis have no way to learn about a proposal
// or vote created on a third via RPC (there's no block, so nothing to sync
// or catch up on), and a /governance/execute call mutated Accounts --
// which IS hashed into Root() -- on whichever single node received that
// one HTTP request, forking that node's state root away from every peer
// permanently. Exactly the same failure class already found and fixed for
// the immune-node bootstrap data and the bridge quorum bypass elsewhere in
// this audit, just for governance/treasury instead.
//
// The fix: proposal storage moves onto *State (GovernanceProposals,
// GovernanceFounder), and every mutation is applied through ApplyTx (see
// applyCitizenGovernanceTx in core.go), the same replicated path every
// other consensus-affecting change in this codebase already goes through.
// TreasuryGovernance survives only as a thin, read-oriented facade so
// existing callers (internal/rpc, internal/agent's reputation scoring)
// don't need to change -- it now delegates to *State instead of holding
// its own independent copy of anything.
// -----------------------------------------------------------------------

// TreasuryGovernance is a thin, read-mostly facade over a *State's real
// governance data. FounderAddress and TreasuryAddr are recorded here (as
// well as on State, which is the value ApplyTx actually enforces) purely
// so RPC status endpoints can report the deployment's configuration
// without needing a Chain reference of their own.
//
// This holds *Chain, not *State, and every method below dereferences
// chain.State fresh on each call rather than once at construction time.
// FinalizeBlock commits each block by building a new *State (a clone of
// the old one with that block's transactions applied) and then
// reassigning Chain.State to point at it (c.State = nextState) -- it does
// not mutate the old *State object in place. A *State pointer captured
// once, before any block was finalized, would silently keep pointing at
// that original, now-permanently-stale object forever after the first
// block. *Chain itself is never reassigned this way (only its .State
// field is), so holding *Chain here and reading .State through it each
// time is what keeps this facade looking at live state.
type TreasuryGovernance struct {
	FounderAddress Address
	TreasuryAddr   Address
	chain          *Chain
}

// NewTreasuryGovernance wires a read facade for founder/treasury/proposal
// status onto the given chain. The founder and treasury addresses are
// caller-supplied (from genesis/env, see cmd/synthosd/main.go's
// initGovernance) -- callers must also make sure chain.State.GovernanceFounder
// is set to the same founder address (Node.InitGovernance does this), since
// that field, not this struct, is what applyCitizenGovernanceTx actually
// checks when a governance_propose transaction is applied.
func NewTreasuryGovernance(founder Address, treasury Address, chain *Chain) *TreasuryGovernance {
	return &TreasuryGovernance{
		FounderAddress: founder,
		TreasuryAddr:   treasury,
		chain:          chain,
	}
}

// Get returns a concurrency-safe copy of a single proposal by ID.
func (tg *TreasuryGovernance) Get(proposalID string) (Proposal, bool) {
	return tg.chain.State.GetGovernanceProposal(proposalID)
}

// Snapshot returns a concurrency-safe copy of every proposal, for read-only
// listing over RPC.
func (tg *TreasuryGovernance) Snapshot() []Proposal {
	return tg.chain.State.SnapshotGovernanceProposals()
}

// Passed reports whether a proposal currently has enough support to
// execute: a simple majority of votes cast, and (when totalStake > 0) at
// least GovernanceQuorumBasisPoints of total stake voting FOR it.
func (tg *TreasuryGovernance) Passed(proposalID string, totalStake uint64) (bool, error) {
	return tg.chain.State.GovernanceProposalPassed(proposalID, totalStake)
}

// VotesCastBy returns how many proposals a given address has actually cast
// a real vote on. Used by internal/agent's reputation scoring.
func (tg *TreasuryGovernance) VotesCastBy(voter Address) int {
	return tg.chain.State.GovernanceVotesCastBy(voter)
}

// cloneProposal returns a deep copy of p -- deep enough (Voters included)
// that callers can't mutate live governance state through the result, and
// that State.Clone can safely use it to give a speculative clone its own
// independent *Proposal objects (see the aliasing note on Clone).
func cloneProposal(p *Proposal) Proposal {
	cp := *p
	cp.Voters = make(map[Address]bool, len(p.Voters))
	for k, v := range p.Voters {
		cp.Voters[k] = v
	}
	return cp
}

// GetGovernanceProposal returns a concurrency-safe copy of a single
// proposal by ID.
func (s *State) GetGovernanceProposal(proposalID string) (Proposal, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.GovernanceProposals[proposalID]
	if !ok {
		return Proposal{}, false
	}
	return cloneProposal(p), true
}

// SnapshotGovernanceProposals returns a concurrency-safe copy of every
// proposal.
func (s *State) SnapshotGovernanceProposals() []Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Proposal, 0, len(s.GovernanceProposals))
	for _, p := range s.GovernanceProposals {
		out = append(out, cloneProposal(p))
	}
	return out
}

// GovernanceProposalPassed reports whether a proposal currently has enough
// support to execute.
func (s *State) GovernanceProposalPassed(proposalID string, totalStake uint64) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.GovernanceProposals[proposalID]
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

// GovernanceVotesCastBy returns how many proposals voter has actually cast
// a real vote on.
func (s *State) GovernanceVotesCastBy(voter Address) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, p := range s.GovernanceProposals {
		if p.Voters != nil && p.Voters[voter] {
			count++
		}
	}
	return count
}

// createGovernanceProposal enforces that ONLY the configured founder
// (s.GovernanceFounder) can create voting options. Called only from
// applyCitizenGovernanceTx, i.e. only while applying a governance_propose
// transaction inside ApplyTx -- never directly from an RPC handler.
func (s *State) createGovernanceProposal(caller Address, id, description string, amount uint64, recipient Address) error {
	if id == "" {
		return errors.New("proposal id must not be empty")
	}
	if recipient == "" {
		return errors.New("proposal recipient must not be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.GovernanceFounder == "" {
		return errors.New("governance is not configured for this deployment (no founder_address)")
	}
	if caller != s.GovernanceFounder {
		return errors.New("unauthorized: only the Founder can create treasury proposals")
	}
	if s.GovernanceProposals == nil {
		s.GovernanceProposals = make(map[string]*Proposal)
	}
	if _, exists := s.GovernanceProposals[id]; exists {
		return errors.New("a proposal with this id already exists")
	}
	s.GovernanceProposals[id] = &Proposal{
		ID:          id,
		Description: description,
		Amount:      amount,
		Recipient:   recipient,
		IsActive:    true,
		Voters:      make(map[Address]bool),
	}
	return nil
}

// voteGovernance lets voter cast a stake-weighted vote on an active
// proposal. Each address may vote once per proposal. Stake weight is
// voter's real, current on-chain balance (the same balance every transfer
// uses), read directly from s.Accounts here since the caller doesn't hold
// s.mu yet when this is called from applyCitizenGovernanceTx. Called only
// while applying a governance_vote transaction inside ApplyTx.
func (s *State) voteGovernance(voter Address, proposalID string, inFavor bool) error {
	if proposalID == "" {
		return errors.New("proposal id must not be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	proposal, exists := s.GovernanceProposals[proposalID]
	if !exists || !proposal.IsActive {
		return errors.New("proposal does not exist or is no longer active")
	}
	if proposal.Voters == nil {
		proposal.Voters = make(map[Address]bool)
	}
	if proposal.Voters[voter] {
		return errors.New("this address has already voted on this proposal")
	}
	voterStake := s.Accounts[voter].Balance
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

// executeGovernanceProposal pays out a passed, still-active, not-yet-
// executed proposal from the treasury address (s.TreasuryAddress) to its
// recipient, then permanently closes it. totalStake gates quorum the same
// way GovernanceProposalPassed does. Called only while applying a
// governance_execute transaction inside ApplyTx -- deliberately not gated
// to any particular caller (see rpc/governance.go's handler comment):
// anyone observing that a proposal has passed can submit the transaction
// that triggers payout, the real gate is this function's own checks, not
// who submitted the wrapping transaction.
func (s *State) executeGovernanceProposal(proposalID string, totalStake uint64) error {
	if proposalID == "" {
		return errors.New("proposal id must not be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.GovernanceProposals[proposalID]
	if !ok || !p.IsActive {
		return errors.New("proposal does not exist or is no longer active")
	}
	if p.Executed {
		return errors.New("proposal has already been executed")
	}
	if p.VotesFor <= p.VotesAgainst {
		return errors.New("proposal has not passed: not enough votes for")
	}
	if totalStake > 0 {
		quorum := totalStake * GovernanceQuorumBasisPoints / 10000
		if p.VotesFor < quorum {
			return fmt.Errorf("proposal has not reached quorum: %d/%d stake-weighted votes for", p.VotesFor, quorum)
		}
	}
	if s.TreasuryAddress == "" {
		return errors.New("treasury address not configured for this deployment")
	}
	amount := p.Amount
	recipient := p.Recipient
	treasuryAddr := s.TreasuryAddress

	treasury := s.Accounts[treasuryAddr]
	if treasury.Balance < amount {
		return fmt.Errorf("treasury balance %d is insufficient for proposal amount %d", treasury.Balance, amount)
	}
	recAcc := s.Accounts[recipient]
	newBal, err := safeAdd(recAcc.Balance, amount)
	if err != nil {
		return errors.New("recipient balance overflow")
	}

	// Mark executed and inactive only once every check above has passed and
	// the payout itself can't fail -- everything from here on is
	// infallible, so a proposal can never end up executed with no payout
	// having actually happened, or vice versa.
	p.Executed = true
	p.IsActive = false
	treasury.Balance -= amount
	s.Accounts[treasuryAddr] = treasury
	recAcc.Balance = newBal
	s.Accounts[recipient] = recAcc
	return nil
}
