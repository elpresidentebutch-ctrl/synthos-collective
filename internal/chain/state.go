package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

type Account struct {
	Balance uint64 `json:"balance"`
	Nonce   uint64 `json:"nonce"`
}

type State struct {
	Accounts map[Address]Account `json:"accounts"`
}

func NewState() *State {
	return &State{Accounts: make(map[Address]Account)}
}

func (s *State) Get(a Address) Account {
	return s.Accounts[a]
}

func (s *State) Set(a Address, ac Account) {
	s.Accounts[a] = ac
}

func (s *State) Clone() *State {
	out := NewState()
	for k, v := range s.Accounts {
		out.Accounts[k] = v
	}
	return out
}

var (
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrBadNonce          = errors.New("bad nonce")
)

// GetNextNonce returns the next expected nonce for an address
func (s *State) GetNextNonce(a Address) uint64 {
	acc := s.Get(a)
	return acc.Nonce
}

// ApplyTx applies a transaction to state.
// Fees are deducted but not distributed here; block proposer receives them when block finalizes.
func (s *State) ApplyTx(tx Tx) error {
	if err := tx.Verify(); err != nil {
		return err
	}
	from := s.Get(tx.From)
	to := s.Get(tx.To)

	// Nonce must be exact next nonce.
	if tx.Nonce != from.Nonce {
		return ErrBadNonce
	}

	total := tx.Amount + tx.Fee
	if from.Balance < total {
		return ErrInsufficientFunds
	}

	from.Balance -= total
	from.Nonce += 1
	to.Balance += tx.Amount

	s.Set(tx.From, from)
	s.Set(tx.To, to)
	// Fee distribution: collected by block proposer during FinalizeBlock
	return nil
}

// Root returns a deterministic hash commitment over all accounts.
func (s *State) Root() string {
	// stable ordering
	addrs := make([]string, 0, len(s.Accounts))
	for a := range s.Accounts {
		addrs = append(addrs, string(a))
	}
	sort.Strings(addrs)

	type kv struct {
		Addr    string  `json:"addr"`
		Balance uint64  `json:"balance"`
		Nonce   uint64  `json:"nonce"`
	}
	items := make([]kv, 0, len(addrs))
	for _, a := range addrs {
		ac := s.Accounts[Address(a)]
		items = append(items, kv{Addr: a, Balance: ac.Balance, Nonce: ac.Nonce})
	}
	data, _ := json.Marshal(items)
	sum := sha256.Sum256(data)
	return "0x" + hex.EncodeToString(sum[:16])
}

