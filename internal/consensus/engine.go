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
type Engine struct {
	mu sync.Mutex

	totalValidators int
	validators      map[string]struct{}

	// proposalsByHeight stores the deterministic canonical candidate per height.
	proposalsByHeight map[uint64]*chain.Block

	// votes[height][voterID] = (blockHash, vote). Each validator votes once/height.
	votes map[uint64]map[string]voteRecord
}

type voteRecord struct {
	BlockHash string
	Vote      int
}

var (
	ErrUnknownProposal  = errors.New("unknown proposal")
	ErrUnknownValidator = errors.New("unknown validator")
)

func NewEngine(totalValidators int) *Engine {
	return &Engine{
		totalValidators:   totalValidators,
		validators:        make(map[string]struct{}),
		proposalsByHeight: make(map[uint64]*chain.Block),
		votes:             make(map[uint64]map[string]voteRecord),
	}
}

func (e *Engine) SetValidators(validators []string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.validators = make(map[string]struct{}, len(validators))
	for _, validatorID := range validators {
		if validatorID != "" {
			e.validators[validatorID] = struct{}{}
		}
	}
	e.totalValidators = len(e.validators)
}

func (e *Engine) isRegisteredValidator(voterID string) bool {
	if len(e.validators) == 0 {
		return true
	}
	_, ok := e.validators[voterID]
	return ok
}

func (e *Engine) RequiredForFinality() int {
	if e.totalValidators <= 0 {
		return 1
	}
	req := (2*e.totalValidators + 2) / 3
	if req < 1 {
		req = 1
	}
	return req
}

// OnProposal applies a deterministic tie-break for competing valid proposals.
// Every node that observes the same candidate set selects the lexicographically
// smallest full block hash, removing first-arrival ordering from fork choice.
func (e *Engine) OnProposal(b *chain.Block) {
	if b == nil || b.Hash == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	h := b.Header.Height
	existing, ok := e.proposalsByHeight[h]
	if !ok || existing == nil || b.Hash < existing.Hash {
		e.proposalsByHeight[h] = b
	}
	if _, ok := e.votes[h]; !ok {
		e.votes[h] = make(map[string]voteRecord)
	}
}

func (e *Engine) Proposal(blockHash string) (*chain.Block, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, b := range e.proposalsByHeight {
		if b != nil && b.Hash == blockHash {
			return b, true
		}
	}
	return nil, false
}

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

	if !e.isRegisteredValidator(v.VoterID) {
		return false, 0, e.RequiredForFinality(), ErrUnknownValidator
	}
	h := v.Height
	canonical, ok := e.proposalsByHeight[h]
	if !ok || canonical == nil || canonical.Hash != v.BlockHash {
		return false, 0, e.RequiredForFinality(), ErrUnknownProposal
	}
	if _, ok := e.votes[h]; !ok {
		e.votes[h] = make(map[string]voteRecord)
	}
	if _, exists := e.votes[h][v.VoterID]; !exists {
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

func FreshEnough(ts time.Time, now time.Time, skew time.Duration) bool {
	if skew <= 0 {
		skew = 5 * time.Minute
	}
	if ts.After(now.Add(skew)) || ts.Before(now.Add(-skew)) {
		return false
	}
	return true
}

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
