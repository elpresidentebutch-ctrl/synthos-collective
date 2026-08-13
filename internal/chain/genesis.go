package chain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Genesis defines initial state distribution for the SYNTHOS L1.
type Genesis struct {
	ChainID   string             `json:"chain_id"`
	TxChainID uint64             `json:"tx_chain_id,omitempty"`
	Alloc     map[Address]uint64 `json:"alloc"`
	Metadata  map[string]any     `json:"metadata,omitempty"`
}

var ErrBadGenesis = errors.New("bad genesis")

func (g Genesis) Validate() error {
	if g.ChainID == "" || len(g.Alloc) == 0 {
		return ErrBadGenesis
	}
	if g.TxChainID == 0 {
		// Legacy configurations use transaction chain ID 1.
		return nil
	}
	return nil
}

func (g Genesis) TransactionChainID() uint64 {
	if g.TxChainID == 0 {
		return 1
	}
	return g.TxChainID
}

func (g Genesis) ValidateTransactionChainID(id uint64) error {
	if id != g.TransactionChainID() {
		return fmt.Errorf("wrong transaction chain ID: got %d, want %d", id, g.TransactionChainID())
	}
	return nil
}

func (g Genesis) ToState() (*State, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	s := NewState()
	for addr, bal := range g.Alloc {
		s.Set(addr, Account{Balance: bal, Nonce: 0})
	}
	if validators, ok := g.Metadata["bridge_validators"]; ok {
		parsed, err := parseBridgeValidators(validators)
		if err != nil {
			return nil, err
		}
		s.BridgeValidators = parsed
	}
	if quorum, ok := g.Metadata["bridge_quorum"]; ok {
		parsed, err := parseMetadataUint64(quorum)
		if err != nil {
			return nil, fmt.Errorf("invalid bridge_quorum: %w", err)
		}
		s.BridgeQuorum = parsed
	}
	return s, nil
}

func (g Genesis) Bytes() ([]byte, error) {
	return json.MarshalIndent(g, "", "  ")
}

func parseBridgeValidators(raw any) (map[string]string, error) {
	out := map[string]string{}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("bridge_validators must be an array")
	}
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("bridge validator must be an object")
		}
		id, _ := obj["id"].(string)
		if id == "" {
			id, _ = obj["validator_id"].(string)
		}
		pub, _ := obj["public_key"].(string)
		if id == "" || pub == "" {
			return nil, fmt.Errorf("bridge validator requires id and public_key")
		}
		out[id] = pub
	}
	return out, nil
}

func parseMetadataUint64(raw any) (uint64, error) {
	switch v := raw.(type) {
	case float64:
		if v < 0 || v != float64(uint64(v)) {
			return 0, fmt.Errorf("not a uint64")
		}
		return uint64(v), nil
	case string:
		return strconv.ParseUint(v, 10, 64)
	default:
		return 0, fmt.Errorf("unsupported type")
	}
}
