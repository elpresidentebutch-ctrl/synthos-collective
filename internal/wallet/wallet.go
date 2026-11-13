package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"synthos-collective/internal/chain"
)

// Wallet holds one ed25519 keypair for the SYNTHOS L1.
type Wallet struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

var (
	ErrNoKey             = errors.New("wallet has no key")
	ErrInvalidPrivateKey = errors.New("invalid ed25519 private key length: expected 32 bytes (seed) or 64 bytes (full key)")
)

// New generates a new random Ed25519 wallet keypair.
func New() (*Wallet, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Wallet{Public: pub, Private: priv}, nil
}

// FromSeed creates a wallet from a 32-byte seed.
func FromSeed(seed []byte) (*Wallet, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("invalid seed length: %d bytes (expected 32)", len(seed))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return &Wallet{Public: pub, Private: priv}, nil
}

// FromPrivateKeyHex parses a hex-encoded private key (either 64-byte full key or 32-byte seed).
func FromPrivateKeyHex(privHex string) (*Wallet, error) {
	privHex = strings.TrimPrefix(strings.TrimSpace(privHex), "0x")
	b, err := hex.DecodeString(privHex)
	if err != nil {
		return nil, fmt.Errorf("decode hex: %w", err)
	}

	if len(b) == ed25519.SeedSize {
		return FromSeed(b)
	}
	if len(b) == ed25519.PrivateKeySize {
		priv := ed25519.PrivateKey(b)
		pub := priv.Public().(ed25519.PublicKey)
		return &Wallet{Public: pub, Private: priv}, nil
	}

	return nil, ErrInvalidPrivateKey
}

// Address returns the 20-byte deterministic SYNTHOS L1 address (0x + sha256(pub)[:20]).
func (w *Wallet) Address() (chain.Address, error) {
	if w.Public == nil {
		return "", ErrNoKey
	}
	return chain.AddressFromPublicKey(w.Public), nil
}

// PublicKeyHex returns the 0x-prefixed hex representation of the public key (32 bytes).
func (w *Wallet) PublicKeyHex() (string, error) {
	if w.Public == nil {
		return "", ErrNoKey
	}
	return "0x" + hex.EncodeToString(w.Public), nil
}

// PrivateKeyHex returns the 0x-prefixed hex representation of the full private key (64 bytes).
func (w *Wallet) PrivateKeyHex() (string, error) {
	if w.Private == nil {
		return "", ErrNoKey
	}
	return "0x" + hex.EncodeToString(w.Private), nil
}

// SeedHex returns the 0x-prefixed hex representation of the 32-byte seed.
func (w *Wallet) SeedHex() (string, error) {
	if w.Private == nil {
		return "", ErrNoKey
	}
	return "0x" + hex.EncodeToString(w.Private.Seed()), nil
}

// Fingerprint returns an 8-byte hash fingerprint of the public key.
func (w *Wallet) Fingerprint() (string, error) {
	if w.Public == nil {
		return "", ErrNoKey
	}
	sum := sha256.Sum256(w.Public)
	return "0x" + hex.EncodeToString(sum[:8]), nil
}

// Sign signs an arbitrary byte payload using the wallet's private key.
func (w *Wallet) Sign(message []byte) ([]byte, error) {
	if w.Private == nil {
		return nil, ErrNoKey
	}
	return ed25519.Sign(w.Private, message), nil
}

// SignTx sets From, PublicKey, and signs the transaction.
func (w *Wallet) SignTx(tx *chain.Tx) error {
	if w.Private == nil {
		return ErrNoKey
	}
	addr, err := w.Address()
	if err != nil {
		return err
	}
	pubHex, err := w.PublicKeyHex()
	if err != nil {
		return err
	}
	tx.From = addr
	tx.PublicKey = pubHex
	return tx.Sign(w.Private)
}

// CreateTransferTx constructs and signs a native coin transfer transaction.
func (w *Wallet) CreateTransferTx(to chain.Address, amount uint64, fee uint64, nonce uint64, chainID uint64) (*chain.Tx, error) {
	addr, err := w.Address()
	if err != nil {
		return nil, err
	}
	pubHex, err := w.PublicKeyHex()
	if err != nil {
		return nil, err
	}

	tx := &chain.Tx{
		ChainID:   chainID,
		From:      addr,
		To:        to,
		Amount:    amount,
		Fee:       fee,
		Nonce:     nonce,
		PublicKey: pubHex,
	}

	if err := tx.Sign(w.Private); err != nil {
		return nil, fmt.Errorf("sign transaction: %w", err)
	}
	return tx, nil
}
