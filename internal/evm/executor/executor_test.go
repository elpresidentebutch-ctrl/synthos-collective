package executor

import (
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"synthos-collective/internal/evm"
	"synthos-collective/internal/evm/statedb"
)

func TestEVMTransfer(t *testing.T) {
	chainID := big.NewInt(int64(evm.DevnetEVMChainID))
	cfg := evm.DefaultChainConfig(chainID)
	exec := NewEVMExecutor(cfg)

	db := statedb.New()

	privKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	sender := crypto.PubkeyToAddress(privKey.PublicKey)
	recipient := common.HexToAddress("0x7777777777777777777777777777777777777777")

	// Pre-fund sender with 10 SYN in wei
	initBalance := evm.NativeToWei(10)
	db.SetBalance(sender, initBalance)

	transferAmount := evm.NativeToWei(3) // 3 SYN
	baseFee := big.NewInt(1_000_000_000) // 1 gwei

	txData := &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasTipCap: big.NewInt(1_000_000_000), // 1 gwei tip
		GasFeeCap: big.NewInt(2_000_000_000), // 2 gwei max
		Gas:       21_000,
		To:        &recipient,
		Value:     transferAmount.ToBig(),
		Data:      nil,
	}

	signer := types.MakeSigner(cfg, big.NewInt(1), 1700000000)
	signedTx, err := types.SignTx(types.NewTx(txData), signer, privKey)
	if err != nil {
		t.Fatalf("failed to sign tx: %v", err)
	}

	blockCtx := BlockContext{
		Coinbase:    common.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc"),
		BlockNumber: big.NewInt(1),
		Time:        1700000000,
		BaseFee:     baseFee,
		GasLimit:    evm.BlockGasLimit,
	}

	res, err := exec.ApplyTransaction(db, signedTx, blockCtx)
	if err != nil {
		t.Fatalf("ApplyTransaction failed: %v", err)
	}

	if res.Status != 1 {
		t.Fatalf("expected tx status 1 (success), got %d (err: %v)", res.Status, res.Err)
	}

	if res.UsedGas != 21_000 {
		t.Fatalf("used gas = %d, want 21,000", res.UsedGas)
	}

	// Verify recipient received 3 SYN
	recBal := db.GetBalance(recipient)
	if !recBal.Eq(transferAmount) {
		t.Fatalf("recipient balance = %s, want %s", recBal.Dec(), transferAmount.Dec())
	}

	// Verify sender balance decreased by 3 SYN + gas fee
	senderBal := db.GetBalance(sender)
	gasCost := new(uint256.Int).Mul(uint256.NewInt(21_000), uint256.NewInt(2_000_000_000))
	expectedRemaining := new(uint256.Int).Sub(initBalance, transferAmount)
	expectedRemaining.Sub(expectedRemaining, gasCost)
	if !senderBal.Eq(expectedRemaining) {
		t.Fatalf("sender balance = %s, want %s", senderBal.Dec(), expectedRemaining.Dec())
	}

	// Verify sender nonce incremented
	if nonce := db.GetNonce(sender); nonce != 1 {
		t.Fatalf("sender nonce = %d, want 1", nonce)
	}
}

func TestEVMContractDeployAndExecute(t *testing.T) {
	chainID := big.NewInt(int64(evm.DevnetEVMChainID))
	cfg := evm.DefaultChainConfig(chainID)
	exec := NewEVMExecutor(cfg)

	db := statedb.New()

	privKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	sender := crypto.PubkeyToAddress(privKey.PublicKey)

	// Pre-fund sender with 50 SYN
	db.SetBalance(sender, evm.NativeToWei(50))

	// Deploy minimal storage contract
	// Init code copies 17 bytes of runtime code:
	// Runtime: 600035 (load calldata) 600055 (sstore slot 0) 600054 (sload slot 0) 600052 (mstore 0) 60206000f3 (return 32 bytes)
	initBytecodeHex := "6011600c60003960116000f3" + "60003560005560005460005260206000f3"
	initBytecode, err := hex.DecodeString(initBytecodeHex)
	if err != nil {
		t.Fatalf("failed decoding bytecode: %v", err)
	}

	baseFee := big.NewInt(1_000_000_000)
	signer := types.MakeSigner(cfg, big.NewInt(1), 1700000000)

	deployTxData := &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(2_000_000_000),
		Gas:       150_000,
		To:        nil, // Contract Creation
		Value:     big.NewInt(0),
		Data:      initBytecode,
	}

	deployTx, err := types.SignTx(types.NewTx(deployTxData), signer, privKey)
	if err != nil {
		t.Fatalf("failed to sign deploy tx: %v", err)
	}

	blockCtx := BlockContext{
		Coinbase:    common.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc"),
		BlockNumber: big.NewInt(1),
		Time:        1700000000,
		BaseFee:     baseFee,
		GasLimit:    evm.BlockGasLimit,
	}

	deployRes, err := exec.ApplyTransaction(db, deployTx, blockCtx)
	if err != nil {
		t.Fatalf("Deploy transaction failed: %v", err)
	}
	if deployRes.Status != 1 {
		t.Fatalf("Deploy tx reverted: %v", deployRes.Err)
	}

	contractAddr := deployRes.ContractAddress
	if contractAddr == (common.Address{}) {
		t.Fatal("expected non-zero contract address")
	}

	// Verify contract code was saved
	runtimeCode := db.GetCode(contractAddr)
	if len(runtimeCode) != 17 {
		t.Fatalf("expected 17 bytes runtime code, got %d", len(runtimeCode))
	}

	// Call contract with value 42 (0x2a)
	calldata := common.HexToHash("0x2a").Bytes()

	callTxData := &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     1,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(2_000_000_000),
		Gas:       100_000,
		To:        &contractAddr,
		Value:     big.NewInt(0),
		Data:      calldata,
	}

	callTx, err := types.SignTx(types.NewTx(callTxData), signer, privKey)
	if err != nil {
		t.Fatalf("failed to sign call tx: %v", err)
	}

	callRes, err := exec.ApplyTransaction(db, callTx, blockCtx)
	if err != nil {
		t.Fatalf("Call transaction failed: %v", err)
	}
	if callRes.Status != 1 {
		t.Fatalf("Call tx reverted: %v", callRes.Err)
	}

	// Verify contract returned 42
	if len(callRes.ReturnData) != 32 {
		t.Fatalf("expected 32 bytes return data, got %d", len(callRes.ReturnData))
	}
	returnedVal := new(big.Int).SetBytes(callRes.ReturnData)
	if returnedVal.Uint64() != 42 {
		t.Fatalf("contract returned %d, want 42", returnedVal.Uint64())
	}

	// Verify storage slot 0 holds 42
	slotVal := db.GetState(contractAddr, common.Hash{})
	if new(big.Int).SetBytes(slotVal.Bytes()).Uint64() != 42 {
		t.Fatalf("storage slot 0 = %s, want 42", slotVal.Hex())
	}
}

// compileCheck ensures privKey ecdsa type is referenced cleanly
var _ *ecdsa.PrivateKey = nil
