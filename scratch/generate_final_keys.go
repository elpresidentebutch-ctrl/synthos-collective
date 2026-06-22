//go:build ignore

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"crypto/sha256"
)

func main() {
	// Generate Founder Wallet
	fPub, fPriv, _ := ed25519.GenerateKey(rand.Reader)
	fSum := sha256.Sum256(fPub)
	fAddr := "0x" + hex.EncodeToString(fSum[:20])
	
	// Generate Public Wallet
	pPub, pPriv, _ := ed25519.GenerateKey(rand.Reader)
	pSum := sha256.Sum256(pPub)
	pAddr := "0x" + hex.EncodeToString(pSum[:20])

	output := fmt.Sprintf("=== THE COLLECTIVE: SOVEREIGN KEYS ===\n\n")
	output += fmt.Sprintf("1. FOUNDER MASTER WALLET (PRIVATE - OWNERSHIP)\n")
	output += fmt.Sprintf("Address:     %s\n", fAddr)
	output += fmt.Sprintf("Public Key:  0x%s\n", hex.EncodeToString(fPub))
	output += fmt.Sprintf("Private Key: 0x%s\n\n", hex.EncodeToString(fPriv))
	
	output += fmt.Sprintf("2. COMMUNITY PUBLIC WALLET (PUBLIC - ONBOARDING)\n")
	output += fmt.Sprintf("Address:     %s\n", pAddr)
	output += fmt.Sprintf("Public Key:  0x%s\n", hex.EncodeToString(pPub))
	output += fmt.Sprintf("Private Key: 0x%s\n", hex.EncodeToString(pPriv))

	err := os.WriteFile("SOVEREIGN_KEYS_FINAL.txt", []byte(output), 0600)
	if err != nil {
		log.Fatal(err)
	}
	
	fmt.Println("Generated SOVEREIGN_KEYS_FINAL.txt")
}
