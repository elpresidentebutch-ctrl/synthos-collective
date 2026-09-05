package chain

import (
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
	Header         BlockHeader   `json:"header"`
	Tx             []Tx          `json:"tx"`
	Hash           string        `json:"hash"`
	ValidatorVotes map[string]int `json:"validator_votes,omitempty"` // agentID -> -1/0/1
	Finalized      bool           `json:"finalized"`
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
