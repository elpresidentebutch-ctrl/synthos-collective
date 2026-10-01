// Package devnode sets up and starts a CometBFT node that runs the
// SYNTHOS application in the same process. The command-line tool and the
// tests both use it.
package devnode

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	cfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/libs/log"
	nm "github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cometbft/cometbft/types"

	"synthos-collective/cometapp/app"
)

// appDataDir is where the SYNTHOS app keeps its committed state inside a
// CometBFT home directory.
const appDataDir = "synthos"

// ValidatorKey creates (or loads) this home's consensus key and returns
// its public key as 0x hex, the form SYNTHOS genesis and bond transactions
// use.
func ValidatorKey(home string) (string, error) {
	c := cfg.DefaultConfig().SetRoot(home)
	cfg.EnsureRoot(home)
	pv := privval.LoadOrGenFilePV(c.PrivValidatorKeyFile(), c.PrivValidatorStateFile())
	pub, err := pv.GetPubKey()
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(pub.Bytes()), nil
}

// WriteGenesis writes home's genesis.json for a chain whose app_state is
// g. The CometBFT validator list mirrors g.Validators (the app also
// returns it from InitChain, which is what CometBFT actually uses).
func WriteGenesis(home string, g app.Genesis, genesisTime time.Time) error {
	if err := g.Validate(); err != nil {
		return err
	}
	appState, err := json.Marshal(g)
	if err != nil {
		return err
	}
	vals := make([]types.GenesisValidator, 0, len(g.Validators))
	for _, v := range g.Validators {
		pub, err := hex.DecodeString(v.ConsensusPubKey[2:])
		if err != nil {
			return fmt.Errorf("validator %s: %w", v.Operator, err)
		}
		pk := ed25519PubKey(pub)
		vals = append(vals, types.GenesisValidator{Address: pk.Address(), PubKey: pk, Power: int64(v.SelfBond), Name: v.Moniker})
	}
	doc := &types.GenesisDoc{
		GenesisTime:     genesisTime,
		ChainID:         g.Chain.ChainID,
		InitialHeight:   1,
		ConsensusParams: types.DefaultConsensusParams(),
		Validators:      vals,
		AppState:        appState,
	}
	if err := doc.ValidateAndComplete(); err != nil {
		return err
	}
	c := cfg.DefaultConfig().SetRoot(home)
	cfg.EnsureRoot(home)
	return doc.SaveAs(c.GenesisFile())
}

// InstallGenesis copies an existing network's genesis.json into home, so
// this node joins that network. Every node of a network must use the
// byte-identical file (its hash is part of the chain's identity), which
// is why joining copies it rather than rebuilding it.
func InstallGenesis(home, file string) (*types.GenesisDoc, error) {
	doc, err := types.GenesisDocFromFile(file)
	if err != nil {
		return nil, err
	}
	var g app.Genesis
	if err := json.Unmarshal(doc.AppState, &g); err != nil {
		return nil, fmt.Errorf("%s is not a SYNTHOS network genesis: %w", file, err)
	}
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if g.Chain.ChainID != doc.ChainID {
		return nil, fmt.Errorf("%s: app_state chain_id %q differs from %q", file, g.Chain.ChainID, doc.ChainID)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	c := cfg.DefaultConfig().SetRoot(home)
	cfg.EnsureRoot(home)
	if existing, err := os.ReadFile(c.GenesisFile()); err == nil && !bytes.Equal(existing, raw) {
		if _, statErr := os.Stat(filepath.Join(home, appDataDir, "synthos-app-state.json")); statErr == nil {
			return nil, fmt.Errorf("%s already runs a different chain; use a new home directory", home)
		}
	}
	return doc, os.WriteFile(c.GenesisFile(), raw, 0o644)
}

// Options tune a node for where it runs.
type Options struct {
	// P2PAddr / RPCAddr are listen addresses like "tcp://0.0.0.0:26656".
	// An empty RPCAddr disables CometBFT's HTTP RPC server.
	P2PAddr string
	RPCAddr string
	// PersistentPeers is CometBFT's "id@host:port,..." list: nodes this
	// one always stays connected to (and redials if they drop).
	PersistentPeers string
	// ExternalAddress is the "host:port" other nodes should dial to
	// reach this one, for a node with a public address (an entry node).
	ExternalAddress string
	// Seeds is an "id@host:port,..." list of nodes to learn peers from.
	Seeds string
	// PrivatePeerIDs are node IDs this node never tells other peers about
	// (an entry node keeps its validators' addresses private this way).
	PrivatePeerIDs string
	// NoPeerExchange turns off peer discovery: the node talks only to
	// PersistentPeers. Validators behind an entry node run this way.
	NoPeerExchange bool
	// LocalNetwork relaxes address checks so several nodes can share
	// 127.0.0.1 (tests and single-machine networks).
	LocalNetwork bool
	// EmptyBlockInterval is how often a block is made when there are no
	// transactions (0: every round, about once a second). Transactions
	// are still included as soon as they arrive.
	EmptyBlockInterval time.Duration
	// Moniker names this node to its peers.
	Moniker string
	// BlockInterval shortens CometBFT's wait between blocks (0 keeps the
	// default of about one second). Tests use a few hundred milliseconds.
	BlockInterval time.Duration
	Logger        log.Logger
}

// Start creates a CometBFT node for home running the SYNTHOS app and
// starts it. The caller stops it with node.Stop() and node.Wait().
func Start(home string, opts Options) (*nm.Node, *app.App, error) {
	c := cfg.DefaultConfig().SetRoot(home)
	cfg.EnsureRoot(home)
	if opts.P2PAddr != "" {
		c.P2P.ListenAddress = opts.P2PAddr
	}
	c.RPC.ListenAddress = opts.RPCAddr
	c.P2P.PersistentPeers = opts.PersistentPeers
	c.P2P.ExternalAddress = opts.ExternalAddress
	c.P2P.Seeds = opts.Seeds
	c.P2P.PrivatePeerIDs = opts.PrivatePeerIDs
	c.P2P.PexReactor = !opts.NoPeerExchange
	if opts.LocalNetwork {
		c.P2P.AllowDuplicateIP = true // several local nodes share 127.0.0.1
		c.P2P.AddrBookStrict = false
	}
	if opts.EmptyBlockInterval > 0 {
		c.Consensus.CreateEmptyBlocksInterval = opts.EmptyBlockInterval
	}
	if opts.Moniker != "" {
		c.Moniker = opts.Moniker
	}
	if opts.BlockInterval > 0 {
		c.Consensus.TimeoutCommit = opts.BlockInterval
		c.Consensus.TimeoutPropose = 4 * opts.BlockInterval
		c.Consensus.TimeoutPrevote = opts.BlockInterval
		c.Consensus.TimeoutPrecommit = opts.BlockInterval
		// CometBFT v0.38 has a shutdown race: a per-peer goroutine wakes
		// every PeerQueryMaj23SleepDuration and reads the block store,
		// and it can wake after Stop has closed that store and panic.
		// A real node process exits on stop, so it never matters there,
		// but tests stop nodes inside one process. Keeping the goroutine
		// asleep for the life of a test avoids that crash; the query is
		// only a rarely needed liveness aid.
		c.Consensus.PeerQueryMaj23SleepDuration = time.Hour
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.NewNopLogger()
	}

	a, err := app.New(filepath.Join(home, appDataDir))
	if err != nil {
		return nil, nil, err
	}
	pv := privval.LoadOrGenFilePV(c.PrivValidatorKeyFile(), c.PrivValidatorStateFile())
	nodeKey, err := p2p.LoadOrGenNodeKey(c.NodeKeyFile())
	if err != nil {
		return nil, nil, err
	}
	n, err := nm.NewNode(c, pv, nodeKey,
		proxy.NewLocalClientCreator(a),
		nm.DefaultGenesisDocProviderFunc(c),
		cfg.DefaultDBProvider,
		nm.DefaultMetricsProvider(c.Instrumentation),
		logger,
	)
	if err != nil {
		return nil, nil, err
	}
	if err := n.Start(); err != nil {
		return nil, nil, err
	}
	return n, a, nil
}

// NodeID returns the p2p ID CometBFT peers use to address this home's
// node ("<id>@host:port").
func NodeID(home string) (string, error) {
	c := cfg.DefaultConfig().SetRoot(home)
	cfg.EnsureRoot(home)
	nk, err := p2p.LoadOrGenNodeKey(c.NodeKeyFile())
	if err != nil {
		return "", err
	}
	return string(nk.ID()), nil
}

// Exists reports whether home already has a genesis file.
func Exists(home string) bool {
	_, err := os.Stat(filepath.Join(home, "config", "genesis.json"))
	return err == nil
}
