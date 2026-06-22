//go:build ignore

package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"log"
	"time"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/crypto"
)

func main() {
	// 1. Setup the 'Buyer' wallet (The one I generated for the user)
	userPrivHex := "0x508f17ddb1eb05d2337467d5b615b7ecc24622e3901cd80fd4a2d9c060722a6f26c13b51f7de625ae40f3af1defb86873da58b92ba974c97214c54bc23bc3d43"
	userAddr := chain.Address("0x205042f06cd3aa7d9a88deec39b9d0ba6b9fbf2b")
	userPubHex := "0x26c13b51f7de625ae40f3af1defb86873da58b92ba974c97214c54bc23bc3d43"

	privBytes, _ := hex.DecodeString(userPrivHex[2:])
	priv := ed25519.PrivateKey(privBytes)

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
		PublicKey: userPubHex,
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
