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
	"fmt"
	"path/filepath"
	"time"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	nm "github.com/cometbft/cometbft/node"

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

	// DefaultFaucetAddress is the well-known developer address (Anvil/Hardhat #0) pre-funded for contracts.
	DefaultFaucetAddress = chain.Address("0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266")
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

// SetupGenesis initializes the devnet genesis file if missing.
func SetupGenesis(home string) error {
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

	return devnode.WriteGenesis(home, g, time.Now().UTC())
}

// StartNode initializes and starts the in-process EVM devnet node.
func StartNode(cfg Config) (*nm.Node, *app.App, error) {
	if cfg.HomeDir == "" {
		cfg.HomeDir = filepath.Join(".", "data-evm-devnet")
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
