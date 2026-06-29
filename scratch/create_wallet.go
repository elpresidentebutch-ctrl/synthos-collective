//go:build ignore

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"synthos-collective/internal/chain"
)

func main() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("Failed to generate key: %v", err)
	}

	addr := chain.AddressFromPublicKey(pub)
	pubHex := "0x" + hex.EncodeToString(pub)
	privHex := "0x" + hex.EncodeToString(priv)

	fmt.Printf("Wallet Address: %s\n", addr)
	fmt.Printf("Public Key:     %s\n", pubHex)
	fmt.Println("Private Key:    <written to scratch/USER_WALLET.local.txt>")

	walletInfo := fmt.Sprintf("Address: %s\nPublic Key: %s\nPrivate Key: %s\n", addr, pubHex, privHex)
	err = os.WriteFile("scratch/USER_WALLET.local.txt", []byte(walletInfo), 0600)
	if err != nil {
		log.Fatalf("Failed to save wallet: %v", err)
	}
}
