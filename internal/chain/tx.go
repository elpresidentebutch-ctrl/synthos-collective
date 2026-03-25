package chain

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	MIN_FEE    = 1
	MAX_AMOUNT = 1_000_000_000_000 // 1 trillion tokens max
)

type Tx struct {
	ID        string    `json:"id"`
	ChainID   uint64    `json:"chain_id"`  // SECURITY: Prevents cross-chain replay attacks
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
		ChainID   uint64 `json:"chain_id"`
		From      Address   `json:"from"`
		To        Address   `json:"to"`
		Amount    uint64    `json:"amount"`
		Fee       uint64    `json:"fee"`
		Nonce     uint64    `json:"nonce"`
		PublicKey string    `json:"public_key"`
	}{
		ChainID:   t.ChainID,
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
// Sign signs the transaction with the given private key.
// ChainID must be set before signing to prevent cross-chain replay attacks.
func (t *Tx) Sign(priv ed25519.PrivateKey) error {
	if t.PublicKey == "" {
		return ErrInvalidTx
	}
	if t.ChainID == 0 {
		return fmt.Errorf("chain_id must be set before signing")
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
	// 1. Basic format validation
	if t.ID == "" {
		return fmt.Errorf("transaction ID missing")
	}
	if t.ChainID == 0 {
		return fmt.Errorf("chain_id missing: must be set to prevent cross-chain replay")
	}
	if t.From == "" || t.To == "" {
		return fmt.Errorf("from or to address missing")
	}
	if t.Amount == 0 {
		return fmt.Errorf("amount cannot be zero")
	}
	if t.Amount > MAX_AMOUNT {
		return fmt.Errorf("amount exceeds maximum: %d > %d", t.Amount, MAX_AMOUNT)
	}
	
	// 2. Fee validation
	if t.Fee < MIN_FEE {
		return fmt.Errorf("fee below minimum: %d < %d", t.Fee, MIN_FEE)
	}
	if t.Fee > t.Amount {
		return fmt.Errorf("fee exceeds amount: %d > %d", t.Fee, t.Amount)
	}
	
	// 3. Public key and signature validation
	if t.PublicKey == "" || t.Signature == "" {
		return fmt.Errorf("public_key or signature missing")
	}
	
	pubBytes, err := hexToBytes(t.PublicKey)
	if err != nil || len(pubBytes) != 32 {
		return fmt.Errorf("invalid public_key format: must be 32 bytes (ED25519)")
	}
	
	sigBytes, err := hexToBytes(t.Signature)
	if err != nil || len(sigBytes) != 64 {
		return fmt.Errorf("invalid signature format: must be 64 bytes (ED25519)")
	}
	
	// 4. Signature verification
	b, err := t.signingBytes()
	if err != nil {
		return fmt.Errorf("failed to compute signing bytes: %w", err)
	}
	
	if !ed25519.Verify(pubBytes, b, sigBytes) {
		return ErrBadTxSig
	}
	
	// 5. From address matches public key
	if AddressFromPublicKey(pubBytes) != t.From {
		return ErrTxFromMismatch
	}
	
	// 6. ID matches computed hash
	sum := sha256.Sum256(b)
	expectedID := "0x" + hex.EncodeToString(sum[:16])
	if t.ID != expectedID {
		return fmt.Errorf("transaction ID mismatch: got %s, want %s", t.ID, expectedID)
	}
	
	return nil
}

func hexToBytes(s string) ([]byte, error) {
	if len(s) >= 2 && s[:2] == "0x" {
		s = s[2:]
	}
	return hex.DecodeString(s)
}

