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

	// slashTracker, when set, turns observed double-signing and vote
	// equivocation into real slashing events instead of the engine just
	// silently overwriting the earlier proposal/vote. Nil-safe if unset.
	slashTracker *SlashingTracker
}

// SetSlashingTracker wires real misbehavior detection into the engine. Once
// set, OnProposal and OnVote actively check for double-signing and
// equivocation and report them to the tracker, which applies real penalties
// (see SlashingTracker.SetExecuteSlash).
func (e *Engine) SetSlashingTracker(t *SlashingTracker) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.slashTracker = t
}

// NoteMissedSlot tells the engine's slashing tracker that the validator
// expected to propose at this height did not do so before the slot passed.
// Callers (the block-production loop) decide what "passed" means -- this
// function just forwards the observation into real downtime accounting.
func (e *Engine) NoteMissedSlot(expectedProposerID string, height uint64) {
	e.mu.Lock()
	tracker := e.slashTracker
	e.mu.Unlock()
	if tracker == nil || expectedProposerID == "" {
		return
	}
	_ = tracker.RecordMissedBlock(expectedProposerID)
}

type voteRecord struct {
	BlockHash string
	Vote      int
	Signature string
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
	h := b.Header.Height
	existing, ok := e.proposalsByHeight[h]
	if !ok || existing == nil || b.Hash < existing.Hash {
		e.proposalsByHeight[h] = b
	}
	if _, ok := e.votes[h]; !ok {
		e.votes[h] = make(map[string]voteRecord)
	}

	// Real double-sign detection: report every proposal we see to the
	// tracker, which remembers (validatorID, height) pairs itself and
	// slashes on the second, conflicting sighting. An honest proposer only
	// ever builds one candidate per height, so a second sighting here is a
	// real safety violation, not a false positive from a retransmit --
	// DetectDoubleSigning is itself idempotent-safe for exact retransmits
	// only in the sense that it tracks by height, so upstream callers
	// (internal/node) should avoid re-delivering the identical envelope;
	// gossip/relay dedup already does this via replay protection.
	proposerID := b.Header.ProposerID
	tracker := e.slashTracker
	e.mu.Unlock()

	if tracker != nil && proposerID != "" {
		_ = tracker.DetectDoubleSigning(proposerID, h)
	}
}

// RecordOwnProposal registers a candidate this node is building as its own
// proposal for a not-yet-finalized height, unconditionally superseding
// whatever (if anything) this node previously proposed at that height
// itself, and clearing any votes collected for the old candidate.
//
// This exists specifically for BuildAndSignProposal's HTTP-consensus retry
// loop (rpc.Server.ProposeBlockWithConsensus, called on a timer by
// startBlockProducer): if a round doesn't reach quorum in time, the next
// tick builds a fresh candidate for the SAME still-unfinalized height. Two
// things go wrong if that fresh candidate is registered via the ordinary
// OnProposal instead:
//
//  1. Livelock: OnProposal's min-hash tie-break exists to arbitrate between
//     genuinely competing candidates from different validators within one
//     round. It was never meant to handle the SAME proposer re-registering
//     a new candidate after abandoning an old, un-finalized one -- if the
//     new candidate's hash doesn't happen to sort below the stale entry
//     already sitting in proposalsByHeight, that stale entry is never
//     replaced, and this node's own SelfVote (cast for the block it just
//     built and actually wants to finalize) fails with ErrUnknownProposal
//     forever, since nothing ever again matches the abandoned entry it's
//     stuck comparing against.
//  2. False slashing: OnProposal also reports every proposal it sees to the
//     SlashingTracker as a double-sign candidate, keyed only on (proposer,
//     height) -- not on whether the block is actually different, and with
//     no concept of "the previous one at this height was abandoned, never
//     voted on, and can never finalize." That's correct for a proposal
//     arriving from the network (HandleProposal): two different blocks
//     broadcast by the same validator for one height, in parallel, headed
//     to potentially different peers, is real equivocation. It is NOT
//     correct for a producer retrying its own round locally -- yet it was
//     firing on literally every retry, real slashing balance debit
//     included.
//
// A producer retrying an abandoned, zero-vote round is not double-signing
// in any meaningful sense -- the old candidate never had a chance at
// quorum and nothing else in the network ever saw or voted on it. So this
// method deliberately does NOT go through the SlashingTracker at all.
func (e *Engine) RecordOwnProposal(b *chain.Block) {
	if b == nil || b.Hash == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	h := b.Header.Height
	e.proposalsByHeight[h] = b
	e.votes[h] = make(map[string]voteRecord)
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

	if !e.isRegisteredValidator(v.VoterID) {
		e.mu.Unlock()
		return false, 0, e.RequiredForFinality(), ErrUnknownValidator
	}
	h := v.Height
	tracker := e.slashTracker
	e.mu.Unlock()

	// Real equivocation detection runs on what the validator actually
	// claims, independent of whether that hash happens to match today's
	// canonical candidate for this height. That's deliberate: equivocation
	// is a validator approving two different blocks at the same height --
	// it's a statement about the validator's own conflicting behavior, not
	// about which proposal the engine currently favors. Only approve votes
	// count; voting "no" on multiple candidates isn't a safety violation.
	if tracker != nil && v.Vote == 1 {
		_ = tracker.RecordEquivocation(v.VoterID, h, v.BlockHash)
	}

	e.mu.Lock()
	canonical, ok := e.proposalsByHeight[h]
	if !ok || canonical == nil || canonical.Hash != v.BlockHash {
		e.mu.Unlock()
		return false, 0, e.RequiredForFinality(), ErrUnknownProposal
	}
	if _, ok := e.votes[h]; !ok {
		e.votes[h] = make(map[string]voteRecord)
	}
	if _, exists := e.votes[h][v.VoterID]; !exists {
		e.votes[h][v.VoterID] = voteRecord{BlockHash: v.BlockHash, Vote: v.Vote, Signature: v.Signature}
	}

	required = e.RequiredForFinality()
	for _, rec := range e.votes[h] {
		if rec.BlockHash == v.BlockHash && rec.Vote == 1 {
			votesFor++
		}
	}
	finalized = votesFor >= required
	e.mu.Unlock()

	return finalized, votesFor, required, nil
}

// CollectedApprovals returns the raw approval signatures gathered so far for
// the given block hash at height, keyed by voter ID -- every accept vote
// (Vote == 1) that arrived with a non-empty Signature. This is a local,
// unverified view (Chain independently re-verifies every signature against
// its own registered validator keys before ever trusting it -- see
// Chain.verifyBlockAuthorizationLocked); it exists so a finalizing node can
// assemble a block's QuorumSignatures before calling Chain.FinalizeBlock.
func (e *Engine) CollectedApprovals(height uint64, blockHash string) map[string]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]string)
	for voterID, rec := range e.votes[height] {
		if rec.BlockHash == blockHash && rec.Vote == 1 && rec.Signature != "" {
			out[voterID] = rec.Signature
		}
	}
	return out
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
