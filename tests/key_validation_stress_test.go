package stress_test

import (
	"fmt"
	"sync"
	"testing"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/crypto"
)

// TestKeyValidation_NilPrivateKey verifies AttachKeys rejects nil private key.
func TestKeyValidation_NilPrivateKey(t *testing.T) {
	kp, err := crypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent("agent-1", crypto.PublicKeyHex(kp.Public), "", "hw-1", 100)

	badKP := crypto.KeyPair{
		Public:  kp.Public,
		Private: nil, // nil private key
	}
	if err := a.AttachKeys(badKP); err == nil {
		t.Fatal("expected error for nil private key")
	}
	t.Logf("✅ Nil private key rejected")
}

// TestKeyValidation_NilPublicKey verifies AttachKeys rejects nil public key.
func TestKeyValidation_NilPublicKey(t *testing.T) {
	kp, err := crypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent("agent-2", crypto.PublicKeyHex(kp.Public), "", "hw-2", 100)

	badKP := crypto.KeyPair{
		Public:  nil, // nil public key
		Private: kp.Private,
	}
	if err := a.AttachKeys(badKP); err == nil {
		t.Fatal("expected error for nil public key")
	}
	t.Logf("✅ Nil public key rejected")
}

// TestKeyValidation_ShortPrivateKey verifies that private keys shorter than
// 64 bytes (Go's ed25519.PrivateKey = seed + public key) are rejected.
func TestKeyValidation_ShortPrivateKey(t *testing.T) {
	kp, err := crypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent("agent-3", crypto.PublicKeyHex(kp.Public), "", "hw-3", 100)

	badKP := crypto.KeyPair{
		Public:  kp.Public,
		Private: kp.Private[:16], // truncated to 16 bytes (wrong size)
	}
	if err := a.AttachKeys(badKP); err == nil {
		t.Fatal("expected error for short (16-byte) private key")
	}
	t.Logf("✅ Short private key (16 bytes) rejected")
}

// TestKeyValidation_ShortPublicKey verifies that public keys shorter than
// 32 bytes (ED25519 requirement) are rejected.
func TestKeyValidation_ShortPublicKey(t *testing.T) {
	kp, err := crypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent("agent-4", crypto.PublicKeyHex(kp.Public), "", "hw-4", 100)

	badKP := crypto.KeyPair{
		Public:  kp.Public[:16], // truncated to 16 bytes (wrong size)
		Private: kp.Private,
	}
	if err := a.AttachKeys(badKP); err == nil {
		t.Fatal("expected error for short (16-byte) public key")
	}
	t.Logf("✅ Short public key (16 bytes) rejected")
}

// TestKeyValidation_ValidED25519Keys verifies that correctly-sized ED25519 keys
// (32-byte public, 64-byte private in Go's representation) are accepted.
func TestKeyValidation_ValidED25519Keys(t *testing.T) {
	kp, err := crypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent("agent-valid", crypto.PublicKeyHex(kp.Public), "", "hw-valid", 100)

	if err := a.AttachKeys(kp); err != nil {
		t.Fatalf("valid ED25519 keys rejected: %v", err)
	}
	t.Logf("✅ Valid ED25519 key pair accepted")
}

// TestKeyValidation_EmptyKeys verifies that zero-length keys are rejected.
func TestKeyValidation_EmptyKeys(t *testing.T) {
	kp, err := crypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent("agent-empty", crypto.PublicKeyHex(kp.Public), "", "hw-empty", 100)

	emptyKP := crypto.KeyPair{
		Public:  []byte{},
		Private: []byte{},
	}
	if err := a.AttachKeys(emptyKP); err == nil {
		t.Fatal("expected error for empty key pair")
	}
	t.Logf("✅ Empty keys rejected")
}

// TestKeyValidation_ConcurrentAgentCreations stress tests creating 100 agents
// concurrently with valid and invalid keys to detect race conditions.
func TestKeyValidation_ConcurrentAgentCreations(t *testing.T) {
	const numAgents = 100

	var wg sync.WaitGroup
	errors := make(chan error, numAgents)

	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			kp, err := crypto.NewKeyPair()
			if err != nil {
				errors <- fmt.Errorf("agent %d: keygen failed: %w", id, err)
				return
			}

			a := agent.NewAgent(
				fmt.Sprintf("agent-%d", id),
				crypto.PublicKeyHex(kp.Public),
				crypto.PrivateKeyHashHex(kp.Private),
				fmt.Sprintf("hw-%d", id),
				100,
			)

			if err := a.AttachKeys(kp); err != nil {
				errors <- fmt.Errorf("agent %d: AttachKeys failed: %w", id, err)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("%v", err)
	}
	t.Logf("✅ %d concurrent agent creations with valid keys all succeeded", numAgents)
}

// TestKeyValidation_ConcurrentInvalidKeys stress tests concurrent AttachKeys
// calls with invalid keys to ensure no panics or data corruption occurs.
func TestKeyValidation_ConcurrentInvalidKeys(t *testing.T) {
	const numGoroutines = 100

	kp, err := crypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent("shared-agent", crypto.PublicKeyHex(kp.Public), "", "hw-shared", 100)

	// Pre-attach valid keys
	if err := a.AttachKeys(kp); err != nil {
		t.Fatalf("initial AttachKeys failed: %v", err)
	}

	var wg sync.WaitGroup
	panicDetected := make(chan struct{}, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer func() {
				if r := recover(); r != nil {
					panicDetected <- struct{}{}
				}
				wg.Done()
			}()

			// Alternate between valid and invalid key pairs
			if id%2 == 0 {
				newKP, _ := crypto.NewKeyPair()
				_ = a.AttachKeys(newKP) // Valid keys - should succeed
			} else {
				badKP := crypto.KeyPair{
					Public:  kp.Public[:10], // invalid length
					Private: kp.Private,
				}
				_ = a.AttachKeys(badKP) // Invalid keys - should return error
			}
		}(i)
	}

	wg.Wait()
	close(panicDetected)

	if len(panicDetected) > 0 {
		t.Fatal("panic detected during concurrent key attachment")
	}
	t.Logf("✅ %d concurrent key attachment attempts (mix of valid/invalid) without panic", numGoroutines)
}
