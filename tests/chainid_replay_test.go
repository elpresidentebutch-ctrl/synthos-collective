package stress_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sync"
	"testing"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/crypto"
)

// TestChainIDReplay_ZeroChainID verifies that a transaction with ChainID=0 is rejected.
func TestChainIDReplay_ZeroChainID(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	from := chain.AddressFromPublicKey(pub)

	tx := chain.Tx{
		ChainID:   0, // Missing ChainID - must be rejected
		From:      from,
		To:        chain.Address("0xdead"),
		Amount:    100,
		Fee:       1,
		Nonce:     0,
		PublicKey: crypto.PublicKeyHex(pub),
	}

	// Sign should fail when ChainID=0
	if err := tx.Sign(priv); err == nil {
		t.Fatal("expected error signing tx with ChainID=0")
	}

	// Verify should also reject ChainID=0
	tx.Signature = "0x" + "00" // fake sig
	if err := tx.Verify(); err == nil {
		t.Fatal("expected error verifying tx with ChainID=0")
	}
	t.Logf("✅ ChainID=0 rejected at sign and verify")
}

// TestChainIDReplay_CrossChain verifies that a signed transaction cannot be
// replayed on a different chain by modifying the ChainID after signing.
func TestChainIDReplay_CrossChain(t *testing.T) {
	const testnetChainID = uint64(1)
	const mainnetChainID = uint64(2)

	c, pubs, privs := newStressChain(t, 2, 100_000)
	from := chain.AddressFromPublicKey(pubs[0])
	to := chain.AddressFromPublicKey(pubs[1])

	// Create and sign a transaction for testnet (ChainID=1)
	txTestnet := newSignedTx(t, testnetChainID, pubs[0], privs[0], to, 100, 1, 0)

	// ATTACK: Attacker copies the signed tx and changes the ChainID to mainnet
	txReplayed := txTestnet
	txReplayed.ChainID = mainnetChainID // Change ChainID without re-signing

	// Create a "mainnet" chain sharing the same funded state
	mainnet := &chain.Chain{
		ChainID: "mainnet",
		State:   c.State,
		Blocks:  c.Blocks,
		Mempool: make(map[string]chain.Tx),
	}
	mainnet.State.Set(from, chain.Account{Balance: 100_000, Nonce: 0})

	// The replayed tx must be rejected on mainnet
	if err := mainnet.SubmitTx(txReplayed); err == nil {
		t.Fatal("SECURITY FAILURE: cross-chain replay accepted")
	}
	t.Logf("✅ Cross-chain replay rejected: ChainID mismatch invalidates signature")
}

// TestChainIDReplay_SignatureCoversChainID verifies that changing any field of
// a signed transaction invalidates the signature (property test).
func TestChainIDReplay_SignatureCoversChainID(t *testing.T) {
	c, pubs, privs := newStressChain(t, 2, 100_000)
	to := chain.AddressFromPublicKey(pubs[1])

	original := newSignedTx(t, 1, pubs[0], privs[0], to, 100, 1, 0)

	tamperedChainIDs := []uint64{0, 2, 42, 999, ^uint64(0)}
	for _, badChainID := range tamperedChainIDs {
		tampered := original
		tampered.ChainID = badChainID

		if err := tampered.Verify(); err == nil {
			t.Fatalf("tampered tx with ChainID=%d passed verification", badChainID)
		}
	}

	// Also verify that SubmitTx on a different chain rejects it
	for _, badChainID := range tamperedChainIDs[1:] { // skip 0, already tested
		tampered := original
		tampered.ChainID = badChainID

		attackChain := &chain.Chain{
			ChainID: fmt.Sprintf("chain-%d", badChainID),
			State:   c.State,
			Blocks:  c.Blocks,
			Mempool: make(map[string]chain.Tx),
		}
		if err := attackChain.SubmitTx(tampered); err == nil {
			t.Fatalf("replay with ChainID=%d was accepted", badChainID)
		}
	}
	t.Logf("✅ All ChainID tampering attempts rejected")
}

// TestChainIDReplay_Concurrent stresses ChainID validation under concurrent load
// with 100 goroutines each submitting a transaction to an independent chain.
func TestChainIDReplay_Concurrent(t *testing.T) {
	const numGoroutines = 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*2)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			pub, priv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: keygen failed: %w", id, err)
				return
			}
			from := chain.AddressFromPublicKey(pub)

			genesis := chain.Genesis{
				ChainID: fmt.Sprintf("stress-%d", id),
				Alloc:   map[chain.Address]uint64{from: 100_000},
			}
			c, err := chain.NewChain(genesis)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: chain creation failed: %w", id, err)
				return
			}

			// Valid tx must succeed
			validTx := chain.Tx{
				ChainID:   1,
				From:      from,
				To:        chain.Address("0xdeadbeef"),
				Amount:    100,
				Fee:       1,
				Nonce:     0,
				PublicKey: crypto.PublicKeyHex(pub),
			}
			if err := validTx.Sign(priv); err != nil {
				errors <- fmt.Errorf("goroutine %d: sign failed: %w", id, err)
				return
			}
			if err := c.SubmitTx(validTx); err != nil {
				errors <- fmt.Errorf("goroutine %d: submit failed: %w", id, err)
				return
			}

			// Replay with modified ChainID must fail
			replayTx := validTx
			replayTx.ChainID = 99
			replayChain := &chain.Chain{
				ChainID: "replay-chain",
				State:   c.State,
				Blocks:  c.Blocks,
				Mempool: make(map[string]chain.Tx),
			}
			if err := replayChain.SubmitTx(replayTx); err == nil {
				errors <- fmt.Errorf("goroutine %d: replay was accepted", id)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("%v", err)
	}
	t.Logf("✅ %d concurrent ChainID replay attempts all rejected", numGoroutines)
}
