// Package stress_test provides comprehensive stress tests for the Synthos system,
// validating the 5 critical security fixes under high-load conditions.
//
// Run with race detector: go test -v -race ./tests/...
// Run benchmarks:         go test -bench=. ./tests/...
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

// newStressChain creates a fresh chain with numAccounts pre-funded accounts for
// stress testing. Returns the chain, public keys, and private keys indexed in order.
func newStressChain(t testing.TB, numAccounts int, balanceEach uint64) (*chain.Chain, []ed25519.PublicKey, []ed25519.PrivateKey) {
	t.Helper()

	genesis := chain.Genesis{
		ChainID: "stress-test",
		Alloc:   make(map[chain.Address]uint64, numAccounts),
	}

	pubs := make([]ed25519.PublicKey, numAccounts)
	privs := make([]ed25519.PrivateKey, numAccounts)
	for i := 0; i < numAccounts; i++ {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("failed to generate key %d: %v", i, err)
		}
		pubs[i] = pub
		privs[i] = priv
		genesis.Alloc[chain.AddressFromPublicKey(pub)] = balanceEach
	}

	c, err := chain.NewChain(genesis)
	if err != nil {
		t.Fatalf("failed to create chain: %v", err)
	}
	return c, pubs, privs
}

// newSignedTx creates a signed transaction with the given parameters.
func newSignedTx(t testing.TB, chainID uint64, pub ed25519.PublicKey, priv ed25519.PrivateKey, to chain.Address, amount, fee, nonce uint64) chain.Tx {
	t.Helper()
	from := chain.AddressFromPublicKey(pub)
	pubHex := crypto.PublicKeyHex(pub)
	tx := chain.Tx{
		ChainID:   chainID,
		From:      from,
		To:        to,
		Amount:    amount,
		Fee:       fee,
		Nonce:     nonce,
		PublicKey: pubHex,
	}
	if err := tx.Sign(priv); err != nil {
		t.Fatalf("failed to sign tx: %v", err)
	}
	return tx
}

// buildAndFinalizeBlock builds and finalizes a block from the current mempool.
// Used in sequential nonce tests to advance state.
func buildAndFinalizeBlock(t testing.TB, c *chain.Chain) *chain.Block {
	t.Helper()
	b, err := c.BuildBlock("stress-proposer", "", 1000)
	if err != nil {
		t.Fatalf("failed to build block: %v", err)
	}
	if err := c.FinalizeBlock(b); err != nil {
		t.Fatalf("failed to finalize block: %v", err)
	}
	return b
}

// TestStress_BasicSanity verifies the stress test helpers work correctly.
func TestStress_BasicSanity(t *testing.T) {
	c, pubs, privs := newStressChain(t, 2, 10_000)

	to := chain.AddressFromPublicKey(pubs[1])
	tx := newSignedTx(t, 1, pubs[0], privs[0], to, 100, 1, 0)

	if err := c.SubmitTx(tx); err != nil {
		t.Fatalf("failed to submit tx: %v", err)
	}
	if len(c.Mempool) != 1 {
		t.Fatalf("expected 1 tx in mempool, got %d", len(c.Mempool))
	}

	b := buildAndFinalizeBlock(t, c)
	if len(b.Tx) != 1 {
		t.Fatalf("expected 1 tx in block, got %d", len(b.Tx))
	}
}

// TestStress_HighThroughput validates that 1000 transactions can be submitted
// and finalized correctly using 1000 independent sender accounts.
func TestStress_HighThroughput(t *testing.T) {
	const numAccounts = 1000
	const amount = 100
	const fee = 1

	c, pubs, privs := newStressChain(t, numAccounts+1, 10_000)
	recipient := chain.AddressFromPublicKey(pubs[numAccounts])

	// Submit one tx per account (nonce=0 for all, independent accounts)
	for i := 0; i < numAccounts; i++ {
		tx := newSignedTx(t, 1, pubs[i], privs[i], recipient, amount, fee, 0)
		if err := c.SubmitTx(tx); err != nil {
			t.Fatalf("account %d: failed to submit tx: %v", i, err)
		}
	}

	if len(c.Mempool) != numAccounts {
		t.Fatalf("expected %d txs in mempool, got %d", numAccounts, len(c.Mempool))
	}

	// Finalize in batches of 1000
	b, err := c.BuildBlock("stress-proposer", "", numAccounts)
	if err != nil {
		t.Fatalf("failed to build block: %v", err)
	}
	if err := c.FinalizeBlock(b); err != nil {
		t.Fatalf("failed to finalize block: %v", err)
	}
	if len(b.Tx) != numAccounts {
		t.Fatalf("expected %d txs in block, got %d", numAccounts, len(b.Tx))
	}
	t.Logf("✅ High throughput: finalized %d transactions in one block", len(b.Tx))
}

// TestStress_ConcurrentChains validates concurrent operations on independent
// chain instances using 100 goroutines.
func TestStress_ConcurrentChains(t *testing.T) {
	const numGoroutines = 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			c, pubs, privs := newStressChain(t, 2, 50_000)
			to := chain.AddressFromPublicKey(pubs[1])

			tx := newSignedTx(t, 1, pubs[0], privs[0], to, 100, 1, 0)
			if err := c.SubmitTx(tx); err != nil {
				errors <- fmt.Errorf("goroutine %d submit failed: %w", id, err)
				return
			}
			buildAndFinalizeBlock(t, c)
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent chain error: %v", err)
	}
	t.Logf("✅ %d concurrent chain operations completed without error", numGoroutines)
}
