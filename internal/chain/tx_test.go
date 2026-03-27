package chain

import (
	"crypto/ed25519"
	"testing"

	"synthos-collective/internal/crypto"
)

func TestAddressFromPublicKey_Deterministic(t *testing.T) {
	pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
	for i := range pub {
		pub[i] = byte(i)
	}
	a1 := AddressFromPublicKey(pub)
	a2 := AddressFromPublicKey(pub)
	if a1 != a2 {
		t.Fatalf("address not deterministic: %q vs %q", a1, a2)
	}
	if len(string(a1)) < 42 { // 0x + 40 hex
		t.Fatalf("unexpected address format: %q", a1)
	}
}

func TestTx_SignVerifyRoundTrip(t *testing.T) {
	kp, err := crypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pubHex := crypto.PublicKeyHex(kp.Public)
	from := AddressFromPublicKey(kp.Public)

	tx := Tx{
		ChainID:   1,
		From:      from,
		To:        Address("0x" + "11"),
		Amount:    100,
		Fee:       1,
		Nonce:     0,
		PublicKey: pubHex,
	}
	if err := tx.Sign(kp.Private); err != nil {
		t.Fatal(err)
	}
	if tx.ID == "" || tx.Signature == "" {
		t.Fatal("expected ID and signature")
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestTx_Verify_FromMismatch(t *testing.T) {
	kp, err := crypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	other, err := crypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	tx := Tx{
		ChainID:   1,
		From:      AddressFromPublicKey(other.Public),
		To:        Address("0x22"),
		Amount:    1,
		Fee:       0,
		Nonce:     0,
		PublicKey: crypto.PublicKeyHex(kp.Public),
	}
	if err := tx.Sign(kp.Private); err != ErrTxFromMismatch {
		t.Fatalf("expected ErrTxFromMismatch, got %v", err)
	}
}

func TestTx_Verify_Invalid(t *testing.T) {
	tx := Tx{From: "0x1", To: "0x2", Amount: 0}
	if err := tx.Verify(); err == nil {
		t.Fatal("expected error for zero amount")
	}
}
