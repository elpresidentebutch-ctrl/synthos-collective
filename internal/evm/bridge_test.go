package evm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/evm/statedb"
)

func TestDualLedgerBridge(t *testing.T) {
	st := &chain.State{
		Accounts: make(map[chain.Address]chain.Account),
	}
	db := statedb.New()

	nativeSender := chain.Address("0x1111111111111111111111111111111111111111")
	evmRecipient := common.HexToAddress("0x2222222222222222222222222222222222222222")

	// Fund native address with 100 SYN
	st.Accounts[nativeSender] = chain.Account{
		Balance: 100,
	}

	// 1. Deposit 40 SYN to EVM
	if err := DepositToEVM(st, db, nativeSender, evmRecipient, 40); err != nil {
		t.Fatalf("DepositToEVM failed: %v", err)
	}

	if st.Accounts[nativeSender].Balance != 60 {
		t.Fatalf("native balance after deposit = %d, want 60", st.Accounts[nativeSender].Balance)
	}

	expectedWei := NativeToWei(40)
	if !db.GetBalance(evmRecipient).Eq(expectedWei) {
		t.Fatalf("EVM balance after deposit = %s, want %s", db.GetBalance(evmRecipient).Dec(), expectedWei.Dec())
	}

	// 2. Try to deposit more than available
	if err := DepositToEVM(st, db, nativeSender, evmRecipient, 100); err != ErrInsufficientNativeBalance {
		t.Fatalf("expected ErrInsufficientNativeBalance, got %v", err)
	}

	// 3. Withdraw 15.5 SYN (15 whole SYN + 0.5 SYN in wei)
	// Only 15 whole SYN should move to native; 0.5 SYN dust stays in EVM
	fifteenSYNWei := NativeToWei(15)
	halfSYNWei := uint256.NewInt(500_000_000_000_000_000)
	withdrawWei := new(uint256.Int).Add(fifteenSYNWei, halfSYNWei)

	synWithdrawn, err := WithdrawFromEVM(st, db, evmRecipient, nativeSender, withdrawWei)
	if err != nil {
		t.Fatalf("WithdrawFromEVM failed: %v", err)
	}
	if synWithdrawn != 15 {
		t.Fatalf("synWithdrawn = %d, want 15", synWithdrawn)
	}

	// Native balance should be 60 + 15 = 75
	if st.Accounts[nativeSender].Balance != 75 {
		t.Fatalf("native balance after withdrawal = %d, want 75", st.Accounts[nativeSender].Balance)
	}

	// EVM balance should be 40 SYN - 15 SYN = 25 SYN
	expectedRemainingEVM := NativeToWei(25)
	if !db.GetBalance(evmRecipient).Eq(expectedRemainingEVM) {
		t.Fatalf("EVM remaining balance = %s, want %s", db.GetBalance(evmRecipient).Dec(), expectedRemainingEVM.Dec())
	}

	// 4. Withdrawal of sub-SYN dust alone (< 10^18 wei) should fail cleanly
	dustOnly := uint256.NewInt(999_999_999_999_999_999)
	if _, err := WithdrawFromEVM(st, db, evmRecipient, nativeSender, dustOnly); err == nil {
		t.Fatal("expected error withdrawing sub-SYN dust")
	}
}
