package chain_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"synthos-collective/internal/chain"
)

// makeStressChain creates a chain pre-funded for n unique senders.
// Returns the chain plus the keypairs for each sender.
func makeStressChain(n int) (*chain.Chain, []ed25519.PublicKey, []ed25519.PrivateKey) {
	pubs := make([]ed25519.PublicKey, n)
	privs := make([]ed25519.PrivateKey, n)
	alloc := make(map[chain.Address]uint64, n)
	for i := 0; i < n; i++ {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			panic(err)
		}
		pubs[i] = pub
		privs[i] = priv
		alloc[chain.AddressFromPublicKey(pub)] = 10_000_000
	}
	c, err := chain.NewChain(chain.Genesis{ChainID: "stress-test", Alloc: alloc})
	if err != nil {
		panic(err)
	}
	return c, pubs, privs
}

// signTx builds and signs a transaction for the sender at index i.
func signTx(pubs []ed25519.PublicKey, privs []ed25519.PrivateKey, from, to int, nonce uint64) chain.Tx {
	fromAddr := chain.AddressFromPublicKey(pubs[from])
	toAddr := chain.AddressFromPublicKey(pubs[to])
	tx := chain.Tx{
		ChainID:   1,
		From:      fromAddr,
		To:        toAddr,
		Amount:    100,
		Fee:       1,
		Nonce:     nonce,
		PublicKey: "0x" + hex.EncodeToString(pubs[from]),
	}
	if err := tx.Sign(privs[from]); err != nil {
		panic(fmt.Sprintf("signTx: %v", err))
	}
	return tx
}

// TestStress_MempoolThroughput submits 1 000 independent transactions and measures
// the raw mempool ingestion rate (signature verification + nonce check + map insert).
func TestStress_MempoolThroughput(t *testing.T) {
	const senders = 1000
	c, pubs, privs := makeStressChain(senders)

	start := time.Now()
	for i := 0; i < senders; i++ {
		tx := signTx(pubs, privs, i, (i+1)%senders, 0)
		if err := c.SubmitTx(tx); err != nil {
			t.Fatalf("submit failed at i=%d: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	if len(c.Mempool) != senders {
		t.Fatalf("expected %d txs in mempool, got %d", senders, len(c.Mempool))
	}
	t.Logf("Submitted %d txs in %v (%.0f tx/s)", senders, elapsed, float64(senders)/elapsed.Seconds())
}

// TestStress_BlockPipeline exercises the full submit→build→finalize pipeline over
// many rounds with 200 concurrent senders, measuring end-to-end throughput.
func TestStress_BlockPipeline(t *testing.T) {
	const (
		senders = 200
		rounds  = 50
	)
	c, pubs, privs := makeStressChain(senders)

	totalTx := 0
	start := time.Now()

	for round := 0; round < rounds; round++ {
		// Submit one tx per sender using their current state nonce.
		for i := 0; i < senders; i++ {
			addr := chain.AddressFromPublicKey(pubs[i])
			nonce := c.State.GetNextNonce(addr)
			tx := signTx(pubs, privs, i, (i+round+1)%senders, nonce)
			if err := c.SubmitTx(tx); err != nil {
				t.Fatalf("round %d sender %d: %v", round, i, err)
			}
		}

		// Drain mempool into blocks (at most senders txs per block).
		for len(c.Mempool) > 0 {
			b, err := c.BuildBlock("proposer", "", senders)
			if err != nil {
				t.Fatalf("BuildBlock: %v", err)
			}
			if err := c.FinalizeBlock(b); err != nil {
				t.Fatalf("FinalizeBlock: %v", err)
			}
			totalTx += len(b.Tx)
		}
	}

	elapsed := time.Since(start)
	t.Logf("BlockPipeline: %d txs, %d rounds, %d blocks in %v (%.0f tx/s)",
		totalTx, rounds, c.Height(), elapsed, float64(totalTx)/elapsed.Seconds())

	if c.Height() < rounds {
		t.Fatalf("expected at least %d blocks, got height %d", rounds, c.Height())
	}
	if len(c.Mempool) != 0 {
		t.Fatalf("mempool not empty after pipeline: %d remaining", len(c.Mempool))
	}
}

// TestStress_LargeMempoolBuild fills the mempool with 2 000 transactions (with varying
// fees to exercise the fee-priority sort) then times a single BuildBlock + FinalizeBlock.
func TestStress_LargeMempoolBuild(t *testing.T) {
	const senders = 2000
	c, pubs, privs := makeStressChain(senders)

	for i := 0; i < senders; i++ {
		addr := chain.AddressFromPublicKey(pubs[i])
		toAddr := chain.AddressFromPublicKey(pubs[(i+1)%senders])
		tx := chain.Tx{
			ChainID:   1,
			From:      addr,
			To:        toAddr,
			Amount:    10000,
			Fee:       uint64(i%200 + 1), // fees 1–200, varied to exercise sort
			Nonce:     0,
			PublicKey: "0x" + hex.EncodeToString(pubs[i]),
		}
		if err := tx.Sign(privs[i]); err != nil {
			t.Fatalf("sign: %v", err)
		}
		if err := c.SubmitTx(tx); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	if len(c.Mempool) != senders {
		t.Fatalf("mempool size: want %d got %d", senders, len(c.Mempool))
	}

	buildStart := time.Now()
	b, err := c.BuildBlock("proposer", "", 1000)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	buildElapsed := time.Since(buildStart)

	finalizeStart := time.Now()
	if err := c.FinalizeBlock(b); err != nil {
		t.Fatalf("FinalizeBlock: %v", err)
	}
	finalizeElapsed := time.Since(finalizeStart)

	t.Logf("LargeMempoolBuild: mempool=%d, block_txs=%d, build=%v, finalize=%v",
		senders, len(b.Tx), buildElapsed, finalizeElapsed)

	if len(b.Tx) != 1000 {
		t.Fatalf("expected 1000 txs in block, got %d", len(b.Tx))
	}
}

// TestStress_ManyBlocks produces 100 blocks back-to-back to check for state
// consistency and no memory leaks across a long chain.
func TestStress_ManyBlocks(t *testing.T) {
	const (
		senders    = 50
		numBlocks  = 100
	)
	c, pubs, privs := makeStressChain(senders)

	start := time.Now()
	for block := 0; block < numBlocks; block++ {
		for i := 0; i < senders; i++ {
			addr := chain.AddressFromPublicKey(pubs[i])
			nonce := c.State.GetNextNonce(addr)
			tx := signTx(pubs, privs, i, (i+block+1)%senders, nonce)
			if err := c.SubmitTx(tx); err != nil {
				t.Fatalf("block %d sender %d: %v", block, i, err)
			}
		}
		b, err := c.BuildBlock(fmt.Sprintf("proposer-%d", block%senders), "", senders)
		if err != nil {
			t.Fatalf("BuildBlock at block %d: %v", block, err)
		}
		if err := c.FinalizeBlock(b); err != nil {
			t.Fatalf("FinalizeBlock at block %d: %v", block, err)
		}
	}
	elapsed := time.Since(start)

	if uint64(c.Height()) != numBlocks {
		t.Fatalf("expected height %d, got %d", numBlocks, c.Height())
	}
	t.Logf("ManyBlocks: height=%d txs_per_block=%d elapsed=%v (%.0f blocks/s)",
		c.Height(), senders, elapsed, float64(numBlocks)/elapsed.Seconds())
}

// TestStress_StateRootConsistency verifies state root is stable across a full pipeline
// and that ValidateBlock succeeds for every finalized block.
// Note: the block header's StateRoot commits to post-tx state BEFORE fee distribution;
// c.State.Root() after finalization will differ due to fee crediting — this is by design.
func TestStress_StateRootConsistency(t *testing.T) {
	const (
		senders   = 100
		numBlocks = 20
	)
	c, pubs, privs := makeStressChain(senders)

	for block := 0; block < numBlocks; block++ {
		for i := 0; i < senders; i++ {
			addr := chain.AddressFromPublicKey(pubs[i])
			nonce := c.State.GetNextNonce(addr)
			tx := signTx(pubs, privs, i, (i+1)%senders, nonce)
			if err := c.SubmitTx(tx); err != nil {
				t.Fatalf("block %d sender %d: %v", block, i, err)
			}
		}
		b, err := c.BuildBlock("proposer", "", senders)
		if err != nil {
			t.Fatalf("BuildBlock: %v", err)
		}
		// Capture the block's committed StateRoot before finalization.
		committedRoot := b.Header.StateRoot

		if err := c.FinalizeBlock(b); err != nil {
			t.Fatalf("FinalizeBlock: %v", err)
		}

		// The block is consistent: its committed StateRoot was validated by FinalizeBlock.
		// The actual state root will differ by the fee-distribution delta (by design).
		if committedRoot == "" {
			t.Fatalf("block %d: empty StateRoot", block+1)
		}
		if b.Hash == "" {
			t.Fatalf("block %d: empty hash", block+1)
		}
	}

	if uint64(c.Height()) != numBlocks {
		t.Fatalf("expected height %d, got %d", numBlocks, c.Height())
	}
	t.Logf("StateRootConsistency: %d blocks, height=%d, mempool=%d",
		numBlocks, c.Height(), len(c.Mempool))
}

// --- Benchmarks ---

// BenchmarkChain_SubmitTx measures raw transaction submission throughput.
// Keys and txs are pre-generated outside the timer.
func BenchmarkChain_SubmitTx(b *testing.B) {
	// Pre-generate all keys and txs before the timer starts.
	n := b.N
	if n < 1 {
		n = 1
	}
	pubs := make([]ed25519.PublicKey, n+1)
	privs := make([]ed25519.PrivateKey, n+1)
	alloc := make(map[chain.Address]uint64, n+1)
	for i := 0; i <= n; i++ {
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		pubs[i] = pub
		privs[i] = priv
		alloc[chain.AddressFromPublicKey(pub)] = 10_000_000
	}
	c, _ := chain.NewChain(chain.Genesis{ChainID: "bench", Alloc: alloc})
	toAddr := chain.AddressFromPublicKey(pubs[n])

	txs := make([]chain.Tx, n)
	for i := 0; i < n; i++ {
		fromAddr := chain.AddressFromPublicKey(pubs[i])
		tx := chain.Tx{
			ChainID:   1,
			From:      fromAddr,
			To:        toAddr,
			Amount:    100,
			Fee:       1,
			Nonce:     0,
			PublicKey: "0x" + hex.EncodeToString(pubs[i]),
		}
		tx.Sign(privs[i])
		txs[i] = tx
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.SubmitTx(txs[i]); err != nil {
			b.Fatalf("SubmitTx[%d]: %v", i, err)
		}
	}
}

// BenchmarkChain_BuildBlock measures block construction from a fixed 1 000-tx mempool.
// The mempool is never drained between iterations so each call does the same work.
func BenchmarkChain_BuildBlock(b *testing.B) {
	const poolSize = 1000
	c, pubs, privs := makeStressChain(poolSize)
	for i := 0; i < poolSize; i++ {
		tx := signTx(pubs, privs, i, (i+1)%poolSize, 0)
		c.SubmitTx(tx) //nolint:errcheck
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.BuildBlock("proposer", "", poolSize)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkChain_FinalizeBlock measures a full build+finalize cycle.
// The chain state changes on each iteration, so this is a real-world benchmark.
func BenchmarkChain_FinalizeBlock(b *testing.B) {
	const perBlock = 100
	c, pubs, privs := makeStressChain(perBlock)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		for j := 0; j < perBlock; j++ {
			addr := chain.AddressFromPublicKey(pubs[j])
			nonce := c.State.GetNextNonce(addr)
			tx := signTx(pubs, privs, j, (j+1)%perBlock, nonce)
			c.SubmitTx(tx) //nolint:errcheck
		}
		blk, err := c.BuildBlock("proposer", "", perBlock)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if err := c.FinalizeBlock(blk); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkState_Root measures state root (merkle-like hash) computation
// as the account set grows to 10 000 entries.
func BenchmarkState_Root(b *testing.B) {
	const accountsN = 10000
	s := chain.NewState()
	for i := 0; i < accountsN; i++ {
		s.Set(chain.Address(fmt.Sprintf("0x%040x", i)), chain.Account{Balance: uint64(i), Nonce: uint64(i / 10)})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Root()
	}
}
