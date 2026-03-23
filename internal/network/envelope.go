package network

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// Envelope is the signed message container relayed through untrusted infrastructure.
// Relays must treat it as opaque bytes; all trust decisions are made by agents.
type Envelope struct {
	Version               string          `json:"version"`
	MessageType           string          `json:"message_type"`
	FromAgentID           string          `json:"from_agent"`
	ToAgentID             string          `json:"to_agent,omitempty"`
	Topic                 string          `json:"topic,omitempty"`
	Nonce                 string          `json:"nonce"`
	Timestamp             time.Time       `json:"timestamp"`
	Payload               json.RawMessage  `json:"payload"`
	ProofOfComputationRoot string         `json:"proof_of_computation_root"`
	HardwareIDHash        string          `json:"hardware_id_hash"`
	Signature             string          `json:"signature"`
}

var (
	ErrMissingField = errors.New("missing required field")
)

// HardwareIDHashHex returns a stable commitment to a hardware ID without revealing it.
// This is a simple SHA-256 hash; in production you may want a domain-separated hash.
func HardwareIDHashHex(hardwareID string) string {
	sum := sha256.Sum256([]byte(hardwareID))
	return "0x" + hex.EncodeToString(sum[:])
}

// NonceHex derives a nonce from random bytes (expected to be already random).
func NonceHex(b []byte) string {
	sum := sha256.Sum256(b)
	return "0x" + hex.EncodeToString(sum[:8])
}

// SigningBytes returns the deterministic bytes that must be signed.
// Signature is excluded by design.
func (e Envelope) SigningBytes() ([]byte, error) {
	tmp := struct {
		Version               string         `json:"version"`
		MessageType           string         `json:"message_type"`
		FromAgentID           string         `json:"from_agent"`
		ToAgentID             string         `json:"to_agent,omitempty"`
		Topic                 string         `json:"topic,omitempty"`
		Nonce                 string         `json:"nonce"`
		Timestamp             time.Time      `json:"timestamp"`
		Payload               json.RawMessage `json:"payload"`
		ProofOfComputationRoot string        `json:"proof_of_computation_root"`
		HardwareIDHash        string         `json:"hardware_id_hash"`
	}{
		Version:               e.Version,
		MessageType:           e.MessageType,
		FromAgentID:           e.FromAgentID,
		ToAgentID:             e.ToAgentID,
		Topic:                 e.Topic,
		Nonce:                 e.Nonce,
		Timestamp:             e.Timestamp,
		Payload:               e.Payload,
		ProofOfComputationRoot: e.ProofOfComputationRoot,
		HardwareIDHash:        e.HardwareIDHash,
	}
	return json.Marshal(tmp)
}

func (e Envelope) ValidateBasic() error {
	if e.Version == "" || e.MessageType == "" || e.FromAgentID == "" || e.Nonce == "" {
		return ErrMissingField
	}
	if e.Timestamp.IsZero() {
		return ErrMissingField
	}
	if len(e.Payload) == 0 {
		return ErrMissingField
	}
	if e.ProofOfComputationRoot == "" || e.HardwareIDHash == "" {
		return ErrMissingField
	}
	if e.Signature == "" {
		return ErrMissingField
	}
	return nil
}

