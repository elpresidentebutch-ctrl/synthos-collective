package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net/http"
	"os"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/rpc"
	"synthos-collective/internal/storage"
)

func main() {
	dataDir := os.Getenv("SYNTHOS_DATA_DIR")
	if dataDir == "" {
		dataDir = ".synthos-data"
	}
	st, err := storage.New(dataDir)
	if err != nil {
		panic(err)
	}

	// Try load existing chain snapshot.
	var c *chain.Chain
	if snap, err := st.Load(); err == nil && snap != nil && len(snap.Blocks) > 0 && snap.State != nil {
		c = &chain.Chain{
			ChainID:  snap.ChainID,
			State:    snap.State,
			Blocks:   snap.Blocks,
			Mempool:  make(map[string]chain.Tx),
		}
	} else {
		// Fresh chain with demo genesis.
		pub, _, _ := ed25519.GenerateKey(rand.Reader)
		addr := chain.AddressFromPublicKey(pub)
		gen := chain.Genesis{
			ChainID: "synthos-l1-local",
			Alloc: map[chain.Address]uint64{
				addr: 100_000_000_000,
			},
			Metadata: map[string]any{"symbol": "SYN", "decimals": 0},
		}
		c, err = chain.NewChain(gen)
		if err != nil {
			panic(err)
		}
		_ = st.Save(c)
		fmt.Printf("Initialized new chain, funded %s\n", addr)
	}

	srv := rpc.NewServer(c, st, nil)
	addr := ":8080"
	fmt.Printf("RPC listening on %s\n", addr)
	fmt.Printf("GET /status\n")
	fmt.Printf("GET /balance?address=0x...\n")
	fmt.Printf("GET /mempool\n")
	fmt.Printf("POST /submitTx (JSON body)\n")
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		panic(err)
	}
}

