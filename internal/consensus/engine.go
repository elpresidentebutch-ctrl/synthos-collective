package consensus

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/network"
)

// Engine is a minimal 2/3+ vote finality engine.
// It assumes an external proposer selection mechanism (round-robin, stake-weighted, etc.).
type Engine struct {
	mu sync.Mutex

	totalValidators int

	// proposalsByHeight[height] = proposal block for that height.
	// v0.1 only allows a single canonical proposal per height.
	proposalsByHeight map[uint64]*chain.Block

	// votes[height][voterID] = (blockHash, vote)
	// This ensures each validator can vote at most once per height.
	votes map[uint64]map[string]voteRecord
}

type voteRecord struct {
	BlockHash string
	Vote      int
}

var (
	ErrUnknownProposal = errors.New("unknown proposal")
)

func NewEngine(totalValidators int) *Engine {
	return &Engine{
		totalValidators:   totalValidators,
		proposalsByHeight: make(map[uint64]*chain.Block),
		votes:             make(map[uint64]map[string]voteRecord),
	}
}

func (e *Engine) RequiredForFinality() int {
	if e.totalValidators <= 0 {
		return 1
	}
	// ceil(2N/3) using integer math
	req := (2*e.totalValidators + 2) / 3
	if req < 1 {
		req = 1
	}
	return req
}

// OnProposal records a proposal for its height. Only one proposal per height is
// currently supported; conflicting proposals are treated as byzantine behavior
// by the proposer and ignored.
func (e *Engine) OnProposal(b *chain.Block) {
	e.mu.Lock()
	defer e.mu.Unlock()
	h := b.Header.Height
	if existing, ok := e.proposalsByHeight[h]; ok {
		if existing.Hash != b.Hash {
			// Conflicting proposal at same height; ignore the new one.
			return
		}
		// Same proposal hash repeated; ignore.
		return
	}
	e.proposalsByHeight[h] = b
	if _, ok := e.votes[h]; !ok {
		e.votes[h] = make(map[string]voteRecord)
	}
}

// Proposal returns the proposal block at a given height, if any.
func (e *Engine) Proposal(blockHash string) (*chain.Block, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for h, b := range e.proposalsByHeight {
		if b != nil && b.Hash == blockHash {
			_ = h
			return b, true
		}
	}
	return nil, false
}

// FinalityStatus returns whether a proposal has reached 2/3+ votes-for.
// It searches for the proposal by hash and inspects votes for its height.
func (e *Engine) FinalityStatus(blockHash string) (finalized bool, votesFor int, required int, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var height uint64
	found := false
	for h, b := range e.proposalsByHeight {
		if b != nil && b.Hash == blockHash {
			height = h
			found = true
			break
		}
	}
	if !found {
		return false, 0, e.RequiredForFinality(), false
	}
	required = e.RequiredForFinality()
	for _, rec := range e.votes[height] {
		if rec.BlockHash == blockHash && rec.Vote == 1 {
			votesFor++
		}
	}
	finalized = votesFor >= required
	return finalized, votesFor, required, true
}

func (e *Engine) OnVote(v BlockVote) (finalized bool, votesFor int, required int, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	h := v.Height
	if _, ok := e.votes[h]; !ok {
		e.votes[h] = make(map[string]voteRecord)
	}
	if _, exists := e.votes[h][v.VoterID]; exists {
		// ignore duplicates per height
	} else {
		e.votes[h][v.VoterID] = voteRecord{BlockHash: v.BlockHash, Vote: v.Vote}
	}

	required = e.RequiredForFinality()
	for _, rec := range e.votes[h] {
		if rec.BlockHash == v.BlockHash && rec.Vote == 1 {
			votesFor++
		}
	}
	finalized = votesFor >= required
	return finalized, votesFor, required, nil
}

// Envelope handlers helpers

const (
	TopicProposals = "consensus/proposals"
	TopicVotes     = "consensus/votes"
)

func EncodeProposal(b *chain.Block) ([]byte, error) {
	return json.Marshal(BlockProposal{Block: *b, Height: b.Header.Height})
}

func DecodeProposal(b []byte) (BlockProposal, error) {
	var p BlockProposal
	err := json.Unmarshal(b, &p)
	return p, err
}

func EncodeVote(v BlockVote) ([]byte, error) { return json.Marshal(v) }
func DecodeVote(b []byte) (BlockVote, error) {
	var v BlockVote
	err := json.Unmarshal(b, &v)
	return v, err
}

// Basic time window check for consensus messages.
func FreshEnough(ts time.Time, now time.Time, skew time.Duration) bool {
	if skew <= 0 {
		skew = 5 * time.Minute
	}
	if ts.After(now.Add(skew)) {
		return false
	}
	if ts.Before(now.Add(-skew)) {
		return false
	}
	return true
}

// VerifyAndUnmarshalEnvelope parses a network envelope into a typed message payload.
func VerifyAndUnmarshalEnvelope[T any](
	verify func(env network.Envelope, senderPub []byte, now time.Time) error,
	env network.Envelope,
	senderPub []byte,
	now time.Time,
) (T, error) {
	var zero T
	if err := verify(env, senderPub, now); err != nil {
		return zero, err
	}
	var out T
	if err := json.Unmarshal(env.Payload, &out); err != nil {
		return zero, err
	}
	return out, nil
}

