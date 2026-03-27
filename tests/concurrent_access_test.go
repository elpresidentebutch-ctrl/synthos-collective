package stress_test

import (
	"fmt"
	"sync"
	"testing"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/crypto"
)

// TestConcurrent_ProofLogConcurrentWrites validates that concurrent calls to
// RecordComputation do not cause data races on the ProofLog.
// Run with: go test -race ./tests/...
func TestConcurrent_ProofLogConcurrentWrites(t *testing.T) {
	kp, err := crypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent(
		"race-agent",
		crypto.PublicKeyHex(kp.Public),
		crypto.PrivateKeyHashHex(kp.Private),
		"hw-race",
		100,
	)
	_ = a.AttachKeys(kp)

	const numWriters = 50
	const recordsPerWriter = 20

	var wg sync.WaitGroup
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < recordsPerWriter; j++ {
				a.RecordComputation(map[string]any{
					"action": "concurrent_write",
					"writer": id,
					"seq":    j,
				})
			}
		}(i)
	}
	wg.Wait()

	// ProofRoot must be non-empty after concurrent writes
	if a.ProofRoot() == "" {
		t.Fatal("ProofRoot is empty after concurrent writes")
	}
	t.Logf("✅ %d concurrent writers each recorded %d computations without race", numWriters, recordsPerWriter)
}

// TestConcurrent_ProofLogReadWrite validates concurrent reads and writes on
// ProofLog via the public API do not cause races.
func TestConcurrent_ProofLogReadWrite(t *testing.T) {
	kp, err := crypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent(
		"rw-agent",
		crypto.PublicKeyHex(kp.Public),
		crypto.PrivateKeyHashHex(kp.Private),
		"hw-rw",
		100,
	)
	_ = a.AttachKeys(kp)

	// Seed some initial records
	for i := 0; i < 10; i++ {
		a.RecordComputation(map[string]any{"seed": i})
	}

	const numReaders = 50
	const numWriters = 20

	var wg sync.WaitGroup
	errors := make(chan error, numReaders+numWriters)

	// Concurrent readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			root := a.ProofRoot()
			if root == "" {
				errors <- fmt.Errorf("reader %d: empty ProofRoot", id)
			}
		}(i)
	}

	// Concurrent writers
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			a.RecordComputation(map[string]any{
				"action": "concurrent_write",
				"writer": id,
			})
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("%v", err)
	}
	t.Logf("✅ %d readers and %d writers on ProofLog without race", numReaders, numWriters)
}

// TestConcurrent_MultipleAgents validates that multiple independent agents can
// record computations concurrently without interfering with each other.
func TestConcurrent_MultipleAgents(t *testing.T) {
	const numAgents = 50
	const recordsEach = 20

	agents := make([]*agent.Agent, numAgents)
	for i := 0; i < numAgents; i++ {
		kp, err := crypto.NewKeyPair()
		if err != nil {
			t.Fatalf("keygen failed for agent %d: %v", i, err)
		}
		a := agent.NewAgent(
			fmt.Sprintf("agent-%d", i),
			crypto.PublicKeyHex(kp.Public),
			crypto.PrivateKeyHashHex(kp.Private),
			fmt.Sprintf("hw-%d", i),
			100,
		)
		_ = a.AttachKeys(kp)
		agents[i] = a
	}

	var wg sync.WaitGroup
	errors := make(chan error, numAgents)

	for i, a := range agents {
		wg.Add(1)
		go func(id int, ag *agent.Agent) {
			defer wg.Done()
			for j := 0; j < recordsEach; j++ {
				ag.RecordComputation(map[string]any{
					"agent": id,
					"seq":   j,
				})
			}
			if ag.ProofRoot() == "" {
				errors <- fmt.Errorf("agent %d: empty ProofRoot after %d records", id, recordsEach)
			}
		}(i, a)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("%v", err)
	}
	t.Logf("✅ %d independent agents recorded computations concurrently without race", numAgents)
}

// TestConcurrent_ParallelBlockValidation verifies that multiple goroutines can
// validate the same block concurrently without data corruption.
func TestConcurrent_ParallelBlockValidation(t *testing.T) {
	const numValidators = 50

	c, pubs, privs := newStressChain(t, 2, 100_000)
	to := chain.AddressFromPublicKey(pubs[1])
	tx := newSignedTx(t, 1, pubs[0], privs[0], to, 100, 1, 0)

	if err := c.SubmitTx(tx); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	b, err := c.BuildBlock("proposer", "", 100)
	if err != nil {
		t.Fatalf("build block failed: %v", err)
	}

	var wg sync.WaitGroup
	errors := make(chan error, numValidators)

	for i := 0; i < numValidators; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// ValidateBlock reads the chain state without modifying it
			if err := c.ValidateBlock(b); err != nil {
				errors <- fmt.Errorf("validator %d: block validation failed: %w", id, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("%v", err)
	}
	t.Logf("✅ %d goroutines validated the same block concurrently without race", numValidators)
}
