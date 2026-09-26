package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"synthos-collective/internal/chain"
)

// NodeConfig describes how to start a SYNTHOS node.
type NodeConfig struct {
	NodeID      string            `json:"node_id"`
	DataDir     string            `json:"data_dir"`
	IsValidator bool              `json:"is_validator"`
	RPCListen   string            `json:"rpc_listen"`
	GenesisPath string            `json:"genesis_path"`
	Peers       []string          `json:"peers"`       // "agentID@host:port"
	HTTPPeers   []string          `json:"http_peers"`  // HTTPS peer RPC endpoints for provider-neutral sync
	ListenAddr  string            `json:"listen_addr"` // e.g. ":9001"
	PrivateKey  string            `json:"private_key"` // hex encoded ed25519 private key for reproducible devnets
	Validators  []string          `json:"validators"`  // validator agent IDs used for finality threshold
	PeerKeys    map[string]string `json:"peer_keys"`   // agentID -> hex encoded ed25519 public key

	// ConsensusPeers, when non-empty, is the roster of OTHER validators this
	// node runs a REAL multi-party consensus round with over HTTPS (see
	// rpc.Server.ProposeBlockWithConsensus and cmd/synthosd/main.go's
	// startBlockProducer): every entry is asked to independently validate
	// and vote on each proposal before it's finalized. Leaving this unset
	// preserves today's behavior exactly -- self-only finalization, same as
	// a deployment with no consensus peers configured at all.
	ConsensusPeers []string `json:"consensus_peers"`

	// TrustedValidators, when set, is the roster of validator IDs this node's
	// Chain should accept finalized-block signatures from (resolved to keys via
	// PeerKeys, same as Validators/AddPeer), independently of Validators above.
	// It exists for deployments like a single-sequencer setup with read-only
	// catch-up nodes: those nodes need to recognize the sequencer's key to
	// accept its blocks, but must NOT have that inflate their own Engine's
	// totalValidators/finality-quorum math (see cmd/synthosd/main.go), since no
	// real multi-party signature-gathering transport exists to ever satisfy a
	// quorum above 1 here. Falls back to Validators when unset, preserving
	// existing behavior for deployments that don't set it.
	TrustedValidators []string `json:"trusted_validators"`

	// AuthEnforceFromHeight, when set, is passed to chain.Chain.
	// SetAuthEnforceFromHeight: it grandfathers in any block below this height
	// from the ProposerSignature/QuorumSignatures check, for a chain whose
	// history predates that feature (those old blocks were never signed, so
	// requiring signatures on them would make catch-up permanently impossible
	// past that point). Must be the same value on every node sharing this
	// chain, and should exactly match the real height at which block-signing
	// was actually deployed for this chain -- not a per-node value.
	AuthEnforceFromHeight uint64 `json:"auth_enforce_from_height"`

	// StateRootEnforceFromHeight, when set, is passed to chain.Chain.
	// SetStateRootEnforceFromHeight: it grandfathers in any block below this
	// height from the replayed-state-root-must-match check, for a chain whose
	// history includes state roots computed before a fix to what State.Root()
	// covers (see internal/chain/core.go's Root()). Must be the same value on
	// every node sharing this chain.
	StateRootEnforceFromHeight uint64 `json:"state_root_enforce_from_height"`

	// IrregularStateCorrections, when set, is passed to chain.Chain.
	// SetIrregularStateCorrections: one-time, explicit, height-gated
	// adjustments that reproduce a real, already-happened, already
	// quorum-agreed state change that was never recorded as an ordinary
	// transaction (see chain.StateCorrection's doc comment for the exact
	// incident and safety contract). Must be identical on every node
	// sharing this chain, exactly like AuthEnforceFromHeight/
	// StateRootEnforceFromHeight above -- a node missing an entry here
	// will permanently disagree with its peers about this chain's state
	// from that block onward.
	IrregularStateCorrections []chain.StateCorrection `json:"irregular_state_corrections"`

	// Runtime configuration (can be set from environment variables)
	ChainID            uint64
	ConsensusTimeout   time.Duration
	BlockInterval      time.Duration
	MinValidators      int
	FinalityThreshold  int
	AgentID            string
	AgentPrivateKey    string
	HSMEnabled         bool
	HSMSlot            int
	HSMPin             string
	LogLevel           string
	RateLimitRPS       int
	MaxTransactionSize int64
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
	if err := cfg.ValidateNodeConfig(); err != nil {
		return nil, err
	}

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
	if v := os.Getenv("SYNTHOS_NODE_ID"); v != "" {
		cfg.NodeID = v
	}
	if v := os.Getenv("SYNTHOS_PRIVATE_KEY"); v != "" {
		cfg.PrivateKey = v
	}
	if v := os.Getenv("SYNTHOS_HTTP_PEERS"); v != "" {
		cfg.HTTPPeers = splitCSV(v)
	}

	if v := os.Getenv("SYNTHOS_CONSENSUS_PEERS"); v != "" {
		cfg.ConsensusPeers = splitCSV(v)
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

	if v := os.Getenv("SYNTHOS_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}

	if v := os.Getenv("PORT"); v != "" {
		cfg.RPCListen = ":" + v
	}

	if v := os.Getenv("SYNTHOS_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
}

func splitCSV(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
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
		ChainID   string            `json:"chain_id"`
		TxChainID uint64            `json:"tx_chain_id"`
		Alloc     map[string]uint64 `json:"alloc"`
		Symbol    string            `json:"symbol"`
		Decimals  int               `json:"decimals"`
		Metadata  map[string]any    `json:"metadata"`
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
		ChainID:   raw.ChainID,
		TxChainID: raw.TxChainID,
		Alloc:     alloc,
		Metadata:  raw.Metadata,
	}
	if gen.Metadata == nil {
		gen.Metadata = map[string]any{}
	}
	if raw.Symbol != "" {
		gen.Metadata["symbol"] = raw.Symbol
	}
	if raw.Decimals != 0 {
		gen.Metadata["decimals"] = raw.Decimals
	}
	return gen, nil
}
