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
//	synthos-comet start --home DIR [--p2p ADDR] [--rpc ADDR] [--peers LIST]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cometbft/cometbft/libs/log"

	"synthos-collective/cometapp/app"
	"synthos-collective/cometapp/devnode"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	home := fs.String("home", "", "node home directory (required)")
	genesisFile := fs.String("genesis", "", "SYNTHOS app genesis JSON (init)")
	p2p := fs.String("p2p", "tcp://0.0.0.0:26656", "p2p listen address (start)")
	rpc := fs.String("rpc", "tcp://127.0.0.1:26657", "CometBFT RPC listen address, empty to disable (start)")
	peers := fs.String("peers", "", "persistent peers id@host:port,... (start)")
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
	case "start":
		n, _, err := devnode.Start(*home, devnode.Options{
			P2PAddr: *p2p, RPCAddr: *rpc, PersistentPeers: *peers,
			Logger: log.NewTMLogger(log.NewSyncWriter(os.Stdout)),
		})
		check(err)
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		_ = n.Stop()
		n.Wait()
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: synthos-comet validator-key|node-id|init|start --home DIR [flags]")
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
