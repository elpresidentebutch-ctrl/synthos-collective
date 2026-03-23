package config

import (
	"encoding/json"
	"fmt"
	"os"

	"synthos-collective/internal/chain"
)

// NodeConfig describes how to start a SYNTHOS node.
type NodeConfig struct {
	NodeID      string   `json:"node_id"`
	DataDir     string   `json:"data_dir"`
	IsValidator bool     `json:"is_validator"`
	RPCListen   string   `json:"rpc_listen"`
	GenesisPath string   `json:"genesis_path"`
	Peers       []string `json:"peers"`        // "agentID@host:port"
	ListenAddr  string   `json:"listen_addr"`  // e.g. ":9001"
}

// LoadNodeConfig reads a JSON node config from disk.
func LoadNodeConfig(path string) (*NodeConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg NodeConfig
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.NodeID == "" {
		return nil, fmt.Errorf("node_id must be set")
	}
	if cfg.GenesisPath == "" {
		return nil, fmt.Errorf("genesis_path must be set")
	}
	if cfg.DataDir == "" {
		cfg.DataDir = ".synthos-data"
	}
	if cfg.RPCListen == "" {
		cfg.RPCListen = ":8080"
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":9001"
	}
	return &cfg, nil
}

// LoadGenesis reads a JSON genesis file and converts it into a chain.Genesis.
func LoadGenesis(path string) (chain.Genesis, error) {
	f, err := os.Open(path)
	if err != nil {
		return chain.Genesis{}, err
	}
	defer f.Close()

	var raw struct {
		ChainID string            `json:"chain_id"`
		Alloc   map[string]uint64 `json:"alloc"`
		Symbol  string            `json:"symbol"`
		Decimals int              `json:"decimals"`
	}
	if err := json.NewDecoder(f).Decode(&raw); err != nil {
		return chain.Genesis{}, err
	}
	if raw.ChainID == "" {
		return chain.Genesis{}, fmt.Errorf("chain_id must be set")
	}
	alloc := make(map[chain.Address]uint64, len(raw.Alloc))
	for k, v := range raw.Alloc {
		alloc[chain.Address(k)] = v
	}
	gen := chain.Genesis{
		ChainID: raw.ChainID,
		Alloc:   alloc,
		Metadata: map[string]any{
			"symbol":   raw.Symbol,
			"decimals": raw.Decimals,
		},
	}
	return gen, nil
}

