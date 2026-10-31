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

type WalletEntry struct {
	Address    string `json:"address"`
	PrivateKey string `json:"privateKey"`
	Role       string `json:"role"`
}

type WalletsFile struct {
	Wallets []WalletEntry `json:"wallets"`
}

func main() {
	var amount uint64
	var toAddr string
	var nonce uint64
	flag.Uint64Var(&amount, "amount", 1000000, "Amount of SYN to fund")
	flag.StringVar(&toAddr, "to", "0x2741c58ae51618177e9cce2bf20fea166909261c", "Destination rewards wallet address")
	flag.Uint64Var(&nonce, "nonce", 0, "Account nonce (defaults to 0)")
	flag.Parse()

	data, err := os.ReadFile(`C:\Users\elpre\Desktop\CC KEYS\ALL_WALLETS.json`)
	if err != nil {
		fmt.Printf("Error reading ALL_WALLETS.json: %v\n", err)
		return
	}

	var wf WalletsFile
	if err := json.Unmarshal(data, &wf); err != nil {
		fmt.Printf("Error parsing ALL_WALLETS.json: %v\n", err)
		return
	}

	var founderEntry *WalletEntry
	for i := range wf.Wallets {
		if strings.EqualFold(wf.Wallets[i].Address, "0x825fd94aa826da6ce0b4e57487418b72aea09f5e") {
			founderEntry = &wf.Wallets[i]
			break
		}
	}

	if founderEntry == nil {
		fmt.Println("Founder wallet not found in ALL_WALLETS.json")
		return
	}

	privHex := strings.TrimPrefix(founderEntry.PrivateKey, "0x")
	privBytes, err := hex.DecodeString(privHex)
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
		fmt.Printf("Unexpected private key byte length: %d\n", len(privBytes))
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
			{Key: "amount_syn", Value: fmt.Sprint(amount)},
		},
	}

	if err := tx.Sign(edPriv); err != nil {
		fmt.Printf("Error signing tx: %v\n", err)
		return
	}

	txJSON, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling tx: %v\n", err)
		return
	}

	fmt.Printf("%s\n", string(txJSON))
}
