package statedb

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie/utils"
	"github.com/holiman/uint256"
)

var _ vm.StateDB = (*StateDB)(nil)

// AccessList tracks addresses and storage slots warmed during transaction execution (EIP-2929).
type AccessList struct {
	addresses map[common.Address]struct{}
	slots     map[common.Address]map[common.Hash]struct{}
}

func newAccessList() *AccessList {
	return &AccessList{
		addresses: make(map[common.Address]struct{}),
		slots:     make(map[common.Address]map[common.Hash]struct{}),
	}
}

// StateDB is an in-memory, journaled EVM state database implementing vm.StateDB.
type StateDB struct {
	mu               sync.RWMutex
	accounts         map[common.Address]*Account
	refund           uint64
	transientStorage map[common.Address]map[common.Hash]common.Hash
	accessList       *AccessList
	logs             []*types.Log
	preimages        map[common.Hash][]byte
	journal          []journalEntry
	pointCache       *utils.PointCache
}

// New creates a new, empty StateDB.
func New() *StateDB {
	return &StateDB{
		accounts:         make(map[common.Address]*Account),
		transientStorage: make(map[common.Address]map[common.Hash]common.Hash),
		accessList:       newAccessList(),
		logs:             make([]*types.Log, 0),
		preimages:        make(map[common.Hash][]byte),
		journal:          make([]journalEntry, 0),
		pointCache:       utils.NewPointCache(1024),
	}
}

// getAccount returns an account if present in state.
func (s *StateDB) getAccount(addr common.Address) *Account {
	return s.accounts[addr]
}

// getOrCreateAccount returns the account for addr, creating it if not present.
func (s *StateDB) getOrCreateAccount(addr common.Address) *Account {
	acc := s.accounts[addr]
	if acc == nil {
		acc = NewAccount(addr)
		s.accounts[addr] = acc
		s.journal = append(s.journal, createAccountChange{account: addr})
	}
	return acc
}

// CreateAccount explicitly adds a new account to the state.
func (s *StateDB) CreateAccount(addr common.Address) {
	acc := s.accounts[addr]
	if acc == nil {
		s.accounts[addr] = NewAccount(addr)
		s.journal = append(s.journal, createAccountChange{account: addr})
	}
}

// CreateContract marks the account as a new contract being created in the current tx.
func (s *StateDB) CreateContract(addr common.Address) {
	acc := s.getOrCreateAccount(addr)
	acc.NewContract = true
}

// SubBalance subtracts amount from addr's balance, returning the new balance.
func (s *StateDB) SubBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	if amount == nil || amount.IsZero() {
		if acc := s.getAccount(addr); acc != nil {
			return acc.Balance
		}
		return uint256.Int{}
	}
	acc := s.getOrCreateAccount(addr)
	s.journal = append(s.journal, balanceChange{account: addr, prev: acc.Balance})
	acc.Balance.Sub(&acc.Balance, amount)
	return acc.Balance
}

// AddBalance adds amount to addr's balance, returning the new balance.
func (s *StateDB) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	if amount == nil || amount.IsZero() {
		if acc := s.getAccount(addr); acc != nil {
			return acc.Balance
		}
		return uint256.Int{}
	}
	acc := s.getOrCreateAccount(addr)
	s.journal = append(s.journal, balanceChange{account: addr, prev: acc.Balance})
	acc.Balance.Add(&acc.Balance, amount)
	return acc.Balance
}

// SetBalance sets addr's balance directly.
func (s *StateDB) SetBalance(addr common.Address, amount *uint256.Int) {
	acc := s.getOrCreateAccount(addr)
	s.journal = append(s.journal, balanceChange{account: addr, prev: acc.Balance})
	if amount != nil {
		acc.Balance = *amount
	} else {
		acc.Balance = uint256.Int{}
	}
}

// GetBalance returns addr's balance in wei.
func (s *StateDB) GetBalance(addr common.Address) *uint256.Int {
	if acc := s.getAccount(addr); acc != nil {
		return &acc.Balance
	}
	return uint256.NewInt(0)
}

// GetNonce returns the transaction nonce of addr.
func (s *StateDB) GetNonce(addr common.Address) uint64 {
	if acc := s.getAccount(addr); acc != nil {
		return acc.Nonce
	}
	return 0
}

// SetNonce sets the transaction nonce of addr.
func (s *StateDB) SetNonce(addr common.Address, nonce uint64) {
	acc := s.getOrCreateAccount(addr)
	s.journal = append(s.journal, nonceChange{account: addr, prev: acc.Nonce})
	acc.Nonce = nonce
}

// GetCodeHash returns the Keccak-256 hash of addr's bytecode.
func (s *StateDB) GetCodeHash(addr common.Address) common.Hash {
	if acc := s.getAccount(addr); acc != nil {
		return acc.CodeHash
	}
	return EmptyCodeHash
}

// GetCode returns addr's contract bytecode.
func (s *StateDB) GetCode(addr common.Address) []byte {
	if acc := s.getAccount(addr); acc != nil {
		return acc.Code
	}
	return nil
}

// SetCode updates addr's contract bytecode and recalculates its CodeHash.
func (s *StateDB) SetCode(addr common.Address, code []byte) {
	acc := s.getOrCreateAccount(addr)
	s.journal = append(s.journal, codeChange{account: addr, prevCode: acc.Code, prevHash: acc.CodeHash})
	acc.Code = code
	if len(code) > 0 {
		acc.CodeHash = crypto.Keccak256Hash(code)
	} else {
		acc.CodeHash = EmptyCodeHash
	}
}

// GetCodeSize returns the length of addr's contract bytecode.
func (s *StateDB) GetCodeSize(addr common.Address) int {
	if acc := s.getAccount(addr); acc != nil {
		return len(acc.Code)
	}
	return 0
}

// AddRefund increases the gas refund counter.
func (s *StateDB) AddRefund(gas uint64) {
	s.journal = append(s.journal, refundChange{prev: s.refund})
	s.refund += gas
}

// SubRefund decreases the gas refund counter.
func (s *StateDB) SubRefund(gas uint64) {
	s.journal = append(s.journal, refundChange{prev: s.refund})
	if gas > s.refund {
		s.refund = 0
	} else {
		s.refund -= gas
	}
}

// GetRefund returns the current gas refund counter.
func (s *StateDB) GetRefund() uint64 {
	return s.refund
}

// GetCommittedState returns the committed storage value for key in addr's storage.
func (s *StateDB) GetCommittedState(addr common.Address, key common.Hash) common.Hash {
	if acc := s.getAccount(addr); acc != nil {
		return acc.CommittedStorage[key]
	}
	return common.Hash{}
}

// GetState returns the current storage value for key in addr's storage.
func (s *StateDB) GetState(addr common.Address, key common.Hash) common.Hash {
	if acc := s.getAccount(addr); acc != nil {
		if val, ok := acc.Storage[key]; ok {
			return val
		}
		return acc.CommittedStorage[key]
	}
	return common.Hash{}
}

// SetState updates a storage slot in addr's state and returns the previous value.
func (s *StateDB) SetState(addr common.Address, key, value common.Hash) common.Hash {
	acc := s.getOrCreateAccount(addr)
	prev := s.GetState(addr, key)
	if prev == value {
		return prev
	}
	s.journal = append(s.journal, storageChange{account: addr, key: key, prev: prev})
	acc.Storage[key] = value
	return prev
}

// GetStorageRoot returns a synthetic root of addr's storage mapping.
func (s *StateDB) GetStorageRoot(addr common.Address) common.Hash {
	acc := s.getAccount(addr)
	if acc == nil || (len(acc.Storage) == 0 && len(acc.CommittedStorage) == 0) {
		return common.Hash{}
	}
	return crypto.Keccak256Hash([]byte(addr.Hex() + ":storage"))
}

// GetTransientState returns transient storage per EIP-1153.
func (s *StateDB) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	if m, ok := s.transientStorage[addr]; ok {
		return m[key]
	}
	return common.Hash{}
}

// SetTransientState sets transient storage per EIP-1153.
func (s *StateDB) SetTransientState(addr common.Address, key, value common.Hash) {
	prev := s.GetTransientState(addr, key)
	if prev == value {
		return
	}
	s.journal = append(s.journal, transientStorageChange{account: addr, key: key, prev: prev})
	m := s.transientStorage[addr]
	if m == nil {
		m = make(map[common.Hash]common.Hash)
		s.transientStorage[addr] = m
	}
	m[key] = value
}

// SelfDestruct marks the account as destructed and returns its prior balance.
func (s *StateDB) SelfDestruct(addr common.Address) uint256.Int {
	acc := s.getAccount(addr)
	if acc == nil {
		return uint256.Int{}
	}
	prevBalance := acc.Balance
	s.journal = append(s.journal, selfDestructChange{account: addr, prev: acc.SelfDestructed, prevBalance: prevBalance})
	acc.SelfDestructed = true
	acc.Balance = uint256.Int{}
	return prevBalance
}

// HasSelfDestructed returns whether addr is marked for destruction.
func (s *StateDB) HasSelfDestructed(addr common.Address) bool {
	if acc := s.getAccount(addr); acc != nil {
		return acc.SelfDestructed
	}
	return false
}

// SelfDestruct6780 implements EIP-6780 selfdestruct behavior: destructs only if created in same tx.
func (s *StateDB) SelfDestruct6780(addr common.Address) (uint256.Int, bool) {
	acc := s.getAccount(addr)
	if acc == nil {
		return uint256.Int{}, false
	}
	if acc.NewContract {
		return s.SelfDestruct(addr), true
	}
	return acc.Balance, false
}

// Exist reports whether the account exists in state.
func (s *StateDB) Exist(addr common.Address) bool {
	return s.getAccount(addr) != nil
}

// Empty returns whether the account is empty per EIP-161.
func (s *StateDB) Empty(addr common.Address) bool {
	acc := s.getAccount(addr)
	return acc == nil || acc.Empty()
}

// AddressInAccessList checks if addr is warm per EIP-2929.
func (s *StateDB) AddressInAccessList(addr common.Address) bool {
	if s.accessList == nil {
		return false
	}
	_, ok := s.accessList.addresses[addr]
	return ok
}

// SlotInAccessList checks if (addr, slot) is warm per EIP-2929.
func (s *StateDB) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	if s.accessList == nil {
		return false, false
	}
	_, addressOk = s.accessList.addresses[addr]
	if slots, ok := s.accessList.slots[addr]; ok {
		_, slotOk = slots[slot]
	}
	return addressOk, slotOk
}

// AddAddressToAccessList warms addr per EIP-2929.
func (s *StateDB) AddAddressToAccessList(addr common.Address) {
	if s.accessList == nil {
		s.accessList = newAccessList()
	}
	if _, ok := s.accessList.addresses[addr]; !ok {
		s.journal = append(s.journal, accessListAddressChange{address: addr})
		s.accessList.addresses[addr] = struct{}{}
	}
}

// AddSlotToAccessList warms (addr, slot) per EIP-2929.
func (s *StateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	s.AddAddressToAccessList(addr)
	slots := s.accessList.slots[addr]
	if slots == nil {
		slots = make(map[common.Hash]struct{})
		s.accessList.slots[addr] = slots
	}
	if _, ok := slots[slot]; !ok {
		s.journal = append(s.journal, accessListSlotChange{address: addr, slot: slot})
		slots[slot] = struct{}{}
	}
}

// PointCache returns the KZG/verkle point cache.
func (s *StateDB) PointCache() *utils.PointCache {
	return s.pointCache
}

// Prepare warms precompiles, sender, destination, and access list keys for a transaction.
func (s *StateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {
	s.accessList = newAccessList()
	s.accessList.addresses[sender] = struct{}{}
	if dest != nil {
		s.accessList.addresses[*dest] = struct{}{}
	}
	for _, p := range precompiles {
		s.accessList.addresses[p] = struct{}{}
	}
	for _, el := range txAccesses {
		s.accessList.addresses[el.Address] = struct{}{}
		slots := s.accessList.slots[el.Address]
		if slots == nil {
			slots = make(map[common.Hash]struct{})
			s.accessList.slots[el.Address] = slots
		}
		for _, key := range el.StorageKeys {
			slots[key] = struct{}{}
		}
	}
	if rules.IsShanghai {
		s.accessList.addresses[coinbase] = struct{}{}
	}
	// Reset transient storage for each top-level transaction per EIP-1153
	s.transientStorage = make(map[common.Address]map[common.Hash]common.Hash)
}

// Snapshot returns the current revision ID.
func (s *StateDB) Snapshot() int {
	return len(s.journal)
}

// RevertToSnapshot rolls back state mutations made since revision.
func (s *StateDB) RevertToSnapshot(revision int) {
	for i := len(s.journal) - 1; i >= revision; i-- {
		s.journal[i].revert(s)
	}
	s.journal = s.journal[:revision]
}

// AddLog appends a new event log to the state logs.
func (s *StateDB) AddLog(log *types.Log) {
	s.journal = append(s.journal, addLogChange{prevLogsCount: len(s.logs)})
	s.logs = append(s.logs, log)
}

// Logs returns all event logs emitted in the current transaction.
func (s *StateDB) Logs() []*types.Log {
	return s.logs
}

// ClearLogs clears transaction logs.
func (s *StateDB) ClearLogs() {
	s.logs = make([]*types.Log, 0)
}

// AddPreimage stores a SHA3/Keccak preimage.
func (s *StateDB) AddPreimage(hash common.Hash, preimage []byte) {
	s.preimages[hash] = preimage
}

// Witness returns the stateless execution witness, if any.
func (s *StateDB) Witness() *stateless.Witness {
	return nil
}

// Finalise commits volatile state changes and cleans up deleted or empty accounts.
func (s *StateDB) Finalise(deleteEmptyObjects bool) {
	for addr, acc := range s.accounts {
		if acc.SelfDestructed || (deleteEmptyObjects && acc.Empty()) {
			delete(s.accounts, addr)
			continue
		}
		// Promote dirty storage to committed storage
		for k, v := range acc.Storage {
			if v == (common.Hash{}) {
				delete(acc.CommittedStorage, k)
			} else {
				acc.CommittedStorage[k] = v
			}
		}
		acc.Storage = make(map[common.Hash]common.Hash)
		acc.NewContract = false
	}
	s.journal = nil
}

// Accounts returns a snapshot map of all accounts in the StateDB.
func (s *StateDB) Accounts() map[common.Address]*Account {
	cp := make(map[common.Address]*Account, len(s.accounts))
	for k, v := range s.accounts {
		cp[k] = v.Copy()
	}
	return cp
}
