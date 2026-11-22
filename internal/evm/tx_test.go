package evm

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestEVMTxDetectionAndDecoding(t *testing.T) {
	chainID := big.NewInt(int64(DevnetEVMChainID))
	cfg := DefaultChainConfig(chainID)

	privKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed generating key: %v", err)
	}
	expectedSender := crypto.PubkeyToAddress(privKey.PublicKey)
	recipient := common.HexToAddress("0x9999999999999999999999999999999999999999")

	// Create EIP-1559 DynamicFeeTx
	txData := &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(2_000_000_000),
		Gas:       21_000,
		To:        &recipient,
		Value:     big.NewInt(1_000_000_000),
		Data:      nil,
	}

	signer := types.MakeSigner(cfg, big.NewInt(1), 1700000000)
	signedTx, err := types.SignTx(types.NewTx(txData), signer, privKey)
	if err != nil {
		t.Fatalf("failed signing tx: %v", err)
	}

	rawBytes, err := signedTx.MarshalBinary()
	if err != nil {
		t.Fatalf("failed marshaling binary: %v", err)
	}

	// 1. Raw binary detection
	if !IsEVMTx(rawBytes) {
		t.Fatal("IsEVMTx should return true for raw EIP-1559 transaction bytes")
	}

	decodedTx, err := DecodeEVMTx(rawBytes)
	if err != nil {
		t.Fatalf("DecodeEVMTx failed: %v", err)
	}
	if decodedTx.Hash() != signedTx.Hash() {
		t.Fatalf("decoded tx hash = %s, want %s", decodedTx.Hash().Hex(), signedTx.Hash().Hex())
	}

	// 2. Sender verification
	sender, err := VerifyEVMTx(decodedTx, DevnetEVMChainID, big.NewInt(1), 1700000000)
	if err != nil {
		t.Fatalf("VerifyEVMTx failed: %v", err)
	}
	if sender != expectedSender {
		t.Fatalf("recovered sender = %s, want %s", sender.Hex(), expectedSender.Hex())
	}

	// 3. Replay protection / Chain ID mismatch
	_, err = VerifyEVMTx(decodedTx, MainnetEVMChainID, big.NewInt(1), 1700000000)
	if err == nil {
		t.Fatal("expected error when validating devnet tx against mainnet chain ID")
	}

	// 4. JSON envelope detection and decoding
	jsonEnvelope := []byte(`{"type": "evm", "raw_hex": "0x` + common.Bytes2Hex(rawBytes) + `"}`)
	if !IsEVMTx(jsonEnvelope) {
		t.Fatal("IsEVMTx should return true for JSON EVM envelope")
	}

	decodedFromJSON, err := DecodeEVMTx(jsonEnvelope)
	if err != nil {
		t.Fatalf("DecodeEVMTx from JSON failed: %v", err)
	}
	if decodedFromJSON.Hash() != signedTx.Hash() {
		t.Fatalf("decoded from JSON hash = %s, want %s", decodedFromJSON.Hash().Hex(), signedTx.Hash().Hex())
	}

	// 5. Native JSON transaction should NOT be recognized as EVM
	nativeJSON := []byte(`{"id": "0x123", "from": "0xabc", "to": "0xdef", "amount": 100}`)
	if IsEVMTx(nativeJSON) {
		t.Fatal("native SYNTHOS JSON should not be detected as EVM tx")
	}
}

var _ *ecdsa.PrivateKey = nil
