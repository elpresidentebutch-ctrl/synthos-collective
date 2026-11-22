package statedb

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func TestStateDBAccountAndBalance(t *testing.T) {
	db := New()
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	if db.Exist(addr) {
		t.Fatal("account should not exist initially")
	}
	if !db.Empty(addr) {
		t.Fatal("non-existent account should be considered empty")
	}

	// Add balance
	oneEth := uint256.NewInt(1_000_000_000_000_000_000)
	db.AddBalance(addr, oneEth, tracing.BalanceChangeUnspecified)

	if !db.Exist(addr) {
		t.Fatal("account should exist after AddBalance")
	}
	if db.Empty(addr) {
		t.Fatal("funded account should not be empty")
	}

	bal := db.GetBalance(addr)
	if !bal.Eq(oneEth) {
		t.Fatalf("balance = %s, want %s", bal.Dec(), oneEth.Dec())
	}

	// Sub balance
	halfEth := uint256.NewInt(500_000_000_000_000_000)
	newBal := db.SubBalance(addr, halfEth, tracing.BalanceChangeUnspecified)
	if !newBal.Eq(halfEth) {
		t.Fatalf("new balance = %s, want %s", newBal.Dec(), halfEth.Dec())
	}

	// Nonce
	db.SetNonce(addr, 5, tracing.NonceChangeUnspecified)
	if nonce := db.GetNonce(addr); nonce != 5 {
		t.Fatalf("nonce = %d, want 5", nonce)
	}

	// Code
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}
	db.SetCode(addr, code)
	if !bytes.Equal(db.GetCode(addr), code) {
		t.Fatal("code mismatch")
	}
	if db.GetCodeSize(addr) != len(code) {
		t.Fatalf("code size = %d, want %d", db.GetCodeSize(addr), len(code))
	}
	expectedHash := crypto.Keccak256Hash(code)
	if db.GetCodeHash(addr) != expectedHash {
		t.Fatalf("code hash = %s, want %s", db.GetCodeHash(addr).Hex(), expectedHash.Hex())
	}
}

func TestStateDBStorageAndTransientStorage(t *testing.T) {
	db := New()
	addr := common.HexToAddress("0x2222222222222222222222222222222222222222")

	key := common.HexToHash("0x01")
	val := common.HexToHash("0xdeadbeef")

	// Regular storage
	prev := db.SetState(addr, key, val)
	if prev != (common.Hash{}) {
		t.Fatalf("expected initial prev value to be zero, got %s", prev.Hex())
	}
	if got := db.GetState(addr, key); got != val {
		t.Fatalf("GetState = %s, want %s", got.Hex(), val.Hex())
	}

	// Transient storage (EIP-1153)
	tKey := common.HexToHash("0x02")
	tVal := common.HexToHash("0xcafe")
	db.SetTransientState(addr, tKey, tVal)
	if got := db.GetTransientState(addr, tKey); got != tVal {
		t.Fatalf("GetTransientState = %s, want %s", got.Hex(), tVal.Hex())
	}
}

func TestStateDBSnapshotAndRevert(t *testing.T) {
	db := New()
	addr := common.HexToAddress("0x3333333333333333333333333333333333333333")

	initBal := uint256.NewInt(100)
	db.AddBalance(addr, initBal, tracing.BalanceChangeUnspecified)
	db.SetNonce(addr, 1, tracing.NonceChangeUnspecified)
	slot := common.HexToHash("0xaa")
	db.SetState(addr, slot, common.HexToHash("0x11"))
	db.SetTransientState(addr, slot, common.HexToHash("0x22"))

	// Take snapshot
	snap := db.Snapshot()

	// Make modifications in inner call
	db.AddBalance(addr, uint256.NewInt(50), tracing.BalanceChangeUnspecified)
	db.SetNonce(addr, 2, tracing.NonceChangeUnspecified)
	db.SetState(addr, slot, common.HexToHash("0x99"))
	db.SetTransientState(addr, slot, common.HexToHash("0x88"))
	db.AddLog(&types.Log{Address: addr})

	if db.GetBalance(addr).Uint64() != 150 {
		t.Fatal("balance should be 150 before revert")
	}
	if db.GetNonce(addr) != 2 {
		t.Fatal("nonce should be 2 before revert")
	}
	if db.GetState(addr, slot) != common.HexToHash("0x99") {
		t.Fatal("storage should be 0x99 before revert")
	}
	if len(db.Logs()) != 1 {
		t.Fatal("logs count should be 1 before revert")
	}

	// Revert to snapshot
	db.RevertToSnapshot(snap)

	if db.GetBalance(addr).Uint64() != 100 {
		t.Fatalf("balance after revert = %d, want 100", db.GetBalance(addr).Uint64())
	}
	if db.GetNonce(addr) != 1 {
		t.Fatalf("nonce after revert = %d, want 1", db.GetNonce(addr))
	}
	if db.GetState(addr, slot) != common.HexToHash("0x11") {
		t.Fatalf("storage after revert = %s, want 0x11", db.GetState(addr, slot).Hex())
	}
	if db.GetTransientState(addr, slot) != common.HexToHash("0x22") {
		t.Fatalf("transient storage after revert = %s, want 0x22", db.GetTransientState(addr, slot).Hex())
	}
	if len(db.Logs()) != 0 {
		t.Fatalf("logs after revert = %d, want 0", len(db.Logs()))
	}
}

func TestStateDBAccessListAndPrepare(t *testing.T) {
	db := New()
	sender := common.HexToAddress("0xaaaa000000000000000000000000000000000000")
	coinbase := common.HexToAddress("0xbbbb000000000000000000000000000000000000")
	dest := common.HexToAddress("0xcccc000000000000000000000000000000000000")
	precompile := common.HexToAddress("0x0000000000000000000000000000000000000001")

	accessList := types.AccessList{
		{
			Address: common.HexToAddress("0xdddd000000000000000000000000000000000000"),
			StorageKeys: []common.Hash{
				common.HexToHash("0x01"),
			},
		},
	}

	rules := params.Rules{IsEIP2929: true, IsShanghai: true}
	db.Prepare(rules, sender, coinbase, &dest, []common.Address{precompile}, accessList)

	if !db.AddressInAccessList(sender) {
		t.Fatal("sender should be in access list")
	}
	if !db.AddressInAccessList(coinbase) {
		t.Fatal("coinbase should be warm in Shanghai rules")
	}
	if !db.AddressInAccessList(dest) {
		t.Fatal("dest should be in access list")
	}
	if !db.AddressInAccessList(precompile) {
		t.Fatal("precompile should be in access list")
	}
	addrOk, slotOk := db.SlotInAccessList(accessList[0].Address, accessList[0].StorageKeys[0])
	if !addrOk || !slotOk {
		t.Fatalf("access list slot should be warm: addrOk=%v slotOk=%v", addrOk, slotOk)
	}
}

func TestStateDBSelfDestruct6780(t *testing.T) {
	db := New()
	existingContract := common.HexToAddress("0x4444444444444444444444444444444444444444")
	db.AddBalance(existingContract, uint256.NewInt(100), tracing.BalanceChangeUnspecified)

	// Existing contract (not created in this tx) should NOT destruct under EIP-6780
	_, destructed := db.SelfDestruct6780(existingContract)
	if destructed {
		t.Fatal("existing contract should NOT be destroyed under EIP-6780")
	}

	// Newly created contract in this tx SHOULD destruct under EIP-6780
	newContract := common.HexToAddress("0x5555555555555555555555555555555555555555")
	db.CreateContract(newContract)
	db.AddBalance(newContract, uint256.NewInt(200), tracing.BalanceChangeUnspecified)

	_, destructedNew := db.SelfDestruct6780(newContract)
	if !destructedNew {
		t.Fatal("new contract should be destroyed under EIP-6780")
	}
	if !db.HasSelfDestructed(newContract) {
		t.Fatal("HasSelfDestructed should be true for new contract")
	}
}
