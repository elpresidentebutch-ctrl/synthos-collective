package stress_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synthos-collective/internal/chain"
)

type persistedState struct {
	ChainID   string                       `json:"chain_id"`
	TxChainID uint64                       `json:"tx_chain_id"`
	Staking   chain.ValidatorStakingParams `json:"staking"`
	Height    int64                        `json:"height"`
	AppHash   string                       `json:"app_hash"`
	State     *chain.State                 `json:"state"`
}

// TestBenchmark_StateStorageBaseline measures the baseline performance of the
// current monolithic JSON state file and full Merkle root rebuild against an
// isolated copy of the mainnet state file.
func TestBenchmark_StateStorageBaseline(t *testing.T) {
	// Point strictly to the isolated temp copy, NEVER a live node or validator
	benchCopyPath := filepath.Join(os.TempDir(), "synthos-evm-benchmark", "state-copy.json")
	raw, err := os.ReadFile(benchCopyPath)
	if err != nil {
		t.Fatalf("could not read benchmark state copy: %v", err)
	}

	fileSize := len(raw)
	fmt.Printf("\n=== SYNTHOS STATE STORAGE BENCHMARK (Task 2) ===\n")
	fmt.Printf("1. Baseline Mainnet Snapshot:\n")
	fmt.Printf("   State File Size: %d bytes (%.2f KB)\n", fileSize, float64(fileSize)/1024.0)

	var p persistedState
	unmarshalStart := time.Now()
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal state failed: %v", err)
	}
	unmarshalDur := time.Since(unmarshalStart)
	fmt.Printf("   Height: %d | Total Accounts: %d\n", p.Height, len(p.State.Accounts))
	fmt.Printf("   Deserialization Time: %s\n", unmarshalDur)

	// Measure Merkle Root Rebuild Time over multiple iterations
	const rootIterations = 500
	rootStart := time.Now()
	var lastRoot string
	for i := 0; i < rootIterations; i++ {
		lastRoot = p.State.Root()
	}
	avgRootRebuild := time.Since(rootStart) / time.Duration(rootIterations)
	fmt.Printf("   State Root Rebuild Time (avg of %d runs): %s (Root: %s)\n", rootIterations, avgRootRebuild, lastRoot)

	// Measure Save Time (MarshalIndent + Write-then-Rename) over multiple iterations
	tempDir := filepath.Join(os.TempDir(), "synthos-evm-benchmark-io")
	_ = os.MkdirAll(tempDir, 0o700)
	defer os.RemoveAll(tempDir)

	const saveIterations = 50
	saveStart := time.Now()
	for i := 0; i < saveIterations; i++ {
		data, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		target := filepath.Join(tempDir, fmt.Sprintf("state-%d.json", i))
		tmp := target + ".tmp"
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		if err := os.Rename(tmp, target); err != nil {
			t.Fatalf("rename failed: %v", err)
		}
	}
	avgSaveTime := time.Since(saveStart) / time.Duration(saveIterations)
	fmt.Printf("   Save Time (JSON Marshal + Disk Sync, avg of %d runs): %s\n", saveIterations, avgSaveTime)

	// Scaled Growth Projections
	fmt.Printf("\n2. Scaled Growth Projections (Monolithic JSON & Full Root Rebuild):\n")
	scales := []int{100, 1000, 10000, 50000, 100000}
	for _, count := range scales {
		scaledState := p.State.Clone()
		for i := len(scaledState.Accounts); i < count; i++ {
			addr := chain.Address(fmt.Sprintf("0x%040x", i+1000))
			scaledState.Accounts[addr] = chain.Account{
				Balance: uint64(100 + i),
				Nonce:   uint64(i % 10),
			}
		}

		// Measure root rebuild time
		rStart := time.Now()
		_ = scaledState.Root()
		rDur := time.Since(rStart)

		// Measure serialization size and time
		mStart := time.Now()
		scaledPersisted := p
		scaledPersisted.State = scaledState
		data, _ := json.Marshal(scaledPersisted)
		mDur := time.Since(mStart)

		fmt.Printf("   Accounts: %7d | Estimated Size: %7.2f KB (%5.2f MB) | Root Rebuild: %10s | JSON Marshal: %10s\n",
			count, float64(len(data))/1024.0, float64(len(data))/(1024.0*1024.0), rDur, mDur)
	}
	fmt.Printf("=================================================\n\n")
}
