package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/config"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
	"synthos-collective/internal/node"
	"synthos-collective/internal/rpc"
	"synthos-collective/internal/storage"
)

// synthosd is a configurable node binary meant to be the long-lived process
// operators run. Networking is still limited (no public P2P), but config +
// genesis + RPC wiring are in place so that the chain is "ready to decentralize"
// once a real transport is plugged in.
func main() {
	cfgPath := os.Getenv("SYNTHOS_CONFIG")
	if cfgPath == "" {
		cfgPath = "config/node.json"
	}
	cfg, err := config.LoadNodeConfig(cfgPath)
	if err != nil {
		panic(err)
	}

	gen, err := config.LoadGenesis(cfg.GenesisPath)
	if err != nil {
		panic(err)
	}

	dataDir := cfg.DataDir
	st, err := storage.New(dataDir)
	if err != nil {
		panic(err)
	}

	// Initialize or load chain.
	var ch *chain.Chain
	if snap, err := st.Load(); err == nil && snap != nil && len(snap.Blocks) > 0 && snap.State != nil {
		genesisChain, err := chain.NewChain(gen)
		if err != nil {
			panic(err)
		}
		if shouldRefreshHeightZeroSnapshot(snap, genesisChain) {
			ch = genesisChain
			_ = st.Save(ch)
		} else {
			ch = &chain.Chain{
				ChainID:   snap.ChainID,
				TxChainID: snap.TxChainID,
				State:     snap.State,
				DEX:       chain.NewDEX(),
				Oracle:    chain.NewOracle(),
				Blocks:    snap.Blocks,
				Mempool:   make(map[string]chain.Tx),
			}
		}
	} else {
		ch, err = chain.NewChain(gen)
		if err != nil {
			panic(err)
		}
		// Ensure ChainID matches genesis when bootstrapping.
		ch.ChainID = gen.ChainID
		_ = st.Save(ch)
	}

	// Agent + keys.
	keys, err := nodeKeys(cfg.PrivateKey, dataDir)
	if err != nil {
		panic(err)
	}
	a := agent.NewAgent(cfg.NodeID, "", "", "synthos-hw-"+cfg.NodeID, 0)
	a.AttachKeys(keys)

	// Use TCP transport so multiple synthosd instances can talk across processes.
	t := network.NewTCPTransport(a.Identity.AgentID, cfg.ListenAddr, cfg.Peers)
	a.AttachTransport(t)

	validators := cfg.Validators
	if len(validators) == 0 && cfg.IsValidator {
		validators = []string{a.Identity.AgentID}
	}
	totalValidators := len(validators)
	if totalValidators == 0 {
		totalValidators = 1
	}
	eng := consensus.NewEngine(totalValidators)
	n := node.NewNode(a, ch, eng, t)
	n.OnFinalize = func(c *chain.Chain) error {
		return st.Save(c)
	}
	bootstrapImmuneNode(cfg, ch, st, a, keys.Public)

	if len(validators) > 0 {
		n.SetValidators(validators)
	}
	for peerID, pubKey := range cfg.PeerKeys {
		if err := n.AddPeer(peerID, pubKey); err != nil {
			panic(err)
		}
	}
	if err := n.Start(); err != nil {
		panic(err)
	}

	// Expose RPC for status, balances, tx submission, and on-demand block proposals.
	srv := rpc.NewServer(ch, st, n)
	srv.SetPeerURLs(cfg.HTTPPeers)
	srv.StartPeerSync(15 * time.Second)
	startRegistryHeartbeat(cfg.NodeID, ch.ChainID, keys.Public)
	startBlockProducer(n, ch)
	fmt.Printf("synthosd: RPC listening on %s (data dir %s, node_id=%s)\n", cfg.RPCListen, dataDir, cfg.NodeID)
	if err := http.ListenAndServe(cfg.RPCListen, srv.Handler()); err != nil {
		panic(err)
	}
}

// startBlockProducer runs the automatic block-proposal loop on the single
// designated sequencer. Enable it on exactly ONE validator via
// SYNTHOS_BLOCK_PRODUCER=true; the others follow via HTTP peer catch-up. It
// proposes and finalizes a block whenever transactions are waiting, so the
// chain advances on its own -- no manual /proposeBlock call needed. Running
// this on more than one node at once would fork the chain.
//
// Env:
//   SYNTHOS_BLOCK_PRODUCER=true          enable the loop on this node
//   SYNTHOS_BLOCK_INTERVAL_SECONDS=10    how often to check/produce (default 10)
//   SYNTHOS_PRODUCE_EMPTY_BLOCKS=true    also produce empty blocks for liveness
func startBlockProducer(n *node.Node, ch *chain.Chain) {
	if os.Getenv("SYNTHOS_BLOCK_PRODUCER") != "true" {
		return
	}
	interval := 10 * time.Second
	if v := os.Getenv("SYNTHOS_BLOCK_INTERVAL_SECONDS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			interval = time.Duration(secs) * time.Second
		}
	}
	produceEmpty := os.Getenv("SYNTHOS_PRODUCE_EMPTY_BLOCKS") == "true"
	log.Printf("Block producer enabled: interval=%s produce_empty=%v (single-sequencer)", interval, produceEmpty)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if !produceEmpty && len(ch.MempoolSnapshot()) == 0 {
				continue
			}
			if _, err := n.ProposeBlockHash(); err != nil {
				log.Printf("auto-propose failed: %v", err)
				continue
			}
			log.Printf("auto-proposed block: height=%d", ch.Height())
		}
	}()
}

func startRegistryHeartbeat(nodeID string, chainID string, publicKey ed25519.PublicKey) {
	registryURL := strings.TrimRight(os.Getenv("SYNTHOS_REGISTRY_URL"), "/")
	selfURL := strings.TrimRight(os.Getenv("SYNTHOS_SELF_URL"), "/")
	if registryURL == "" || selfURL == "" {
		return
	}
	secret := os.Getenv("SYNTHOS_REGISTRY_SECRET")
	payload := map[string]any{
		"name":          nodeID,
		"url":           selfURL,
		"kind":          "validator",
		"network":       "mainnet",
		"status":        "running",
		"public_key":    hex.EncodeToString(publicKey),
		"capabilities":  agent.CoreCapabilities(),
		"cloud":         "render",
		"mode":          "reachable",
		"inbound_ports": 1,
	}
	post := func() {
		body, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, registryURL+"/register", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if secret != "" {
			req.Header.Set("X-Registry-Secret", secret)
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("registry heartbeat failed: %v", err)
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Printf("registry heartbeat returned %s", resp.Status)
		}
	}
	post()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			post()
		}
	}()
}

func bootstrapImmuneNode(cfg *config.NodeConfig, ch *chain.Chain, st *storage.Store, a *agent.Agent, publicKey ed25519.PublicKey) {
	if cfg == nil || ch == nil || st == nil || a == nil || !cfg.IsValidator {
		return
	}
	if os.Getenv("SYNTHOS_BOOTSTRAP_IMMUNE_NODE") != "true" {
		return
	}
	hardwareHash := a.Identity.HardwareID
	if hardwareHash == "" {
		hardwareHash = "synthos-hw-" + cfg.NodeID
	}
	addr := chain.AddressFromPublicKey(publicKey)
	if !ch.State.EnsureImmuneNode(addr, hardwareHash, time.Now().UTC().Unix()) {
		return
	}
	if err := st.Save(ch); err != nil {
		log.Printf("immune node bootstrap save failed: %v", err)
		return
	}
	log.Printf("immune node bootstrapped: node_id=%s address=%s", cfg.NodeID, addr)
}

func shouldRefreshHeightZeroSnapshot(snap *storage.Snapshot, genesisChain *chain.Chain) bool {
	if snap == nil || genesisChain == nil || snap.State == nil || len(snap.Blocks) != 1 {
		return false
	}
	genesisBlock := snap.Blocks[0]
	if genesisBlock == nil || genesisBlock.Header.Height != 0 || len(genesisBlock.Tx) != 0 {
		return false
	}
	if snap.ChainID != genesisChain.ChainID || snap.TxChainID != genesisChain.TransactionChainID() {
		return true
	}
	return snap.State.Root() != genesisChain.State.Root()
}

// nodeKeys returns the ed25519 identity synthosd should run with. An
// explicit private_key in the config always wins (useful for reproducible
// devnets / test fixtures). Otherwise the node's identity is persisted to
// disk under its data directory so that restarting the process reuses the
// same key instead of generating a brand new random identity every time --
// which would otherwise reset the node's on-chain history/reputation (and,
// for a validator, drop it out of the validator set) on every restart.
func nodeKeys(privateKeyHex string, dataDir string) (synthoscrypto.KeyPair, error) {
	if privateKeyHex != "" {
		return keyPairFromHex(privateKeyHex)
	}
	return loadOrCreatePersistedKeyPair(dataDir)
}

func keyPairFromHex(privateKeyHex string) (synthoscrypto.KeyPair, error) {
	raw := strings.TrimPrefix(privateKeyHex, "0x")
	b, err := hex.DecodeString(raw)
	if err != nil {
		return synthoscrypto.KeyPair{}, err
	}
	if len(b) != ed25519.PrivateKeySize {
		return synthoscrypto.KeyPair{}, fmt.Errorf("private_key must be %d bytes, got %d", ed25519.PrivateKeySize, len(b))
	}
	priv := ed25519.PrivateKey(b)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return synthoscrypto.KeyPair{}, fmt.Errorf("failed to derive public key")
	}
	return synthoscrypto.KeyPair{Public: pub, Private: priv}, nil
}

// persistedNodeKey is the on-disk shape of a node's ed25519 identity.
type persistedNodeKey struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	CreatedAt  string `json:"created_at"`
}

func persistedKeyPath(dataDir string) string {
	if dataDir == "" {
		dataDir = "."
	}
	return filepath.Join(dataDir, "node_identity.json")
}

// loadOrCreatePersistedKeyPair loads the node's identity from
// <dataDir>/node_identity.json, or generates one and saves it there the
// first time the node runs. The file contains raw private key material, so
// it's written with owner-only permissions and its data directory should
// never be committed to source control (see .gitignore).
func loadOrCreatePersistedKeyPair(dataDir string) (synthoscrypto.KeyPair, error) {
	path := persistedKeyPath(dataDir)

	if body, err := os.ReadFile(path); err == nil {
		var stored persistedNodeKey
		if err := json.Unmarshal(body, &stored); err != nil {
			return synthoscrypto.KeyPair{}, fmt.Errorf("reading persisted node identity %s: %w", path, err)
		}
		kp, err := keyPairFromHex(stored.PrivateKey)
		if err != nil {
			return synthoscrypto.KeyPair{}, fmt.Errorf("persisted node identity %s is invalid: %w", path, err)
		}
		return kp, nil
	} else if !os.IsNotExist(err) {
		return synthoscrypto.KeyPair{}, fmt.Errorf("reading persisted node identity %s: %w", path, err)
	}

	kp, err := synthoscrypto.NewKeyPair()
	if err != nil {
		return synthoscrypto.KeyPair{}, err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return synthoscrypto.KeyPair{}, fmt.Errorf("creating data dir %s for node identity: %w", dataDir, err)
	}
	stored := persistedNodeKey{
		PrivateKey: hex.EncodeToString(kp.Private),
		PublicKey:  hex.EncodeToString(kp.Public),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return synthoscrypto.KeyPair{}, err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return synthoscrypto.KeyPair{}, fmt.Errorf("writing persisted node identity %s: %w", path, err)
	}
	return kp, nil
}
