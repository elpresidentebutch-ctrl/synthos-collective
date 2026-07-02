//go:build ignore

package main

import (
	"crypto/ed25519"
	"fmt"
	"log"
	"synthos-collective/internal/chain"
	"time"
)

func main() {
	// 1. Setup a deterministic dev-only buyer wallet.
	seed := []byte("synthos-dev-tax-demo-seed-00001")
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)

	userAddr := chain.Address("0xdev_tax_demo_buyer")

	// 2. Create an Escrow Lock Transaction
	// Amount: 5,400 SYN
	// 1% Mesh Tax should be: 54 SYN
	tx := chain.Tx{
		ChainID:   1, // devnet
		From:      userAddr,
		To:        chain.Address("0xescrow_agent"),
		Amount:    5400,
		Fee:       chain.MIN_FEE,
		Nonce:     0, // First tx from this wallet in genesis
		PublicKey: fmt.Sprintf("0x%x", pub),
		Timestamp: time.Now(),
		Metadata:  map[string]string{"type": "escrow_lock", "usd_price": "27.00"},
	}

	if err := tx.Sign(priv); err != nil {
		log.Fatalf("Failed to sign tx: %v", err)
	}

	fmt.Printf("🚀 Test Transaction Created!\n")
	fmt.Printf("Amount: %d SYN\n", tx.Amount)
	fmt.Printf("Expected Mesh Tax (1%%): 54 SYN\n")
	fmt.Printf("Transaction ID: %s\n", tx.ID)

	// Normally we'd submit to RPC, but we'll just output the verification for now
	fmt.Printf("\n✅ Ready for simulation. The protocol will redirect 54 SYN to your founder address upon finalization.\n")
}
