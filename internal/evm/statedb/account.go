package statedb

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// EmptyCodeHash is Keccak256(nil), the standard code hash for accounts without contract bytecode.
var EmptyCodeHash = crypto.Keccak256Hash(nil)

// Account represents an individual EVM state account (EOA or Smart Contract).
type Account struct {
	Address          common.Address              `json:"address"`
	Nonce            uint64                      `json:"nonce"`
	Balance          uint256.Int                 `json:"balance"`
	Code             []byte                      `json:"code,omitempty"`
	CodeHash         common.Hash                 `json:"code_hash"`
	Storage          map[common.Hash]common.Hash `json:"storage,omitempty"`
	CommittedStorage map[common.Hash]common.Hash `json:"committed_storage,omitempty"`
	SelfDestructed   bool                        `json:"self_destructed,omitempty"`
	NewContract      bool                        `json:"new_contract,omitempty"`
}

// NewAccount creates an empty account with zero balance, zero nonce, and EmptyCodeHash.
func NewAccount(addr common.Address) *Account {
	return &Account{
		Address:          addr,
		CodeHash:         EmptyCodeHash,
		Storage:          make(map[common.Hash]common.Hash),
		CommittedStorage: make(map[common.Hash]common.Hash),
	}
}

// Copy performs a deep copy of the account.
func (a *Account) Copy() *Account {
	if a == nil {
		return nil
	}
	cp := &Account{
		Address:          a.Address,
		Nonce:            a.Nonce,
		Balance:          a.Balance,
		CodeHash:         a.CodeHash,
		Storage:          make(map[common.Hash]common.Hash, len(a.Storage)),
		CommittedStorage: make(map[common.Hash]common.Hash, len(a.CommittedStorage)),
		SelfDestructed:   a.SelfDestructed,
		NewContract:      a.NewContract,
	}
	if len(a.Code) > 0 {
		cp.Code = make([]byte, len(a.Code))
		copy(cp.Code, a.Code)
	}
	for k, v := range a.Storage {
		cp.Storage[k] = v
	}
	for k, v := range a.CommittedStorage {
		cp.CommittedStorage[k] = v
	}
	return cp
}

// Empty returns whether the account is empty per EIP-161 (balance = 0, nonce = 0, code = empty).
func (a *Account) Empty() bool {
	if a == nil {
		return true
	}
	return a.Nonce == 0 && a.Balance.IsZero() && (len(a.Code) == 0 || a.CodeHash == EmptyCodeHash)
}
