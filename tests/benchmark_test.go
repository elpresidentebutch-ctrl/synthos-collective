package stress_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/crypto"
)

// BenchmarkTxSign measures the throughput of ED25519 transaction signing.
func BenchmarkTxSign(b *testing.B) {
	kp, _ := crypto.NewKeyPair()
	from := chain.AddressFromPublicKey(kp.Public)
	to := chain.Address("0xbenchmarkrecipient")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := chain.Tx{
			ChainID:   1,
			From:      from,
			To:        to,
			Amount:    100,
			Fee:       1,
			Nonce:     uint64(i),
			PublicKey: crypto.PublicKeyHex(kp.Public),
		}
		if err := tx.Sign(kp.Private); err != nil {
			b.Fatalf("sign failed: %v", err)
		}
	}
}

// BenchmarkTxVerify measures the throughput of ED25519 transaction verification.
func BenchmarkTxVerify(b *testing.B) {
	kp, _ := crypto.NewKeyPair()
	from := chain.AddressFromPublicKey(kp.Public)
	to := chain.Address("0xbenchmarkrecipient")

	// Pre-sign a tx to benchmark verification only
	tx := chain.Tx{
		ChainID:   1,
		From:      from,
		To:        to,
		Amount:    100,
		Fee:       1,
		Nonce:     0,
		PublicKey: crypto.PublicKeyHex(kp.Public),
	}
	if err := tx.Sign(kp.Private); err != nil {
		b.Fatalf("sign failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := tx.Verify(); err != nil {
			b.Fatalf("verify failed: %v", err)
		}
	}
}

// BenchmarkChainSubmitTx measures mempool submission throughput using
// independent sender accounts (no nonce contention).
func BenchmarkChainSubmitTx(b *testing.B) {
	// Pre-generate keys and transactions
	txs := make([]chain.Tx, b.N)
	alloc := make(map[chain.Address]uint64, b.N)

	for i := 0; i < b.N; i++ {
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		from := chain.AddressFromPublicKey(pub)
		alloc[from] = 100_000

		tx := chain.Tx{
			ChainID:   1,
			From:      from,
			To:        chain.Address("0xbenchrecipient"),
			Amount:    100,
			Fee:       1,
			Nonce:     0,
			PublicKey: crypto.PublicKeyHex(pub),
		}
		tx.Sign(priv)
		txs[i] = tx
	}

	c, _ := chain.NewChain(chain.Genesis{
		ChainID: "bench",
		Alloc:   alloc,
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.SubmitTx(txs[i]); err != nil {
			b.Fatalf("submit %d failed: %v", i, err)
		}
	}
}

// BenchmarkBlockBuildAndFinalize measures block construction and finalization
// throughput with 100 transactions per block.
func BenchmarkBlockBuildAndFinalize(b *testing.B) {
	const txPerBlock = 100

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Setup: pre-fill mempool with 100 txs
		alloc := make(map[chain.Address]uint64, txPerBlock)
		txs := make([]chain.Tx, txPerBlock)
		for j := 0; j < txPerBlock; j++ {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			from := chain.AddressFromPublicKey(pub)
			alloc[from] = 100_000
			tx := chain.Tx{
				ChainID:   1,
				From:      from,
				To:        chain.Address("0xfeerecipient"),
				Amount:    10,
				Fee:       1,
				Nonce:     0,
				PublicKey: crypto.PublicKeyHex(pub),
			}
			tx.Sign(priv)
			txs[j] = tx
		}
		c, _ := chain.NewChain(chain.Genesis{ChainID: "bench", Alloc: alloc})
		for _, tx := range txs {
			_ = c.SubmitTx(tx)
		}
		b.StartTimer()

		block, err := c.BuildBlock("bench-proposer", "", txPerBlock)
		if err != nil {
			b.Fatalf("build block failed: %v", err)
		}
		if err := c.FinalizeBlock(block); err != nil {
			b.Fatalf("finalize block failed: %v", err)
		}
	}
}

// BenchmarkKeyPairGeneration measures ED25519 key pair generation throughput.
func BenchmarkKeyPairGeneration(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := crypto.NewKeyPair(); err != nil {
			b.Fatalf("keygen failed: %v", err)
		}
	}
}

// BenchmarkAgentCreation measures Agent initialization throughput including
// key pair generation and attachment.
func BenchmarkAgentCreation(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		kp, _ := crypto.NewKeyPair()
		a := agent.NewAgent(
			"bench-agent",
			crypto.PublicKeyHex(kp.Public),
			crypto.PrivateKeyHashHex(kp.Private),
			"hw-bench",
			100,
		)
		if err := a.AttachKeys(kp); err != nil {
			b.Fatalf("AttachKeys failed: %v", err)
		}
	}
}

// BenchmarkProofLogRecord measures ProofLog.RecordComputation throughput.
func BenchmarkProofLogRecord(b *testing.B) {
	kp, _ := crypto.NewKeyPair()
	a := agent.NewAgent(
		"bench-proof-agent",
		crypto.PublicKeyHex(kp.Public),
		crypto.PrivateKeyHashHex(kp.Private),
		"hw-bench",
		100,
	)
	_ = a.AttachKeys(kp)
	payload := map[string]any{
		"action": "benchmark",
		"data":   "payload",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.RecordComputation(payload)
	}
}

// BenchmarkTxSignVerifyRoundTrip measures the full sign-then-verify cycle.
func BenchmarkTxSignVerifyRoundTrip(b *testing.B) {
	kp, _ := crypto.NewKeyPair()
	from := chain.AddressFromPublicKey(kp.Public)
	to := chain.Address("0xbenchmarkrecipient")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := chain.Tx{
			ChainID:   1,
			From:      from,
			To:        to,
			Amount:    100,
			Fee:       1,
			Nonce:     uint64(i),
			PublicKey: crypto.PublicKeyHex(kp.Public),
		}
		if err := tx.Sign(kp.Private); err != nil {
			b.Fatalf("sign failed: %v", err)
		}
		if err := tx.Verify(); err != nil {
			b.Fatalf("verify failed: %v", err)
		}
	}
}
