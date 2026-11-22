package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// BaseMainnetAnchorContract is the verified live SynthosAnchor contract on Base mainnet.
const BaseMainnetAnchorContract = "0xae4C167ff08d13CaC9b397D13c50904bb9c881a5"

// BaseMainnetChainID is the Base L2 network ID.
const BaseMainnetChainID = 8453

// anchorMethodSelector is keccak256("anchor(uint64,bytes32,bytes32,uint64)")[:4] -> 0x491741b9
// Matches the deployed SynthosAnchor contract on Base mainnet.
const anchorMethodSelector = "491741b9"

// AnchorCommitment represents an anchored Layer 1 state commitment on Base L2.
type AnchorCommitment struct {
	BaseChainID    uint64    `json:"base_chain_id"`
	AnchorContract string    `json:"anchor_contract"`
	Height         uint64    `json:"height"`
	BlockHash      string    `json:"block_hash"`
	AppHash        string    `json:"app_hash"`
	BlockTime      uint64    `json:"block_time"`
	Fingerprint    string    `json:"fingerprint"`
	AnchoredAt     time.Time `json:"anchored_at"`
}

// ComputeFingerprint computes a deterministic cryptographic digest of (height, blockHash, appHash, blockTime).
func ComputeFingerprint(height uint64, blockHash, appHash string, blockTime uint64) string {
	payload := fmt.Sprintf("SYNTHOS_ANCHOR_V1:%d:%s:%s:%d", height, strings.ToLower(blockHash), strings.ToLower(appHash), blockTime)
	hash := sha256.Sum256([]byte(payload))
	return "0x" + hex.EncodeToString(hash[:])
}

// EncodeAnchorCalldata builds the ABI calldata for anchor(uint64,bytes32,bytes32,uint64).
func EncodeAnchorCalldata(height uint64, blockHashHex, appHashHex string, blockTime uint64) (string, error) {
	bHashClean := strings.TrimPrefix(blockHashHex, "0x")
	aHashClean := strings.TrimPrefix(appHashHex, "0x")

	if len(bHashClean) != 64 {
		return "", fmt.Errorf("blockHash must be 32 bytes (64 hex characters), got %d", len(bHashClean))
	}
	if len(aHashClean) != 64 {
		return "", fmt.Errorf("appHash must be 32 bytes (64 hex characters), got %d", len(aHashClean))
	}

	heightBig := new(big.Int).SetUint64(height)
	hWord := fmt.Sprintf("%064x", heightBig)

	timeBig := new(big.Int).SetUint64(blockTime)
	tWord := fmt.Sprintf("%064x", timeBig)

	calldata := "0x" + anchorMethodSelector + hWord + bHashClean + aHashClean + tWord
	return calldata, nil
}

// SimulateAnchorSubmission performs an eth_call against Base JSON-RPC to verify that the
// anchor transaction executes successfully before submitting.
func SimulateAnchorSubmission(evmRPCURL, contractAddr string, height uint64, blockHash, appHash string, blockTime uint64, senderAddr string) (bool, string, error) {
	calldata, err := EncodeAnchorCalldata(height, blockHash, appHash, blockTime)
	if err != nil {
		return false, "", err
	}

	txCall := map[string]string{
		"to":   contractAddr,
		"data": calldata,
	}
	if senderAddr != "" {
		txCall["from"] = senderAddr
	}

	var callResult string
	err = evmRPC(evmRPCURL, "eth_call", []any{txCall, "latest"}, &callResult)
	if err != nil {
		return false, "", fmt.Errorf("anchor simulation failed: %w", err)
	}

	fingerprint := ComputeFingerprint(height, blockHash, appHash, blockTime)
	return true, fingerprint, nil
}

// VerifyBaseNetwork confirms that the RPC endpoint points to Base (8453 or Base Sepolia 84532).
func VerifyBaseNetwork(evmRPCURL string) (uint64, error) {
	chainID, err := evmChainID(evmRPCURL)
	if err != nil {
		return 0, fmt.Errorf("failed fetching chain id: %w", err)
	}
	if chainID != BaseMainnetChainID && chainID != 84532 {
		return chainID, fmt.Errorf("connected EVM network %d is not Base (want 8453 or 84532)", chainID)
	}
	return chainID, nil
}
