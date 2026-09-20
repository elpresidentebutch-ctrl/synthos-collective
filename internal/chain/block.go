package chain

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

type BlockHeader struct {
	Height     uint64 `json:"height"`
	ParentHash string `json:"parent_hash"`
	// Timestamp is consensus-critical ONLY for genesis (height 0).
	// For all non-genesis blocks it must be the zero value.
	Timestamp  time.Time `json:"timestamp,omitempty"`
	ProposerID string    `json:"proposer_id"`

	// TxMerkleRoot commits to the exact ordered transaction list.
	TxMerkleRoot string `json:"tx_merkle_root"`

	// StateRoot commits to the complete post-block state, including fees.
	StateRoot string `json:"state_root"`

	// Optional: commit to the proposer’s PoC history.
	ProposerPoCRoot string `json:"proposer_poc_root,omitempty"`
}

type Block struct {
	Header         BlockHeader    `json:"header"`
	Tx             []Tx           `json:"tx"`
	Hash           string         `json:"hash"`
	ValidatorVotes map[string]int `json:"validator_votes,omitempty"` // agentID -> -1/0/1
	Finalized      bool           `json:"finalized"`

	// ProposerSignature is the block proposer's ed25519 signature (hex,
	// "0x"-prefixed) over Hash, proving the block genuinely came from the
	// validator named in Header.ProposerID rather than being forged by an
	// unauthorized party. Populated by the proposer (see
	// internal/node.Node.ProposeBlock) before the block is broadcast or
	// finalized; verified in Chain.validateBlockLocked once a validator set
	// is configured (see Chain.SetValidatorSet). Left empty for chains that
	// don't configure a validator set, preserving old behavior.
	ProposerSignature string `json:"proposer_signature,omitempty"`

	// QuorumSignatures maps each approving validator's ID to their ed25519
	// signature (hex, "0x"-prefixed) over QuorumApprovalMessage(). A block is
	// only accepted once at least the chain's configured quorum threshold of
	// distinct, registered validators' signatures are present and verify --
	// this is the cryptographic proof that real consensus, not just a single
	// proposer or an unauthenticated HTTP request, approved the block. It is
	// what makes cross-node block sync (CatchUpOnce / handleGossipBlock) safe
	// against a malicious or compromised peer.
	QuorumSignatures map[string]string `json:"quorum_signatures,omitempty"`
}

// QuorumApprovalMessage returns the canonical bytes a validator signs to
// record their approval of this block by hash. Used both when a validator
// casts an approving vote and when verifying QuorumSignatures.
func (b *Block) QuorumApprovalMessage() []byte {
	return []byte("APPROVE|" + b.Hash)
}

// verifiedApprovalCount returns how many DISTINCT, cryptographically valid
// approvals this block carries, counting only QuorumSignatures entries whose
// signer is present in keys and whose signature actually verifies. Used only
// for fork-choice weight comparisons (see Chain.TryReorg); ordinary block
// acceptance already independently enforces the quorum threshold via
// Chain.verifyBlockAuthorizationLocked.
func (b *Block) verifiedApprovalCount(keys map[string]ed25519.PublicKey) int {
	if len(keys) == 0 {
		return 0
	}
	approvalMsg := b.QuorumApprovalMessage()
	count := 0
	for validatorID, sigHex := range b.QuorumSignatures {
		key, ok := keys[validatorID]
		if !ok || len(key) != ed25519.PublicKeySize {
			continue
		}
		sig, err := decodeHexSig(sigHex)
		if err != nil || !ed25519.Verify(key, approvalMsg, sig) {
			continue
		}
		count++
	}
	return count
}

// CalculateHash returns the canonical block hash without mutating the block.
func (b *Block) CalculateHash() (string, error) {
	// Canonicalize a nil transaction list to an empty (non-nil) slice before
	// hashing non-genesis blocks. The block producer assembles Tx as a
	// non-nil, possibly-empty slice, so a proposed block's hash is computed
	// over a JSON body containing "tx":[]. But once that block round-trips
	// through storage or an HTTP response, an empty slice unmarshals back as
	// a nil slice, which encodes as "tx":null instead. That mismatched byte
	// representation makes a peer's hash recomputation during validation
	// (validateBlockLocked) disagree with the original b.Hash for every empty
	// block, so HTTP peer sync can never finalize a single block.
	// Height 0 (genesis) is exempt: NewChain hard-codes Tx to a literal nil,
	// and the genesis hash already anchored across the live network was
	// computed against "tx":null, so it must not shift under this fix.
	txs := b.Tx
	if txs == nil && b.Header.Height > 0 {
		txs = []Tx{}
	}
	tmp := struct {
		Header BlockHeader `json:"header"`
		Tx     []Tx        `json:"tx"`
	}{
		Header: b.Header,
		Tx:     txs,
	}
	data, err := json.Marshal(tmp)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "0x" + hex.EncodeToString(sum[:]), nil
}

func (b *Block) ComputeHash() (string, error) {
	hash, err := b.CalculateHash()
	if err != nil {
		return "", err
	}
	b.Hash = hash
	return hash, nil
}
