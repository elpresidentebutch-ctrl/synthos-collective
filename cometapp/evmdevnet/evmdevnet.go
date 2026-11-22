// Package evmdevnet provides tools and configuration to stand up an isolated,
// single-node or multi-node CometBFT EVM devnet for experiments.
//
// Invariants:
//   - CometBFT Chain ID: synthos-evm-devnet-1
//   - EVM / Tx Chain ID: 20260799
//   - NEVER 20260702 (mainnet) and NEVER touches mainnet validators or live state.
package evmdevnet

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	nm "github.com/cometbft/cometbft/node"
	cmttypes "github.com/cometbft/cometbft/types"

	"synthos-collective/cometapp/app"
	"synthos-collective/cometapp/devnode"
	"synthos-collective/internal/chain"
)

const (
	// DevnetChainID is the CometBFT consensus chain ID for this isolated devnet.
	DevnetChainID = "synthos-evm-devnet-1"

	// DevnetEVMChainID is the EVM/Tx Chain ID for experiments.
	// Invariant: This is NEVER 20260702 (mainnet) and NEVER touches live validators.
	DevnetEVMChainID uint64 = 20260799

	// DefaultFaucetAddress is the well-known public Hardhat/Anvil test account #0
	// (0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266), whose private key is publicly known
	// worldwide. Strictly for local isolated devnet (synthos-evm-devnet-1, EVM Chain ID 20260799)
	// experiments only — NEVER send real SYN/ETH to it and NEVER include it in mainnet or public
	// testnet genesis.
	DefaultFaucetAddress = chain.Address("0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266")

	// devnetMarkerFile identifies a directory created exclusively for the isolated EVM devnet.
	devnetMarkerFile = ".evm-devnet-marker"
)

// Config configures the isolated EVM devnet.
type Config struct {
	HomeDir       string
	RPCAddr       string
	P2PAddr       string
	BlockInterval time.Duration
	Logger        cmtlog.Logger
}

// DefaultConfig returns a safe, local-only configuration for the EVM devnet.
func DefaultConfig(home string) Config {
	return Config{
		HomeDir:       home,
		RPCAddr:       "tcp://127.0.0.1:26757",
		P2PAddr:       "tcp://127.0.0.1:26756",
		BlockInterval: 500 * time.Millisecond,
		Logger:        cmtlog.NewNopLogger(),
	}
}

// ValidateHomeDir refuses to initialize or start the isolated EVM devnet in any
// directory that belongs to mainnet, another network, an existing validator, or
// an unrelated non-empty folder.
func ValidateHomeDir(home string) error {
	if home == "" {
		return fmt.Errorf("devnet home directory cannot be empty")
	}

	genPath := filepath.Join(home, "config", "genesis.json")
	if _, err := os.Stat(genPath); err == nil {
		doc, err := cmttypes.GenesisDocFromFile(genPath)
		if err != nil {
			return fmt.Errorf("refusing devnet in %s: unreadable existing genesis.json: %w", home, err)
		}
		if doc.ChainID != DevnetChainID {
			return fmt.Errorf("refusing devnet in %s: existing genesis belongs to chain %q, expected %q", home, doc.ChainID, DevnetChainID)
		}
		var g app.Genesis
		if err := json.Unmarshal(doc.AppState, &g); err != nil {
			return fmt.Errorf("refusing devnet in %s: invalid app_state in genesis.json: %w", home, err)
		}
		if g.Chain.ChainID != DevnetChainID {
			return fmt.Errorf("refusing devnet in %s: app_state belongs to chain %q, expected %q", home, g.Chain.ChainID, DevnetChainID)
		}
		if rawID, ok := g.Chain.Metadata["tx_chain_id"]; ok {
			txID, okID := parseUint64Metadata(rawID)
			if !okID || txID == 20260702 || txID != DevnetEVMChainID {
				return fmt.Errorf("refusing devnet in %s: unexpected tx_chain_id %v (must be %d, never mainnet 20260702)", home, rawID, DevnetEVMChainID)
			}
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking genesis in %s: %w", home, err)
	}

	// Protect directories that already hold validator keys, signing state, or chain state
	// even if genesis.json is absent.
	protected := []string{
		filepath.Join(home, "config", "priv_validator_key.json"),
		filepath.Join(home, "data", "priv_validator_state.json"),
		filepath.Join(home, "synthos", "synthos-app-state.json"),
	}
	for _, p := range protected {
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("refusing devnet in %s: existing validator/state file %s detected", home, p)
		}
	}

	// Refuse any non-empty directory that was not initialized by evmdevnet.
	entries, err := os.ReadDir(home)
	if err == nil && len(entries) > 0 {
		markerPath := filepath.Join(home, devnetMarkerFile)
		if _, markerErr := os.Stat(markerPath); markerErr != nil {
			return fmt.Errorf("refusing devnet in %s: directory is non-empty and lacks %s", home, devnetMarkerFile)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspecting devnet home %s: %w", home, err)
	}

	return nil
}

func parseUint64Metadata(v any) (uint64, bool) {
	switch x := v.(type) {
	case uint64:
		return x, true
	case int64:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	case float64:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	case json.Number:
		u, err := strconv.ParseUint(x.String(), 10, 64)
		return u, err == nil
	default:
		return 0, false
	}
}

// SetupGenesis initializes the devnet genesis file if missing.
func SetupGenesis(home string) error {
	if err := ValidateHomeDir(home); err != nil {
		return err
	}
	if devnode.Exists(home) {
		return nil
	}

	valKey, err := devnode.ValidatorKey(home)
	if err != nil {
		return fmt.Errorf("creating validator key: %w", err)
	}

	// Generate an operator address for the devnet validator
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generating operator key: %w", err)
	}
	operatorAddr := chain.AddressFromPublicKey(pub)

	alloc := map[chain.Address]uint64{
		operatorAddr:         10_000_000_000,
		DefaultFaucetAddress: 5_000_000_000,
	}

	g := app.Genesis{
		Chain: chain.Genesis{
			ChainID: DevnetChainID,
			Alloc:   alloc,
			Metadata: map[string]any{
				"tx_chain_id": DevnetEVMChainID,
				"symbol":      "SYN",
				"decimals":    0,
			},
		},
		Staking: chain.ValidatorStakingParams{
			EnabledFromHeight: 1,
			MinSelfBond:       1_000,
			UnbondingBlocks:   50,
			EpochBlocks:       10,
			SlashFractionBps:  500,
			ReporterRewardBps: 1_000,
		},
		Validators: []app.GenesisValidator{
			{
				Operator:        operatorAddr,
				ConsensusPubKey: valKey,
				SelfBond:        1_000_000,
				Moniker:         "evm-devnet-validator-1",
			},
		},
	}

	if err := devnode.WriteGenesis(home, g, time.Now().UTC()); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, devnetMarkerFile), []byte(DevnetChainID+"\n"), 0o600)
}

// StartNode initializes and starts the in-process EVM devnet node.
func StartNode(cfg Config) (*nm.Node, *app.App, error) {
	if cfg.HomeDir == "" {
		cfg.HomeDir = filepath.Join(".", "data-evm-devnet")
	}

	if err := ValidateHomeDir(cfg.HomeDir); err != nil {
		return nil, nil, err
	}

	if err := SetupGenesis(cfg.HomeDir); err != nil {
		return nil, nil, fmt.Errorf("setting up genesis: %w", err)
	}

	// Ensure config directory exists
	c := cmtcfg.DefaultConfig().SetRoot(cfg.HomeDir)
	cmtcfg.EnsureRoot(cfg.HomeDir)
	_ = c

	opts := devnode.Options{
		RPCAddr:            cfg.RPCAddr,
		P2PAddr:            cfg.P2PAddr,
		LocalNetwork:       true,
		Moniker:            "synthos-evm-devnode-1",
		BlockInterval:      cfg.BlockInterval,
		EmptyBlockInterval: cfg.BlockInterval,
		Logger:             cfg.Logger,
	}

	return devnode.Start(cfg.HomeDir, opts)
}
