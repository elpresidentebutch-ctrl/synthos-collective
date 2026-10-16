package consensus

import "synthos-collective/internal/chain"

// VoteStage represents the consensus voting phase gleaned from CometBFT BFT consensus.
// Two-phase voting ensures mathematical safety: prevote achieves Proof-of-Lock (POL),
// and precommit achieves finalization/commit.
type VoteStage string

const (
	// VoteStagePrevote is the first phase: validators verify the block and cast a prevote.
	VoteStagePrevote VoteStage = "prevote"

	// VoteStagePrecommit is the second phase: validators only precommit once a 2/3+ POL is observed.
	// Empty stage in legacy votes is treated as VoteStagePrecommit for full backwards compatibility.
	VoteStagePrecommit VoteStage = "precommit"
)

// BlockProposal is broadcast by a proposer to validators.
// Gleaned from CometBFT: includes Round and ValidRound so that round retries
// are explicitly segregated and locked proposals can be re-proposed with proof.
type BlockProposal struct {
	Block      chain.Block `json:"block"`
	Height     uint64      `json:"height"`
	Round      int         `json:"round,omitempty"`
	ValidRound int         `json:"valid_round,omitempty"` // -1 if fresh; >= 0 if re-proposing a locked block
	POLRound   int         `json:"pol_round,omitempty"`   // Round where 2/3+ prevotes (POL) was achieved
}

// BlockVote is broadcast by validators to approve/reject a proposal at a given height.
// Vote semantics: 1 = accept, -1 = reject, 0 = abstain.
type BlockVote struct {
	BlockHash string    `json:"block_hash"`
	Height    uint64    `json:"height"`
	Round     int       `json:"round,omitempty"`
	Stage     VoteStage `json:"stage,omitempty"` // "prevote" or "precommit" (default: precommit)
	VoterID   string    `json:"voter_id"`
	Vote      int       `json:"vote"` // -1, 0, 1

	// Signature is the voter's ed25519 signature (hex, "0x"-prefixed) over
	// chain.Block.QuorumApprovalMessage() ("APPROVE|"+BlockHash), present only
	// on an accept (Vote == 1) vote. The engine collects these per block hash
	// (see Engine.CollectedApprovals) so the finalizing node can embed them as
	// the block's QuorumSignatures -- independently-verifiable proof that a
	// real quorum of registered validators approved this exact block, not
	// just a claim from whoever is relaying it.
	Signature string `json:"signature,omitempty"`
}

// IsPrevote returns true if the vote is in the prevote phase.
func (v BlockVote) IsPrevote() bool {
	return v.Stage == VoteStagePrevote
}

// IsPrecommit returns true if the vote is in the precommit phase (or default legacy stage).
func (v BlockVote) IsPrecommit() bool {
	return v.Stage == "" || v.Stage == VoteStagePrecommit
}
