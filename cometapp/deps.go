// Package main will become the SYNTHOS node running on CometBFT: CometBFT
// handles networking, voting and block-producer rotation, and the SYNTHOS
// chain rules (internal/chain) run as its application.
//
// It lives in its own Go module so that nothing it depends on can affect
// the live synthosd build (the Dockerfile Render deploys builds only the
// root module). For now this file just names the CometBFT packages the
// application will use, so the GitLab cometapp-deps job can resolve and
// vendor them.
package main

import (
	_ "github.com/cometbft/cometbft/abci/server"
	_ "github.com/cometbft/cometbft/abci/types"
	_ "github.com/cometbft/cometbft/cmd/cometbft/commands"
	_ "github.com/cometbft/cometbft/config"
	_ "github.com/cometbft/cometbft/crypto/ed25519"
	_ "github.com/cometbft/cometbft/libs/log"
	_ "github.com/cometbft/cometbft/node"
	_ "github.com/cometbft/cometbft/p2p"
	_ "github.com/cometbft/cometbft/privval"
	_ "github.com/cometbft/cometbft/proxy"
	_ "github.com/cometbft/cometbft/rpc/client/http"
	_ "github.com/cometbft/cometbft/rpc/client/local"
	_ "github.com/cometbft/cometbft/types"

	_ "synthos-collective/internal/chain"
)

func main() {}
