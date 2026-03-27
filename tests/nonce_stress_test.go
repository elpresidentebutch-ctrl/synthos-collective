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

// TestNonce_SequentialProgression validates that sequential nonces are accepted
// and that each nonce is only accepted once after the previous tx is finalized.
func TestNonce_SequentialProgression(t *testing.T) {
	const numTxs = 20
	const initialBalance = 100_000

	c, pubs, privs := newStressChain(t, 2, initialBalance)
	to := chain.AddressFromPublicKey(pubs[1])

	for i := uint64(0); i < numTxs; i++ {
		tx := newSignedTx(t, 1, pubs[0], privs[0], to, 10, 1, i)
		if err := c.SubmitTx(tx); err != nil {
			t.Fatalf("nonce=%d: failed to submit tx: %v", i, err)
		}
		// Finalize after each tx to advance the state nonce
		buildAndFinalizeBlock(t, c)
	}
	t.Logf("✅ %d sequential nonce transactions accepted", numTxs)
}

// TestNonce_GapRejection verifies that a nonce gap is rejected at the mempool layer.
// Submitting nonce=2 when the expected nonce is 0 must fail.
func TestNonce_GapRejection(t *testing.T) {
	c, pubs, privs := newStressChain(t, 2, 100_000)
	to := chain.AddressFromPublicKey(pubs[1])

	// First, submit nonce=0 and finalize to advance state to nonce=1
	tx0 := newSignedTx(t, 1, pubs[0], privs[0], to, 10, 1, 0)
	if err := c.SubmitTx(tx0); err != nil {
		t.Fatalf("nonce=0 submit failed: %v", err)
	}
	buildAndFinalizeBlock(t, c)

	// State nonce is now 1. Skipping to nonce=3 must be rejected.
	tx3 := newSignedTx(t, 1, pubs[0], privs[0], to, 10, 1, 3)
	if err := c.SubmitTx(tx3); err == nil {
		t.Fatal("SECURITY FAILURE: nonce gap (expected 1, got 3) was accepted")
	}

	// nonce=1 must succeed
	tx1 := newSignedTx(t, 1, pubs[0], privs[0], to, 10, 1, 1)
	if err := c.SubmitTx(tx1); err != nil {
		t.Fatalf("nonce=1 submit failed: %v", err)
	}
	t.Logf("✅ Nonce gap rejected; correct sequential nonce accepted")
}

// TestNonce_DoubleSpendPrevention verifies that submitting the same transaction
// twice is rejected (duplicate tx ID in mempool).
func TestNonce_DoubleSpendPrevention(t *testing.T) {
	c, pubs, privs := newStressChain(t, 2, 100_000)
	to := chain.AddressFromPublicKey(pubs[1])

	tx := newSignedTx(t, 1, pubs[0], privs[0], to, 100, 1, 0)

	// First submission succeeds
	if err := c.SubmitTx(tx); err != nil {
		t.Fatalf("first submit failed: %v", err)
	}

	// Second submission with the same tx must be rejected
	if err := c.SubmitTx(tx); err == nil {
		t.Fatal("SECURITY FAILURE: duplicate tx (same ID) accepted into mempool")
	}
	t.Logf("✅ Double-spend via duplicate tx ID rejected")
}

// TestNonce_WrongNonceRejection verifies several invalid nonce scenarios.
func TestNonce_WrongNonceRejection(t *testing.T) {
	c, pubs, privs := newStressChain(t, 2, 100_000)
	to := chain.AddressFromPublicKey(pubs[1])

	// State nonce is 0; submit nonce=1 must fail immediately
	txBad := newSignedTx(t, 1, pubs[0], privs[0], to, 10, 1, 1)
	if err := c.SubmitTx(txBad); err == nil {
		t.Fatal("SECURITY FAILURE: nonce=1 accepted when state nonce=0")
	}

	// Submit correct nonce=0
	tx0 := newSignedTx(t, 1, pubs[0], privs[0], to, 10, 1, 0)
	if err := c.SubmitTx(tx0); err != nil {
		t.Fatalf("nonce=0 submit failed: %v", err)
	}

	// Before finalizing, nonce=1 is also rejected (state hasn't advanced yet)
	tx1 := newSignedTx(t, 1, pubs[0], privs[0], to, 10, 1, 1)
	if err := c.SubmitTx(tx1); err == nil {
		t.Fatal("SECURITY FAILURE: nonce=1 accepted before state advances")
	}
	t.Logf("✅ Wrong nonce rejected at all stages")
}

// TestNonce_HighVolume stress tests with 1000 independent senders, each
// submitting one transaction (nonce=0), simulating high mempool load.
func TestNonce_HighVolume(t *testing.T) {
	const numSenders = 1000

	c, pubs, privs := newStressChain(t, numSenders+1, 10_000)
	recipient := chain.AddressFromPublicKey(pubs[numSenders])

	for i := 0; i < numSenders; i++ {
		tx := newSignedTx(t, 1, pubs[i], privs[i], recipient, 10, 1, 0)
		if err := c.SubmitTx(tx); err != nil {
			t.Fatalf("sender %d: submit failed: %v", i, err)
		}
	}

	if len(c.Mempool) != numSenders {
		t.Fatalf("expected %d txs, got %d", numSenders, len(c.Mempool))
	}

	b, err := c.BuildBlock("stress-proposer", "", numSenders)
	if err != nil {
		t.Fatalf("build block failed: %v", err)
	}
	if err := c.FinalizeBlock(b); err != nil {
		t.Fatalf("finalize block failed: %v", err)
	}
	t.Logf("✅ High volume: %d transactions submitted and finalized", numSenders)
}

// TestNonce_ConcurrentSenders validates correct nonce handling when many
// goroutines submit transactions to independent chain instances simultaneously.
func TestNonce_ConcurrentSenders(t *testing.T) {
	const numGoroutines = 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

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
				ChainID: fmt.Sprintf("nonce-stress-%d", id),
				Alloc:   map[chain.Address]uint64{from: 100_000},
			}
			c, err := chain.NewChain(genesis)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: chain creation failed: %w", id, err)
				return
			}

			// Submit 5 sequential transactions per goroutine
			for n := uint64(0); n < 5; n++ {
				tx := chain.Tx{
					ChainID:   1,
					From:      from,
					To:        chain.Address("0xdeadbeef"),
					Amount:    10,
					Fee:       1,
					Nonce:     n,
					PublicKey: crypto.PublicKeyHex(pub),
				}
				if signErr := tx.Sign(priv); signErr != nil {
					errors <- fmt.Errorf("goroutine %d nonce %d: sign failed: %w", id, n, signErr)
					return
				}
				if submitErr := c.SubmitTx(tx); submitErr != nil {
					errors <- fmt.Errorf("goroutine %d nonce %d: submit failed: %w", id, n, submitErr)
					return
				}
				// Advance state nonce by applying the tx
				if applyErr := c.State.ApplyTx(tx); applyErr != nil {
					errors <- fmt.Errorf("goroutine %d nonce %d: apply failed: %w", id, n, applyErr)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("%v", err)
	}
	t.Logf("✅ %d concurrent senders each submitted 5 sequential transactions", numGoroutines)
}
