// synthos-comet runs a SYNTHOS node on CometBFT: CometBFT handles
// networking, voting and block-producer rotation; the SYNTHOS chain rules
// (internal/chain) run as its application.
//
// Usage:
//
//	synthos-comet validator-key --home DIR   create/show this node's consensus key
//	synthos-comet node-id --home DIR         show this node's p2p id
//	synthos-comet init --home DIR --genesis FILE
//	                                         write CometBFT genesis from a SYNTHOS
//	                                         app genesis (see app.Genesis)
//	synthos-comet join --home DIR --genesis-file FILE
//	                                         set up a node for an existing network
//	                                         from its genesis.json
//	synthos-comet start --home DIR [--p2p ADDR] [--rpc ADDR] [--peers LIST]
//	                    [--external-address HOST:PORT] [--seeds LIST]
//	                    [--private-peer-ids IDS] [--no-pex] [--moniker NAME]
//	                    [--empty-block-interval DUR] [--log-level LEVEL]
//	                    [--api ADDR] [--trust-proxy]
//	                                         run the node; --api also serves the
//	                                         original synthosd HTTP API
//	                                         (/status, /account, /submitTx, ...)
//	synthos-comet faucet-key --out FILE      make (or show) a test network faucet key
//	synthos-comet export-genesis --legacy PATH --chain-id ID --tx-chain-id N
//	                    --validators FILE --out FILE [--staking FILE]
//	                    [--legacy-genesis FILE] [--mainnet-switch]
//	                                         build a new chain's genesis from a
//	                                         legacy synthosd node's saved state,
//	                                         keeping every account and balance
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	cmtflags "github.com/cometbft/cometbft/libs/cli/flags"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/rpc/client/local"

	"synthos-collective/cometapp/app"
	"synthos-collective/cometapp/devnode"
	"synthos-collective/cometapp/export"
	"synthos-collective/cometapp/legacyapi"
	"synthos-collective/internal/chain"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	if cmd == "export-genesis" {
		exportGenesis(args)
		return
	}
	if cmd == "faucet-key" {
		faucetKey(args)
		return
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	home := fs.String("home", "", "node home directory (required)")
	genesisFile := fs.String("genesis", "", "SYNTHOS app genesis JSON (init)")
	p2p := fs.String("p2p", "tcp://0.0.0.0:26656", "p2p listen address (start)")
	rpc := fs.String("rpc", "tcp://127.0.0.1:26657", "CometBFT RPC listen address, empty to disable (start)")
	peers := fs.String("peers", "", "persistent peers id@host:port,... (start)")
	api := fs.String("api", "", "listen address for the synthosd-compatible HTTP API, e.g. 0.0.0.0:8080; empty disables it (start)")
	trustProxy := fs.Bool("trust-proxy", false, "rate-limit API clients by X-Forwarded-For; only behind a proxy that sets it (start)")
	external := fs.String("external-address", "", "public host:port other nodes dial to reach this one (start)")
	seeds := fs.String("seeds", "", "seed nodes id@host:port,... to discover peers from (start)")
	privateIDs := fs.String("private-peer-ids", "", "node IDs never gossiped to other peers, e.g. validators behind this entry node (start)")
	noPex := fs.Bool("no-pex", false, "no peer discovery: talk only to --peers (validators behind an entry node) (start)")
	moniker := fs.String("moniker", "", "this node's name shown to peers (start)")
	localNet := fs.Bool("local", false, "allow several nodes on one machine / private addresses (start)")
	emptyEvery := fs.Duration("empty-block-interval", 10*time.Second, "make a block at least this often when idle; transactions are still included at once; 0 = every second (start)")
	logLevel := fs.String("log-level", "main:info,*:error", "log detail, e.g. info, debug, or main:info,*:error (start)")
	genesisPath := fs.String("genesis-file", "", "the network's genesis.json to join (join)")
	faucetKeyFile := fs.String("faucet-key", "", "test networks only: serve /faucet, paying from this key file (start)")
	faucetAmount := fs.Uint64("faucet-amount", 10_000, "SYN per faucet request (start)")
	_ = fs.Parse(args)
	if *home == "" {
		fail("--home is required")
	}

	switch cmd {
	case "validator-key":
		key, err := devnode.ValidatorKey(*home)
		check(err)
		fmt.Println(key)
	case "node-id":
		id, err := devnode.NodeID(*home)
		check(err)
		fmt.Println(id)
	case "init":
		if *genesisFile == "" {
			fail("--genesis is required")
		}
		raw, err := os.ReadFile(*genesisFile)
		check(err)
		var g app.Genesis
		check(json.Unmarshal(raw, &g))
		check(devnode.WriteGenesis(*home, g, time.Now().UTC()))
		fmt.Println("wrote", *home+"/config/genesis.json")
	case "join":
		if *genesisPath == "" {
			fail("--genesis-file is required")
		}
		doc, err := devnode.InstallGenesis(*home, *genesisPath)
		check(err)
		id, err := devnode.NodeID(*home)
		check(err)
		fmt.Printf("joined %s\nnode id %s\n", doc.ChainID, id)
	case "start":
		logger, err := cmtflags.ParseLogLevel(*logLevel, log.NewTMLogger(log.NewSyncWriter(os.Stdout)), "info")
		check(err)
		mainLog := logger.With("module", "main")
		n, a, err := devnode.Start(*home, devnode.Options{
			P2PAddr: *p2p, RPCAddr: *rpc, PersistentPeers: *peers, Logger: logger,
			ExternalAddress: *external, Seeds: *seeds, PrivatePeerIDs: *privateIDs,
			NoPeerExchange: *noPex, Moniker: *moniker, LocalNetwork: *localNet,
			EmptyBlockInterval: *emptyEvery,
		})
		check(err)
		var srv *http.Server
		if *api != "" {
			srvAPI := &legacyapi.Server{App: a, Comet: local.New(n), TrustForwardedFor: *trustProxy, DataDir: *home}
			if *faucetKeyFile != "" {
				key, addr, err := readFaucetKey(*faucetKeyFile)
				check(err)
				srvAPI.Faucet = &legacyapi.Faucet{Key: key, Amount: *faucetAmount}
				mainLog.Info("faucet enabled", "address", addr, "amount", *faucetAmount)
			}
			h := srvAPI.Handler()
			warmCtx, stopWarm := context.WithCancel(context.Background())
			defer stopWarm()
			go srvAPI.WarmActivity(warmCtx)
			srv = &http.Server{Addr: *api, Handler: h, ReadHeaderTimeout: 10 * time.Second}
			go func() {
				if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					fmt.Fprintln(os.Stderr, "api server:", err)
					os.Exit(1)
				}
			}()
			mainLog.Info("synthosd-compatible API listening", "addr", *api)
		}
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		if srv != nil {
			_ = srv.Close()
		}
		_ = n.Stop()
		n.Wait()
	default:
		usage()
	}
}

// DefaultStaking are the validator rules export-genesis uses when no
// --staking file is given. Amounts are whole SYN. Heights are in blocks:
// one a second while transactions flow, one per --empty-block-interval
// when idle, so 86,400 blocks is between one and about ten days.
var DefaultStaking = chain.ValidatorStakingParams{
	EnabledFromHeight: 1,
	MinSelfBond:       10_000,
	UnbondingBlocks:   86_400,
	EpochBlocks:       100,
	SlashFractionBps:  500,   // 5% for double-signing
	ReporterRewardBps: 1_000, // 10% of a slash to whoever proves it
}

func exportGenesis(args []string) {
	fs := flag.NewFlagSet("export-genesis", flag.ExitOnError)
	legacy := fs.String("legacy", "", "legacy node data directory (state.json + blocks/) or a state.json file (required)")
	chainID := fs.String("chain-id", "", "the new chain's ID, e.g. synthos-testnet-1 (required)")
	txChainID := fs.Uint64("tx-chain-id", 0, "transaction chain ID wallets sign with on the new chain (required)")
	validatorsFile := fs.String("validators", "", "JSON list of genesis validators: operator, consensus_pub_key, self_bond, moniker (required)")
	stakingFile := fs.String("staking", "", "JSON validator staking rules (default: built-in rules, printed below)")
	legacyGenesisFile := fs.String("legacy-genesis", "", "the legacy chain's genesis.json, to restore treasury/founder/reward settings a saved state lost")
	mainnetSwitch := fs.Bool("mainnet-switch", false, "keep the legacy transaction chain ID (only for replacing the old mainnet)")
	out := fs.String("out", "", "where to write the new app genesis JSON (required)")
	faucetAddr := fs.String("faucet", "", "test networks: fund this faucet address at genesis")
	faucetFund := fs.Uint64("faucet-amount", 0, "SYN to move to the faucet")
	faucetFrom := fs.String("faucet-from", "", "account the faucet's SYN are moved from")
	_ = fs.Parse(args)
	if *legacy == "" || *chainID == "" || *txChainID == 0 || *validatorsFile == "" || *out == "" {
		fail("export-genesis needs --legacy, --chain-id, --tx-chain-id, --validators and --out")
	}

	l, err := export.Load(*legacy)
	check(err)
	before, err := export.Summarize(l.State)
	check(err)

	o := export.Options{ChainID: *chainID, TxChainID: *txChainID, AllowSameTxChainID: *mainnetSwitch, Staking: DefaultStaking}
	readJSON(*validatorsFile, &o.Validators)
	if *stakingFile != "" {
		readJSON(*stakingFile, &o.Staking)
	}
	if *legacyGenesisFile != "" {
		var lg chain.Genesis
		readJSON(*legacyGenesisFile, &lg)
		o.LegacyGenesis = &lg
	}
	if *faucetAddr != "" {
		o.Faucet = &export.FaucetFunding{Address: chain.Address(*faucetAddr), Amount: *faucetFund, From: chain.Address(*faucetFrom)}
	}
	g, err := export.Genesis(l, o)
	check(err)
	appHash, err := export.Check(g)
	check(err)
	after, err := export.Summarize(g.InitialState)
	check(err)
	newAccounts := 0
	if o.Faucet != nil && l.State.Get(o.Faucet.Address).Balance == 0 {
		newAccounts = 1 // the faucet's account is new
	}
	if after.Held != before.Held || after.Accounts != before.Accounts+newAccounts {
		fail(fmt.Sprintf("totals changed during export (before %+v, after %+v)", before, after))
	}
	raw, err := json.MarshalIndent(g, "", "  ")
	check(err)
	check(os.WriteFile(*out, raw, 0o644))

	verified := "NOT VERIFIED: no saved blocks next to the state to compare against"
	if l.TipStateRoot != "" {
		verified = fmt.Sprintf("NOT VERIFIED: hashes to %s but the newest saved block (height %d) says %s", before.StateRoot, l.Height, l.TipStateRoot)
	}
	if l.Verified {
		verified = fmt.Sprintf("verified: matches the chain's state root at height %d", l.Height)
	}
	stakingJSON, _ := json.Marshal(o.Staking)
	fmt.Printf(`Legacy chain   %s (tx chain ID %d), height %d
  state        %s
  accounts     %d
  balances     %d SYN
  citizen stake %d SYN
  total held   %d SYN of %d max (%d burned or never issued)
  treasury     %s = %d SYN
  founder      %s
  citizen rate %d bps/year
New chain      %s (tx chain ID %d)
  validators   %d
  staking      %s
  app hash     %s
  faucet       %s
Wrote %s
`, l.ChainID, l.TxChainID, l.Height, verified, before.Accounts, before.Balances, before.CitizenStaked,
		before.Held, before.MaxSupply, before.Burned, after.Treasury, after.TreasuryBalance, after.Founder,
		after.CitizenRateBps, g.Chain.ChainID, g.Chain.TxChainID, len(g.Validators), stakingJSON, appHash, faucetLine(o.Faucet), *out)
	if after.Treasury == "" || after.Founder == "" || after.CitizenRateBps == 0 {
		fmt.Fprintln(os.Stderr, "WARNING: treasury, founder or citizen rate is empty; pass --legacy-genesis to restore them")
	}
}

type faucetKeyFileJSON struct {
	Address    chain.Address `json:"address"`
	PublicKey  string        `json:"public_key"`
	PrivateKey string        `json:"private_key"`
	Note       string        `json:"note"`
}

func faucetKey(args []string) {
	fs := flag.NewFlagSet("faucet-key", flag.ExitOnError)
	out := fs.String("out", "", "key file to create, or to read if it exists (required)")
	_ = fs.Parse(args)
	if *out == "" {
		fail("--out is required")
	}
	if _, err := os.Stat(*out); err == nil {
		_, addr, err := readFaucetKey(*out)
		check(err)
		fmt.Println(addr)
		return
	}
	key, addr, err := legacyapi.NewFaucetKey()
	check(err)
	raw, err := json.MarshalIndent(faucetKeyFileJSON{
		Address: addr, PublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)),
		PrivateKey: hex.EncodeToString(key), Note: "Test network faucet key. Never send real SYN to this address.",
	}, "", "  ")
	check(err)
	check(os.WriteFile(*out, raw, 0o600))
	fmt.Println(addr)
}

func readFaucetKey(path string) (ed25519.PrivateKey, chain.Address, error) {
	var f faucetKeyFileJSON
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	b, err := hex.DecodeString(f.PrivateKey)
	if err != nil || len(b) != ed25519.PrivateKeySize {
		return nil, "", fmt.Errorf("%s: bad private key", path)
	}
	key := ed25519.PrivateKey(b)
	addr := chain.AddressFromPublicKey(key.Public().(ed25519.PublicKey))
	if f.Address != "" && f.Address != addr {
		return nil, "", fmt.Errorf("%s: address does not match the key", path)
	}
	return key, addr, nil
}

func faucetLine(f *export.FaucetFunding) string {
	if f == nil {
		return "none"
	}
	return fmt.Sprintf("%s gets %d SYN moved from %s", f.Address, f.Amount, f.From)
}

func readJSON(path string, v any) {
	raw, err := os.ReadFile(path)
	check(err)
	if err := json.Unmarshal(raw, v); err != nil {
		fail(fmt.Sprintf("%s: %v", path, err))
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: synthos-comet validator-key|node-id|init|start --home DIR [flags]")
	fmt.Fprintln(os.Stderr, "       synthos-comet export-genesis --legacy PATH --chain-id ID --tx-chain-id N --validators FILE --out FILE")
	os.Exit(2)
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(2)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
