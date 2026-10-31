package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"synthos-collective/internal/chain"
)

// DefaultValidatorRewardsPool is the official Validator / Security Rewards genesis allocation (12 Billion SYN).
const DefaultValidatorRewardsPool = "0x83f91f1fbef8b0a0d995a7fe3cedbbab6b8e3ed9"

// DefaultHotWalletAddress is the live automated rewards keeper hot wallet on Hosteons.
const DefaultHotWalletAddress = "0x2741c58ae51618177e9cce2bf20fea166909261c"

func main() {
	var amount uint64
	var toAddr string
	var nonce uint64
	var keyHex string
	var keyFile string

	flag.Uint64Var(&amount, "amount", 1000000, "Amount of SYN to fund into the hot wallet")
	flag.StringVar(&toAddr, "to", DefaultHotWalletAddress, "Destination rewards keeper hot wallet address")
	flag.Uint64Var(&nonce, "nonce", 0, "Account nonce of the funding wallet")
	flag.StringVar(&keyHex, "key", "", "Hex private key for the source pool (or set SYNTHOS_SOURCE_PRIVATE_KEY)")
	flag.StringVar(&keyFile, "key-file", "", "Path to a single private key file (e.g. offline USB drive)")
	flag.Parse()

	// 1. Resolve private key from CLI flag, env var, or offline key file
	if keyHex == "" {
		keyHex = os.Getenv("SYNTHOS_SOURCE_PRIVATE_KEY")
	}
	if keyHex == "" && keyFile != "" {
		data, err := os.ReadFile(keyFile)
		if err != nil {
			fmt.Printf("Error reading key file %s: %v\n", keyFile, err)
			return
		}
		keyHex = strings.TrimSpace(string(data))
	}

	if keyHex == "" {
		fmt.Println("Error: No source private key provided.")
		fmt.Println("\nUsage for future top-ups (using the 12B Validator / Security Rewards pool):")
		fmt.Println("  go run ./cmd/fund-rewards -amount 1000000 -key <HEX_PRIVATE_KEY>")
		fmt.Println("  go run ./cmd/fund-rewards -amount 1000000 -key-file <OFFLINE_KEY_FILE>")
		fmt.Println("  $env:SYNTHOS_SOURCE_PRIVATE_KEY='...'; go run ./cmd/fund-rewards -amount 1000000")
		fmt.Printf("\nTarget Pool: %s (Validator / Security Rewards)\n", DefaultValidatorRewardsPool)
		return
	}

	keyHex = strings.TrimPrefix(strings.TrimSpace(keyHex), "0x")
	privBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		fmt.Printf("Error decoding private key hex: %v\n", err)
		return
	}

	var edPriv ed25519.PrivateKey
	if len(privBytes) == ed25519.PrivateKeySize {
		edPriv = ed25519.PrivateKey(privBytes)
	} else if len(privBytes) == ed25519.SeedSize {
		edPriv = ed25519.NewKeyFromSeed(privBytes)
	} else {
		fmt.Printf("Unexpected private key byte length: %d (expected 32-byte seed or 64-byte key)\n", len(privBytes))
		return
	}

	pubKey := edPriv.Public().(ed25519.PublicKey)
	derivedAddr := chain.AddressFromPublicKey(pubKey)

	tx := chain.Tx{
		ChainID:   20260702,
		From:      derivedAddr,
		To:        chain.Address(strings.ToLower(toAddr)),
		Amount:    amount,
		Fee:       chain.MIN_FEE,
		Nonce:     nonce,
		PublicKey: hex.EncodeToString(pubKey),
		Metadata: []chain.KeyValuePair{
			{Key: "purpose", Value: "fund_rewards_hot_wallet"},
			{Key: "source_pool", Value: "Validator / Security Rewards"},
			{Key: "amount_syn", Value: fmt.Sprint(amount)},
		},
	}

	if err := tx.Sign(edPriv); err != nil {
		fmt.Printf("Error signing transaction: %v\n", err)
		return
	}

	txJSON, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling tx: %v\n", err)
		return
	}

	fmt.Printf("// Signed Funding Transaction (from %s to %s):\n", derivedAddr, toAddr)
	fmt.Printf("%s\n", string(txJSON))
}
