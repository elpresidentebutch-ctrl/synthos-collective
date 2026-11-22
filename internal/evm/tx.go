package evm

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// EVMTxEnvelope wraps an RLP-encoded Ethereum transaction for JSON transport.
type EVMTxEnvelope struct {
	Type   string `json:"type"`              // Must be "evm" or "0x02"
	RawHex string `json:"raw_hex,omitempty"` // Hex-encoded RLP bytes (with or without 0x)
	Raw    []byte `json:"raw,omitempty"`     // Raw binary RLP bytes
}

// IsEVMTx checks if the incoming raw transaction bytes represent an Ethereum transaction.
func IsEVMTx(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	// Check for standard EIP-2718 typed transaction envelopes (0x01 = EIP-2930, 0x02 = EIP-1559, 0x03 = Blob, 0x04 = SetCode)
	if raw[0] == 0x01 || raw[0] == 0x02 || raw[0] == 0x03 || raw[0] == 0x04 {
		return true
	}
	// Check for legacy RLP list (0xc0 - 0xfe)
	if raw[0] >= 0xc0 && raw[0] <= 0xfe {
		return true
	}
	// Check for JSON EVMTxEnvelope
	if raw[0] == '{' {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &probe); err == nil {
			return strings.EqualFold(probe.Type, "evm") || strings.EqualFold(probe.Type, "0x02")
		}
	}
	return false
}

// DecodeEVMTx decodes raw bytes into a *types.Transaction.
func DecodeEVMTx(raw []byte) (*types.Transaction, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty transaction bytes")
	}

	payload := raw

	// Handle JSON envelope
	if raw[0] == '{' {
		var env EVMTxEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("invalid EVM JSON envelope: %w", err)
		}
		if len(env.Raw) > 0 {
			payload = env.Raw
		} else if env.RawHex != "" {
			cleanHex := strings.TrimPrefix(env.RawHex, "0x")
			decoded, err := hex.DecodeString(cleanHex)
			if err != nil {
				return nil, fmt.Errorf("invalid hex in EVM envelope: %w", err)
			}
			payload = decoded
		} else {
			return nil, errors.New("EVM JSON envelope missing raw or raw_hex payload")
		}
	}

	var tx types.Transaction
	if err := tx.UnmarshalBinary(payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal EVM transaction: %w", err)
	}

	return &tx, nil
}

// VerifyEVMTx verifies that the EVM transaction has a valid signature and matches the expected chain ID.
func VerifyEVMTx(tx *types.Transaction, expectedChainID uint64, blockNumber *big.Int, blockTime uint64) (common.Address, error) {
	if tx == nil {
		return common.Address{}, errors.New("nil transaction")
	}

	cfg := DefaultChainConfig(new(big.Int).SetUint64(expectedChainID))
	signer := types.MakeSigner(cfg, blockNumber, blockTime)

	// Verify chain ID
	if txChainID := tx.ChainId(); txChainID != nil && txChainID.Sign() != 0 {
		if txChainID.Uint64() != expectedChainID {
			return common.Address{}, fmt.Errorf("wrong EVM chain ID: got %d, want %d", txChainID.Uint64(), expectedChainID)
		}
	}

	sender, err := types.Sender(signer, tx)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to recover sender: %w", err)
	}

	return sender, nil
}
