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
	tmp := struct {
		Header BlockHeader `json:"header"`
		Tx     []Tx        `json:"tx"`
	}{
		Header: b.Header,
		Tx:     b.Tx,
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
