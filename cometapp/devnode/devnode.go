// Package devnode sets up and starts a CometBFT node that runs the
// SYNTHOS application in the same process. The command-line tool and the
// tests both use it.
package devnode

import (
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

// Options tune a node for where it runs.
type Options struct {
	// P2PAddr / RPCAddr are listen addresses like "tcp://0.0.0.0:26656".
	// An empty RPCAddr disables CometBFT's HTTP RPC server.
	P2PAddr string
	RPCAddr string
	// PersistentPeers is CometBFT's "id@host:port,..." list.
	PersistentPeers string
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
	c.P2P.AllowDuplicateIP = true // several local nodes share 127.0.0.1
	c.P2P.AddrBookStrict = false
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
