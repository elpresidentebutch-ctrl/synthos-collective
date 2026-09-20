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

	// Signature is the voter's ed25519 signature (hex, "0x"-prefixed) over
	// chain.Block.QuorumApprovalMessage() ("APPROVE|"+BlockHash), present only
	// on an accept (Vote == 1) vote. The engine collects these per block hash
	// (see Engine.CollectedApprovals) so the finalizing node can embed them as
	// the block's QuorumSignatures -- independently-verifiable proof that a
	// real quorum of registered validators approved this exact block, not
	// just a claim from whoever is relaying it.
	Signature string `json:"signature,omitempty"`
}
