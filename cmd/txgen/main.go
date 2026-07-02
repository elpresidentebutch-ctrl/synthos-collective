package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/wallet"
)

func main() {
	priv := flag.String("priv", "", "ed25519 private key hex")
	to := flag.String("to", "", "recipient address")
	amount := flag.Uint64("amount", 0, "amount")
	fee := flag.Uint64("fee", 0, "fee")
	nonce := flag.Uint64("nonce", 0, "nonce")
	flag.Parse()

	if *priv == "" || *to == "" || *amount == 0 {
		fmt.Fprintln(os.Stderr, "usage: txgen --priv $SYNTHOS_PRIVATE_KEY --to 0x170D6650347ff4DaAC78B359e09C59a0e2D9758c --amount N [--fee N] [--nonce N]")
		os.Exit(2)
	}

	w, err := wallet.FromPrivateKeyHex(*priv)
	if err != nil {
		panic(err)
	}
	fromAddr, _ := w.Address()
	pubHex, _ := w.PublicKeyHex()

	tx := chain.Tx{
		From:      fromAddr,
		To:        chain.Address(*to),
		Amount:    *amount,
		Fee:       *fee,
		Nonce:     *nonce,
		PublicKey: pubHex,
	}
	if err := tx.Sign(w.Private); err != nil {
		panic(err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(tx)
}
