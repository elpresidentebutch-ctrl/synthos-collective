package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"synthos-collective/internal/config"
)

func TestLoadGenesisPreservesTransactionChainIDAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "genesis.json")
	data := []byte(`{
  "chain_id": "synthos-mainnet-1",
  "tx_chain_id": 20260702,
  "alloc": {
    "0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 100000000000
  },
  "metadata": {
    "symbol": "SYN",
    "decimals": 0,
    "network": "SYNTHOS Collective"
  }
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	gen, err := config.LoadGenesis(path)
	if err != nil {
		t.Fatal(err)
	}
	if gen.TxChainID != 20260702 {
		t.Fatalf("TxChainID: got %d, want 20260702", gen.TxChainID)
	}
	if gen.Metadata["network"] != "SYNTHOS Collective" {
		t.Fatalf("metadata network was not preserved: %#v", gen.Metadata)
	}
}

func TestLoadNodeConfigHonorsRenderEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.json")
	data := []byte(`{
  "node_id": "validator-1",
  "data_dir": ".local-data",
  "is_validator": true,
  "rpc_listen": ":8080",
  "genesis_path": "/config/genesis.json",
  "peers": [],
  "listen_addr": ":9001"
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SYNTHOS_DATA_DIR", "/data")
	t.Setenv("PORT", "10000")
	t.Setenv("SYNTHOS_LISTEN_ADDR", ":0")

	cfg, err := config.LoadNodeConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/data" {
		t.Fatalf("DataDir: got %q, want /data", cfg.DataDir)
	}
	if cfg.RPCListen != ":10000" {
		t.Fatalf("RPCListen: got %q, want :10000", cfg.RPCListen)
	}
	if cfg.ListenAddr != ":0" {
		t.Fatalf("ListenAddr: got %q, want :0", cfg.ListenAddr)
	}
}
