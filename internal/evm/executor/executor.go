package executor

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"synthos-collective/internal/evm/statedb"
)

// BlockContext provides block-level metadata to the EVM environment.
type BlockContext struct {
	Coinbase    common.Address
	BlockNumber *big.Int
	Time        uint64
	BaseFee     *big.Int
	GasLimit    uint64
	Random      *common.Hash
}

// ExecutionResult wraps the EVM execution outcome for a transaction.
type ExecutionResult struct {
	UsedGas         uint64
	RefundedGas     uint64
	ReturnData      []byte
	ContractAddress common.Address
	Status          uint64 // 1 for success, 0 for revert/failure
	Logs            []*types.Log
	Err             error
}

// EVMExecutor executes transactions and messages against a StateDB.
type EVMExecutor struct {
	chainConfig *params.ChainConfig
}

// NewEVMExecutor creates a new EVMExecutor for the specified chain configuration.
func NewEVMExecutor(chainConfig *params.ChainConfig) *EVMExecutor {
	return &EVMExecutor{
		chainConfig: chainConfig,
	}
}

// ApplyTransaction executes an Ethereum transaction against the given StateDB.
func (e *EVMExecutor) ApplyTransaction(
	db *statedb.StateDB,
	tx *types.Transaction,
	blockCtx BlockContext,
) (*ExecutionResult, error) {
	signer := types.MakeSigner(e.chainConfig, blockCtx.BlockNumber, blockCtx.Time)
	_, err := types.Sender(signer, tx)
	if err != nil {
		return nil, fmt.Errorf("failed to recover transaction signer: %w", err)
	}

	msg, err := core.TransactionToMessage(tx, signer, blockCtx.BaseFee)
	if err != nil {
		return nil, fmt.Errorf("failed to convert transaction to message: %w", err)
	}

	return e.ApplyMessage(db, msg, blockCtx, tx.Gas())
}

// ApplyMessage executes a core.Message against the given StateDB.
func (e *EVMExecutor) ApplyMessage(
	db *statedb.StateDB,
	msg *core.Message,
	blockCtx BlockContext,
	txGasLimit uint64,
) (*ExecutionResult, error) {
	db.ClearLogs()

	randomHash := common.Hash{}
	if blockCtx.Random != nil {
		randomHash = *blockCtx.Random
	}

	vmBlockCtx := vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash: func(n uint64) common.Hash {
			return crypto.Keccak256Hash([]byte(fmt.Sprintf("synthos:block:%d", n)))
		},
		Coinbase:    blockCtx.Coinbase,
		GasLimit:    blockCtx.GasLimit,
		BlockNumber: blockCtx.BlockNumber,
		Time:        blockCtx.Time,
		Difficulty:  big.NewInt(1),
		BaseFee:     blockCtx.BaseFee,
		BlobBaseFee: big.NewInt(1),
		Random:      &randomHash,
	}

	vmTxCtx := core.NewEVMTxContext(msg)
	evmInst := vm.NewEVM(vmBlockCtx, vmTxCtx, db, e.chainConfig, vm.Config{})

	rules := e.chainConfig.Rules(blockCtx.BlockNumber, true, blockCtx.Time)
	db.Prepare(rules, msg.From, blockCtx.Coinbase, msg.To, vm.ActivePrecompiles(rules), msg.AccessList)

	gasPool := new(core.GasPool).AddGas(txGasLimit)
	res, err := core.ApplyMessage(evmInst, msg, gasPool)

	out := &ExecutionResult{
		Logs: db.Logs(),
	}

	if msg.To == nil {
		out.ContractAddress = crypto.CreateAddress(msg.From, msg.Nonce)
	}

	if err != nil {
		out.Status = 0
		out.Err = err
		return out, err
	}

	if res != nil {
		out.UsedGas = res.UsedGas
		out.RefundedGas = res.RefundedGas
		out.ReturnData = res.ReturnData
		if res.Failed() {
			out.Status = 0
			out.Err = res.Err
		} else {
			out.Status = 1
		}
	} else {
		out.Status = 1
	}

	db.Finalise(rules.IsEIP158)
	return out, nil
}
