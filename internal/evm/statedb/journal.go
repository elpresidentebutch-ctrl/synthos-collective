package statedb

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

type journalEntry interface {
	revert(s *StateDB)
}

type balanceChange struct {
	account common.Address
	prev    uint256.Int
}

func (c balanceChange) revert(s *StateDB) {
	if acc := s.getAccount(c.account); acc != nil {
		acc.Balance = c.prev
	}
}

type nonceChange struct {
	account common.Address
	prev    uint64
}

func (c nonceChange) revert(s *StateDB) {
	if acc := s.getAccount(c.account); acc != nil {
		acc.Nonce = c.prev
	}
}

type codeChange struct {
	account  common.Address
	prevCode []byte
	prevHash common.Hash
}

func (c codeChange) revert(s *StateDB) {
	if acc := s.getAccount(c.account); acc != nil {
		acc.Code = c.prevCode
		acc.CodeHash = c.prevHash
	}
}

type storageChange struct {
	account common.Address
	key     common.Hash
	prev    common.Hash
}

func (c storageChange) revert(s *StateDB) {
	if acc := s.getAccount(c.account); acc != nil {
		if c.prev == (common.Hash{}) {
			delete(acc.Storage, c.key)
		} else {
			acc.Storage[c.key] = c.prev
		}
	}
}

type transientStorageChange struct {
	account common.Address
	key     common.Hash
	prev    common.Hash
}

func (c transientStorageChange) revert(s *StateDB) {
	if m, ok := s.transientStorage[c.account]; ok {
		if c.prev == (common.Hash{}) {
			delete(m, c.key)
		} else {
			m[c.key] = c.prev
		}
	}
}

type createAccountChange struct {
	account common.Address
}

func (c createAccountChange) revert(s *StateDB) {
	delete(s.accounts, c.account)
}

type refundChange struct {
	prev uint64
}

func (c refundChange) revert(s *StateDB) {
	s.refund = c.prev
}

type selfDestructChange struct {
	account     common.Address
	prev        bool
	prevBalance uint256.Int
}

func (c selfDestructChange) revert(s *StateDB) {
	if acc := s.getAccount(c.account); acc != nil {
		acc.SelfDestructed = c.prev
		acc.Balance = c.prevBalance
	}
}

type accessListAddressChange struct {
	address common.Address
}

func (c accessListAddressChange) revert(s *StateDB) {
	delete(s.accessList.addresses, c.address)
}

type accessListSlotChange struct {
	address common.Address
	slot    common.Hash
}

func (c accessListSlotChange) revert(s *StateDB) {
	if slots, ok := s.accessList.slots[c.address]; ok {
		delete(slots, c.slot)
	}
}

type addLogChange struct {
	prevLogsCount int
}

func (c addLogChange) revert(s *StateDB) {
	s.logs = s.logs[:c.prevLogsCount]
}
