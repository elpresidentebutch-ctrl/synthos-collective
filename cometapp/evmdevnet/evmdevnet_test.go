package evmdevnet_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	cmtlog "github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/rpc/client/local"
	cmttypes "github.com/cometbft/cometbft/types"

	"synthos-collective/cometapp/evmdevnet"
)

func lenientTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "evmdevnet-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestEVMDevnet_SetupAndProduceBlocks(t *testing.T) {
	home := lenientTempDir(t)

	// 1. Setup Devnet Genesis
	if err := evmdevnet.SetupGenesis(home); err != nil {
		t.Fatalf("SetupGenesis failed: %v", err)
	}

	// 2. Validate Genesis Invariants
	genFile := filepath.Join(home, "config", "genesis.json")
	doc, err := cmttypes.GenesisDocFromFile(genFile)
	if err != nil {
		t.Fatalf("failed reading genesis doc: %v", err)
	}

	if doc.ChainID != evmdevnet.DevnetChainID {
		t.Fatalf("wrong ChainID: got %s, want %s", doc.ChainID, evmdevnet.DevnetChainID)
	}

	// Invariant Checks: strictly isolated from mainnet
	if evmdevnet.DevnetEVMChainID == 20260702 {
		t.Fatalf("FATAL INVARIANT VIOLATION: Devnet EVM chain ID cannot be mainnet 20260702!")
	}
	if doc.ChainID == "synthos-mainnet-1" {
		t.Fatalf("FATAL INVARIANT VIOLATION: Devnet CometBFT chain ID cannot be mainnet!")
	}

	// 3. Start Devnet Node on Ephemeral Ports
	rpcPort := freePort(t)
	p2pPort := freePort(t)

	cfg := evmdevnet.Config{
		HomeDir:       home,
		RPCAddr:       fmt.Sprintf("tcp://127.0.0.1:%d", rpcPort),
		P2PAddr:       fmt.Sprintf("tcp://127.0.0.1:%d", p2pPort),
		BlockInterval: 100 * time.Millisecond,
		Logger:        cmtlog.NewNopLogger(),
	}

	node, _, err := evmdevnet.StartNode(cfg)
	if err != nil {
		t.Fatalf("StartNode failed: %v", err)
	}
	defer func() {
		_ = node.Stop()
		node.Wait()
	}()

	// 4. Verify Block Production via Local Client
	client := local.New(node)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	produced := false
	for ctx.Err() == nil {
		status, err := client.Status(ctx)
		if err == nil && status.SyncInfo.LatestBlockHeight >= 2 {
			t.Logf("Devnet produced block %d (AppHash: %s)",
				status.SyncInfo.LatestBlockHeight, status.SyncInfo.LatestAppHash)
			produced = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !produced {
		t.Fatalf("devnet failed to produce blocks within timeout")
	}

	// 5. Verify State File was created
	statePath := filepath.Join(home, "synthos", "synthos-app-state.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("synthos-app-state.json not created at %s: %v", statePath, err)
	}
}

func TestEVMDevnet_RefusesMainnetGenesisDir(t *testing.T) {
	home := lenientTempDir(t)
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mainnetGenesis := []byte(`{
  "genesis_time": "2026-10-22T05:11:39.7806013Z",
  "chain_id": "synthos-mainnet-1",
  "initial_height": "1",
  "app_state": {"chain": {"chain_id": "synthos-mainnet-1"}}
}`)
	genPath := filepath.Join(cfgDir, "genesis.json")
	if err := os.WriteFile(genPath, mainnetGenesis, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := evmdevnet.SetupGenesis(home); err == nil {
		t.Fatal("SetupGenesis must refuse a directory with synthos-mainnet-1 genesis.json")
	}
	cfg := evmdevnet.DefaultConfig(home)
	cfg.RPCAddr = ""
	cfg.P2PAddr = fmt.Sprintf("tcp://127.0.0.1:%d", freePort(t))
	if _, _, err := evmdevnet.StartNode(cfg); err == nil {
		t.Fatal("StartNode must refuse a directory with synthos-mainnet-1 genesis.json")
	}
	got, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, mainnetGenesis) {
		t.Fatal("existing mainnet genesis.json was modified after refusal")
	}
}

func TestEVMDevnet_RefusesExistingValidatorKeyOrStateDir(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		body []byte
	}{
		{name: "priv_validator_key", rel: filepath.Join("config", "priv_validator_key.json"), body: []byte(`{"address":"DEAD"}`)},
		{name: "priv_validator_state", rel: filepath.Join("data", "priv_validator_state.json"), body: []byte(`{"height":"380000"}`)},
		{name: "app_state", rel: filepath.Join("synthos", "synthos-app-state.json"), body: []byte(`{"height":380000}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := lenientTempDir(t)
			full := filepath.Join(home, tc.rel)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, tc.body, 0o600); err != nil {
				t.Fatal(err)
			}

			if err := evmdevnet.SetupGenesis(home); err == nil {
				t.Fatalf("SetupGenesis must refuse dir containing %s", tc.rel)
			}
			cfg := evmdevnet.DefaultConfig(home)
			cfg.RPCAddr = ""
			cfg.P2PAddr = fmt.Sprintf("tcp://127.0.0.1:%d", freePort(t))
			if _, _, err := evmdevnet.StartNode(cfg); err == nil {
				t.Fatalf("StartNode must refuse dir containing %s", tc.rel)
			}
			got, err := os.ReadFile(full)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.body) {
				t.Fatalf("protected file %s was modified after refusal", tc.rel)
			}
		})
	}
}

func TestEVMDevnet_RefusesNonEmptyUnrelatedDir(t *testing.T) {
	home := lenientTempDir(t)
	if err := os.WriteFile(filepath.Join(home, "notes.txt"), []byte("unrelated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := evmdevnet.SetupGenesis(home); err == nil {
		t.Fatal("SetupGenesis must refuse non-empty unrelated directory without marker")
	}
	cfg := evmdevnet.DefaultConfig(home)
	cfg.RPCAddr = ""
	cfg.P2PAddr = fmt.Sprintf("tcp://127.0.0.1:%d", freePort(t))
	if _, _, err := evmdevnet.StartNode(cfg); err == nil {
		t.Fatal("StartNode must refuse non-empty unrelated directory without marker")
	}
}

func TestEVMDevnet_AllowsRestartInOwnDevnetDir(t *testing.T) {
	home := lenientTempDir(t)
	if err := evmdevnet.SetupGenesis(home); err != nil {
		t.Fatalf("initial SetupGenesis failed: %v", err)
	}
	if err := evmdevnet.ValidateHomeDir(home); err != nil {
		t.Fatalf("ValidateHomeDir failed on own devnet dir: %v", err)
	}
	if err := evmdevnet.SetupGenesis(home); err != nil {
		t.Fatalf("idempotent SetupGenesis failed on own devnet dir: %v", err)
	}
}
