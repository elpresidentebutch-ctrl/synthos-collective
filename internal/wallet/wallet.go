package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"synthos-collective/internal/chain"
)

// Wallet holds one ed25519 keypair for the SYNTHOS L1.
// (We can extend to HD derivation + multiple accounts later.)
type Wallet struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

var ErrNoKey = errors.New("wallet has no key")

func New() (*Wallet, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Wallet{Public: pub, Private: priv}, nil
}

func FromPrivateKeyHex(privHex string) (*Wallet, error) {
	if len(privHex) >= 2 && privHex[:2] == "0x" {
		privHex = privHex[2:]
	}
	b, err := hex.DecodeString(privHex)
	if err != nil {
		return nil, err
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid ed25519 private key length")
	}
	priv := ed25519.PrivateKey(b)
	pub := priv.Public().(ed25519.PublicKey)
	return &Wallet{Public: pub, Private: priv}, nil
}

func (w *Wallet) Address() (chain.Address, error) {
	if w.Public == nil {
		return "", ErrNoKey
	}
	return chain.AddressFromPublicKey(w.Public), nil
}

func (w *Wallet) PublicKeyHex() (string, error) {
	if w.Public == nil {
		return "", ErrNoKey
	}
	return "0x" + hex.EncodeToString(w.Public), nil
}

func (w *Wallet) PrivateKeyHex() (string, error) {
	if w.Private == nil {
		return "", ErrNoKey
	}
	return "0x" + hex.EncodeToString(w.Private), nil
}

func (w *Wallet) Fingerprint() (string, error) {
	if w.Public == nil {
		return "", ErrNoKey
	}
	sum := sha256.Sum256(w.Public)
	return "0x" + hex.EncodeToString(sum[:8]), nil
}

