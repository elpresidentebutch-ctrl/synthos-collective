package memory

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func testVault(t *testing.T) (*Vault, ed25519.PublicKey) {
	t.Helper()
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	vault, err := NewVault("agent-citizen-1", key, private)
	if err != nil {
		t.Fatal(err)
	}
	return vault, pub
}

func TestVaultEncryptsSignsAndChainsMemory(t *testing.T) {
	vault, pub := testVault(t)
	first, err := vault.Append([]byte("private reflection"), []byte("owner-only"), 10)
	if err != nil {
		t.Fatal(err)
	}
	second, err := vault.Append([]byte("new experience"), []byte("share-by-consent"), 11)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(first.Ciphertext, []byte("private reflection")) {
		t.Fatal("plaintext leaked into ciphertext")
	}
	if second.PreviousCommitment != first.Commitment {
		t.Fatal("memory chain did not link to previous commitment")
	}
	if err := VerifyChain(vault.Records(), pub); err != nil {
		t.Fatal(err)
	}
	plaintext, err := vault.Decrypt(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "private reflection" {
		t.Fatalf("unexpected plaintext: %q", plaintext)
	}
}

func TestVaultDetectsTampering(t *testing.T) {
	vault, pub := testVault(t)
	env, err := vault.Append([]byte("unaltered"), []byte("owner-only"), 1)
	if err != nil {
		t.Fatal(err)
	}
	env.Ciphertext[0] ^= 0xff
	if err := VerifyEnvelope(env, pub); err == nil {
		t.Fatal("expected tampering to invalidate commitment")
	}
}
