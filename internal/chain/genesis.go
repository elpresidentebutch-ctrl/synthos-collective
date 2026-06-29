package chain

import (
	"encoding/json"
	"errors"
	"fmt"
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
	return s, nil
}

func (g Genesis) Bytes() ([]byte, error) {
	return json.MarshalIndent(g, "", "  ")
}
