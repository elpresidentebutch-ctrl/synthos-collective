package chain

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type Tx struct {
	ID        string    `json:"id"`
	From      Address   `json:"from"`
	To        Address   `json:"to"`
	Amount    uint64    `json:"amount"`
	Fee       uint64    `json:"fee"`
	Nonce     uint64    `json:"nonce"`
	// Timestamp is non-consensus metadata. It is NOT signed or hashed for tx identity.
	// The SYNTHOS L1 runtime is "timeless": only genesis carries a timestamp.
	Timestamp time.Time `json:"timestamp,omitempty"`

	// PublicKey is required for signature verification at the ledger layer.
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
}

var (
	ErrInvalidTx     = errors.New("invalid transaction")
	ErrBadTxSig      = errors.New("bad transaction signature")
	ErrTxFromMismatch = errors.New("from address does not match public key")
)

func (t Tx) signingBytes() ([]byte, error) {
	tmp := struct {
		From      Address   `json:"from"`
		To        Address   `json:"to"`
		Amount    uint64    `json:"amount"`
		Fee       uint64    `json:"fee"`
		Nonce     uint64    `json:"nonce"`
		PublicKey string    `json:"public_key"`
	}{
		From:      t.From,
		To:        t.To,
		Amount:    t.Amount,
		Fee:       t.Fee,
		Nonce:     t.Nonce,
		PublicKey: t.PublicKey,
	}
	return json.Marshal(tmp)
}

func (t *Tx) ComputeID() error {
	b, err := t.signingBytes()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	t.ID = "0x" + hex.EncodeToString(sum[:16])
	return nil
}

// Sign signs the transaction using ed25519 and sets Signature + ID.
// PublicKey must be set.
func (t *Tx) Sign(priv ed25519.PrivateKey) error {
	if t.PublicKey == "" {
		return ErrInvalidTx
	}
	// Ensure From matches PublicKey.
	pubBytes, err := hexToBytes(t.PublicKey)
	if err != nil {
		return err
	}
	if AddressFromPublicKey(pubBytes) != t.From {
		return ErrTxFromMismatch
	}

	b, err := t.signingBytes()
	if err != nil {
		return err
	}
	sig := ed25519.Sign(priv, b)
	t.Signature = "0x" + hex.EncodeToString(sig)
	return t.ComputeID()
}

func (t Tx) Verify() error {
	if t.From == "" || t.To == "" || t.Amount == 0 {
		return ErrInvalidTx
	}
	pubBytes, err := hexToBytes(t.PublicKey)
	if err != nil {
		return ErrInvalidTx
	}
	if AddressFromPublicKey(pubBytes) != t.From {
		return ErrTxFromMismatch
	}
	sigBytes, err := hexToBytes(t.Signature)
	if err != nil {
		return ErrInvalidTx
	}
	b, err := t.signingBytes()
	if err != nil {
		return ErrInvalidTx
	}
	if !ed25519.Verify(pubBytes, b, sigBytes) {
		return ErrBadTxSig
	}
	// Optional: verify ID matches bytes.
	return nil
}

func hexToBytes(s string) ([]byte, error) {
	if len(s) >= 2 && s[:2] == "0x" {
		s = s[2:]
	}
	return hex.DecodeString(s)
}

