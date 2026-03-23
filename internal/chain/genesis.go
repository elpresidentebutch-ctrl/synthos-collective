package chain

import (
	"encoding/json"
	"errors"
)

// Genesis defines initial state distribution for the SYNTHOS L1.
type Genesis struct {
	ChainID  string                 `json:"chain_id"`
	Alloc    map[Address]uint64     `json:"alloc"`
	Metadata map[string]any         `json:"metadata,omitempty"`
}

var ErrBadGenesis = errors.New("bad genesis")

func (g Genesis) Validate() error {
	if g.ChainID == "" || len(g.Alloc) == 0 {
		return ErrBadGenesis
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

