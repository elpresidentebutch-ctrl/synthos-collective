package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

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

	// Runtime configuration (can be set from environment variables)
	ChainID               uint64
	ConsensusTimeout      time.Duration
	BlockInterval         time.Duration
	MinValidators         int
	FinalityThreshold     int
	AgentID               string
	AgentPrivateKey       string
	HSMEnabled            bool
	HSMSlot               int
	HSMPin                string
	LogLevel              string
	RateLimitRPS          int
	MaxTransactionSize    int64
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

	// Load runtime configuration from environment variables
	cfg.LoadRuntimeConfig()

	return &cfg, nil
}

// LoadRuntimeConfig loads runtime configuration from environment variables
func (cfg *NodeConfig) LoadRuntimeConfig() {
	// Defaults
	cfg.ChainID = 1
	cfg.ConsensusTimeout = 10 * time.Second
	cfg.BlockInterval = 5 * time.Second
	cfg.MinValidators = 3
	cfg.FinalityThreshold = 2
	cfg.LogLevel = "info"
	cfg.RateLimitRPS = 1000
	cfg.MaxTransactionSize = 1024 * 1024 // 1MB

	// Override from environment variables
	if v := os.Getenv("CHAIN_ID"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			cfg.ChainID = id
		}
	}

	if v := os.Getenv("CONSENSUS_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ConsensusTimeout = d
		}
	}

	if v := os.Getenv("BLOCK_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.BlockInterval = d
		}
	}

	if v := os.Getenv("AGENT_ID"); v != "" {
		cfg.AgentID = v
	}

	if v := os.Getenv("AGENT_PRIVATE_KEY"); v != "" {
		cfg.AgentPrivateKey = v
	}

	if v := os.Getenv("HSM_ENABLED"); v == "true" {
		cfg.HSMEnabled = true
	}

	if v := os.Getenv("HSM_SLOT"); v != "" {
		if slot, err := strconv.Atoi(v); err == nil {
			cfg.HSMSlot = slot
		}
	}

	if v := os.Getenv("HSM_PIN"); v != "" {
		cfg.HSMPin = v
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		if rps, err := strconv.Atoi(v); err == nil {
			cfg.RateLimitRPS = rps
		}
	}
}

// ValidateNodeConfig validates that required fields are set
func (cfg *NodeConfig) ValidateNodeConfig() error {
	if cfg.NodeID == "" {
		return fmt.Errorf("node_id must be set")
	}
	if cfg.GenesisPath == "" {
		return fmt.Errorf("genesis_path must be set")
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
	return nil
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

