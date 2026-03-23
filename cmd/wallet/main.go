package main

import (
	"encoding/json"
	"flag"
	"os"

	"synthos-collective/internal/wallet"
)

func main() {
	showPrivate := flag.Bool("show-private", false, "print private key (DANGEROUS)")
	flag.Parse()

	w, err := wallet.New()
	if err != nil {
		panic(err)
	}
	addr, _ := w.Address()
	pub, _ := w.PublicKeyHex()
	fp, _ := w.Fingerprint()

	out := map[string]any{
		"address":     addr,
		"fingerprint": fp,
		"public_key":  pub,
	}
	if *showPrivate {
		priv, _ := w.PrivateKeyHex()
		out["private_key"] = priv
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

