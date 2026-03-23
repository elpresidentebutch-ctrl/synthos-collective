package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"synthos-collective/internal/chain"
)

// This is a minimal L1 node skeleton. It does not open inbound ports.
// Networking/consensus will be driven by the agent + relay transport layer.
func main() {
	// Create a demo key (wallet integration will replace this).
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	addr := chain.AddressFromPublicKey(pub)

	// Genesis: allocate 100B SYN (native coin) to this address for now.
	// (You’ll likely replace this with a treasury address + multiple allocations.)
	gen := chain.Genesis{
		ChainID: "synthos-l1-devnet",
		Alloc: map[chain.Address]uint64{
			addr: 100_000_000_000,
		},
		Metadata: map[string]any{
			"symbol":   "SYN",
			"decimals": 0,
		},
	}

	c, err := chain.NewChain(gen)
	if err != nil {
		panic(err)
	}

	// Create and sign a tx to another address.
	recvPub, _, _ := ed25519.GenerateKey(rand.Reader)
	recv := chain.AddressFromPublicKey(recvPub)

	tx := chain.Tx{
		From:      addr,
		To:        recv,
		Amount:    25,
		Fee:       0,
		Nonce:     0,
		Timestamp: time.Now().UTC(),
		PublicKey: "0x" + fmt.Sprintf("%x", pub),
	}
	_ = tx.Sign(priv)
	_ = c.SubmitTx(tx)

	// Build and finalize a block locally (consensus will replace this).
	b, _ := c.BuildBlock("agent-demo", "", 100)
	_ = c.FinalizeBlock(b)

	out := map[string]any{
		"chain_id": gen.ChainID,
		"height":   c.Height(),
		"tip":      c.Tip().Hash,
		"state_root": c.State.Root(),
		"balances": map[string]any{
			string(addr): c.State.Get(addr).Balance,
			string(recv): c.State.Get(recv).Balance,
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

