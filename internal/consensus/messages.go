package consensus

import "synthos-collective/internal/chain"

// BlockProposal is broadcast by a proposer to validators.
// In v0.1 we include Height explicitly so that consensus can reason per-height.
type BlockProposal struct {
	Block  chain.Block `json:"block"`
	Height uint64      `json:"height"`
}

// BlockVote is broadcast by validators to approve/reject a proposal at a given height.
// Vote semantics: 1 = accept, -1 = reject, 0 = abstain.
type BlockVote struct {
	BlockHash string `json:"block_hash"`
	Height    uint64 `json:"height"`
	VoterID   string `json:"voter_id"`
	Vote      int    `json:"vote"` // -1, 0, 1
}

