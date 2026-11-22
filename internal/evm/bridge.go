package evm

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/evm/statedb"
)

var (
	ErrInsufficientNativeBalance = errors.New("insufficient native SYN balance for EVM deposit")
	ErrInsufficientEVMBalance    = errors.New("insufficient EVM balance for withdrawal")
	ErrZeroDepositAmount         = errors.New("deposit amount must be greater than zero")
	ErrZeroWithdrawalAmount      = errors.New("withdrawal amount must be greater than zero")
)

// DepositToEVM transfers whole SYN tokens from a native address into an EVM address as 18-decimal wei.
func DepositToEVM(
	st *chain.State,
	db *statedb.StateDB,
	nativeSender chain.Address,
	evmRecipient common.Address,
	amountSYN uint64,
) error {
	if amountSYN == 0 {
		return ErrZeroDepositAmount
	}

	acc, ok := st.Accounts[nativeSender]
	if !ok || acc.Balance < amountSYN {
		return ErrInsufficientNativeBalance
	}

	// Deduct native SYN
	acc.Balance -= amountSYN
	st.Accounts[nativeSender] = acc

	// Credit EVM wei (1 SYN = 10^18 wei)
	wei := NativeToWei(amountSYN)
	db.AddBalance(evmRecipient, wei, tracing.BalanceChangeUnspecified)

	return nil
}

// WithdrawFromEVM transfers wei from an EVM address back into a native SYN address as whole tokens.
// Any fractional dust (< 10^18 wei) is retained in the EVM account.
func WithdrawFromEVM(
	st *chain.State,
	db *statedb.StateDB,
	evmSender common.Address,
	nativeRecipient chain.Address,
	amountWei *uint256.Int,
) (uint64, error) {
	if amountWei == nil || amountWei.IsZero() {
		return 0, ErrZeroWithdrawalAmount
	}

	bal := db.GetBalance(evmSender)
	if bal.Lt(amountWei) {
		return 0, ErrInsufficientEVMBalance
	}

	synTokens, _ := WeiToNative(amountWei)
	if synTokens == 0 {
		return 0, fmt.Errorf("withdrawal amount %s wei is less than 1 whole SYN (10^18 wei)", amountWei.Dec())
	}

	deductWei := NativeToWei(synTokens)
	db.SubBalance(evmSender, deductWei, tracing.BalanceChangeUnspecified)

	acc := st.Accounts[nativeRecipient]
	acc.Balance += synTokens
	st.Accounts[nativeRecipient] = acc

	return synTokens, nil
}
