package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
	"synthos-collective/internal/node"
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
			ChainID:   snap.ChainID,
			TxChainID: snap.TxChainID,
			State:     snap.State,
			DEX:       chain.NewDEX(),
			Oracle:    chain.NewOracle(),
			Blocks:    snap.Blocks,
			Mempool:   make(map[string]chain.Tx),
		}
	} else {
		// Load genesis from config/genesis.json if present, otherwise use defaults.
		gen := loadGenesis("config/genesis.json")
		c, err = chain.NewChain(gen)
		if err != nil {
			panic(err)
		}
		_ = st.Save(c)
		for addr, bal := range gen.Alloc {
			fmt.Printf("Funded %s with %d SYN\n", addr, bal)
		}
	}
	seedDEX(c)

	// Wire up relay transport if registry URL is configured.
	// This lets the Go node participate in the same network as
	// Self-hosted SYNTHOS validators and mobile PWA validators.
	var relay *network.RelayTransport
	registryURL := os.Getenv("REGISTRY_URL")
	registryURLs := os.Getenv("REGISTRY_URLS")
	selfName := os.Getenv("WORKER_NAME")
	selfURL := os.Getenv("SELF_URL")

	if (registryURL != "" || registryURLs != "") && selfName != "" {
		relay = network.NewRelayTransportFromConfig(network.RelayConfig{
			RegistryURL:    registryURL,
			RegistryURLs:   []string{registryURLs},
			SelfName:       selfName,
			SelfURL:        selfURL,
			RegistrySecret: os.Getenv("REGISTRY_SECRET"),
			Cloud:          "go-node",
			Logf:           log.Printf,
		})
		if err := relay.Start(); err != nil {
			log.Printf("WARNING: relay transport failed to start: %v", err)
		} else {
			log.Printf("Relay transport started: registry=%s registries=%s self=%s", registryURL, registryURLs, selfName)
		}
	}

	// Wire in a solo block-producing validator if a validator key is
	// configured. Without this, the RPC server can accept transactions into
	// the mempool but can never finalize them (ProposeBlockHash requires a
	// non-nil Node) -- the chain would accept payments and never deliver them.
	var n *node.Node
	if validatorNode, err := newSoloValidatorNode(c, st); err != nil {
		log.Printf("WARNING: block production disabled: %v", err)
	} else if validatorNode != nil {
		n = validatorNode
		if err := n.Start(); err != nil {
			log.Printf("WARNING: validator node failed to start, block production disabled: %v", err)
			n = nil
		} else {
			log.Printf("Block production enabled: node_id=%s (solo validator)", n.Agent.Identity.AgentID)
		}
	}

	srv := rpc.NewServer(c, st, n)
	_ = relay // relay runs independently; push-from-proposer model, no gossip needed

	addr := ":8080"
	port := os.Getenv("PORT")
	if port != "" {
		addr = ":" + port
	}
	fmt.Printf("RPC listening on %s\n", addr)
	fmt.Printf("GET  /health /status /account /balance /mempool /blocks /peers\n")
	fmt.Printf("POST /submitTx /proposeBlock /gossip/block /gossip/tx-batch\n")
	if registryURL != "" || registryURLs != "" {
		fmt.Printf("Registry: %s | Registries: %s | Self: %s (%s)\n", registryURL, registryURLs, selfName, selfURL)
	}
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		panic(err)
	}
}

func seedDEX(c *chain.Chain) {
	if c.DEX == nil {
		c.DEX = chain.NewDEX()
	}
	if len(c.DEX.ListPools()) > 0 {
		return
	}
	c.DEX.SeedPool("B12", 10_000_000, 50_000)
	c.DEX.SeedPool("NGOT", 5_000_000, 100_000)
	c.DEX.SeedPool("MOMENTUM", 2_000_000, 10_000)
}

func loadGenesis(path string) chain.Genesis {
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		var g chain.Genesis
		if json.NewDecoder(f).Decode(&g) == nil && g.Validate() == nil {
			return g
		}
	}
	// Fallback: default genesis with large agent-0 allocation.
	return chain.Genesis{
		ChainID: "synthos-l1-local",
		Alloc: map[chain.Address]uint64{
			"agent-0": 100_000_000_000,
		},
		Metadata: map[string]any{"symbol": "SYN", "decimals": 0},
	}
}

// newSoloValidatorNode builds a single-validator Node so this RPC instance can
// actually finalize blocks (propose, self-vote, finalize -- 1 validator only
// ever needs 1 vote for finality). Returns (nil, nil) if no validator key is
// configured, so the server falls back to the old read-only behavior.
func newSoloValidatorNode(c *chain.Chain, st *storage.Store) (*node.Node, error) {
	privHex := os.Getenv("SYNTHOS_VALIDATOR_PRIVATE_KEY")
	if privHex == "" {
		return nil, nil
	}
	nodeID := os.Getenv("SYNTHOS_NODE_ID")
	if nodeID == "" {
		nodeID = "synthos-rpc-1"
	}

	keys, err := validatorKeys(privHex)
	if err != nil {
		return nil, fmt.Errorf("invalid SYNTHOS_VALIDATOR_PRIVATE_KEY: %w", err)
	}

	a := agent.NewAgent(nodeID, "", "", "synthos-hw-"+nodeID, 0)
	a.AttachKeys(keys)

	// Solo node: no peers to gossip with, so a local-only transport is
	// sufficient. Finality is achieved locally (self-proposal + self-vote).
	bus := network.NewMemoryTransport()
	t := bus.NodeTransport(a.Identity.AgentID)
	a.AttachTransport(t)

	eng := consensus.NewEngine(1) // 1 total validator: itself.
	n := node.NewNode(a, c, eng, t)
	n.SetValidators([]string{a.Identity.AgentID})
	n.OnFinalize = func(chn *chain.Chain) error {
		return st.Save(chn)
	}
	return n, nil
}

func validatorKeys(privateKeyHex string) (synthoscrypto.KeyPair, error) {
	raw := strings.TrimPrefix(privateKeyHex, "0x")
	b, err := hex.DecodeString(raw)
	if err != nil {
		return synthoscrypto.KeyPair{}, err
	}
	if len(b) != ed25519.PrivateKeySize {
		return synthoscrypto.KeyPair{}, fmt.Errorf("private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(b))
	}
	priv := ed25519.PrivateKey(b)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return synthoscrypto.KeyPair{}, fmt.Errorf("failed to derive public key")
	}
	return synthoscrypto.KeyPair{Public: pub, Private: priv}, nil
}
