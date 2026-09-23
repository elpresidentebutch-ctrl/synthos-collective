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
		// A restored chain starts from its saved State, not from genesis, so it
		// doesn't have the pre-block-1 state TryReorg needs to safely replay an
		// alternative branch. Seed it from a freshly-computed genesis chain
		// (already built above for the snapshot-freshness check) so fork-choice
		// reorgs work after a restart too.
		ch.SeedGenesisState(genesisChain.State)
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

	// Use a TLS-encrypted, peer-authenticated TCP transport so multiple
	// synthosd instances can talk across processes -- this replaces the
	// old TCPTransport, which sent every message in plaintext and had no
	// notion of peer identity at the connection level at all: anyone who
	// could reach the listen port could read every message and open a
	// connection claiming to be any agent ID. See
	// internal/network/secure_transport.go and peer_auth.go for how this
	// one authenticates peers (a signed handshake, cryptographically bound
	// to the specific TLS session it arrived on) without needing any new
	// secrets: it reuses this node's existing ed25519 identity key and the
	// same cfg.PeerKeys roster already configured for consensus.
	//
	// requirePeerAuth is on automatically whenever this node has any peer
	// keys configured at all; a bare devnet/local config with no
	// cfg.PeerKeys set (nothing to check signatures against) falls back to
	// accepting any handshake, same as this transport's previous
	// zero-configuration behavior.
	t, err := network.NewSecureTCPTransport(a.Identity.AgentID, cfg.ListenAddr, cfg.Peers, keys.Private, true, len(cfg.PeerKeys) > 0)
	if err != nil {
		panic(fmt.Errorf("creating secure transport: %w", err))
	}
	for agentID, pubKeyHex := range cfg.PeerKeys {
		if err := t.RegisterTrustedPeer(agentID, pubKeyHex); err != nil {
			panic(fmt.Errorf("registering trusted peer %q for secure transport: %w", agentID, err))
		}
	}
	a.AttachTransport(t)

	// consensusEnabled is the real signal for "does this deployment
	// participate in real multi-party consensus at all" -- true on every
	// node in the real-consensus roster (producer AND followers alike),
	// since every one of them independently enforces chain-level quorum on
	// any block it accepts, not just the one node that happens to initiate
	// HTTP rounds (that's the separate, narrower cfg.ConsensusPeers below,
	// only needed on the actual producer). Deliberately NOT gated on
	// cfg.ConsensusPeers: a follower validator has no proposals to fan out
	// (so it configures no ConsensusPeers) but still MUST require the real
	// quorum threshold on every block it accepts via gossip-push or
	// catch-up, or it would silently accept an under-signed block a
	// misbehaving or buggy producer sent it.
	consensusToken := strings.TrimSpace(os.Getenv("SYNTHOS_CONSENSUS_TOKEN"))
	consensusEnabled := consensusToken != ""

	validators := resolveConsensusValidators(cfg, consensusEnabled, a.Identity.AgentID)
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
	initGovernance(n, gen)

	if len(validators) > 0 {
		n.SetValidators(validators)
	}
	for peerID, pubKey := range cfg.PeerKeys {
		if err := n.AddPeer(peerID, pubKey); err != nil {
			panic(err)
		}
	}
	if len(validators) > 0 {
		// Require every finalized block to carry a real proposer signature
		// plus quorum-threshold validator approvals (see chain.Chain.
		// SetValidatorSet) using the same roster and keys just configured
		// above, so this closes the same unauthenticated-finalization gap for
		// both the gossip/HTTP catch-up path and local self-finalization.
		//
		// chainValidators/chainQuorum are deliberately kept separate from
		// validators/eng above: cfg.TrustedValidators lets this node recognize
		// (and accept catch-up blocks signed by) other known validators -- e.g.
		// the other nodes in a single-sequencer deployment, which all need to
		// trust the sequencer's key -- without inflating totalValidators and
		// therefore the *local* self-vote quorum this node's own Engine
		// requires before it will ever call TryFinalize on its own proposals.
		// When cfg.TrustedValidators isn't set, behavior is unchanged: the
		// roster and quorum come straight from cfg.Validators and the engine's
		// real BFT threshold, exactly as before. See resolveChainQuorum's own
		// doc comment for the consensusEnabled gating.
		chainValidators, chainQuorum := resolveChainQuorum(cfg.TrustedValidators, validators, consensusEnabled, eng.RequiredForFinality())
		valKeys, err := buildValidatorKeySet(chainValidators, a.Identity.AgentID, keys.Public, cfg.PeerKeys)
		if err != nil {
			panic(fmt.Errorf("building validator key set: %w", err))
		}
		ch.SetValidatorSet(valKeys, chainQuorum)
		if cfg.AuthEnforceFromHeight > 0 {
			ch.SetAuthEnforceFromHeight(cfg.AuthEnforceFromHeight)
		}
	}
	// Unconditional (not gated on len(validators) > 0): the state-root check
	// it relaxes applies to every block regardless of whether a validator set
	// is configured, so a node with StateRootEnforceFromHeight set but no
	// validators/trusted_validators should still get the grandfathering.
	if cfg.StateRootEnforceFromHeight > 0 {
		ch.SetStateRootEnforceFromHeight(cfg.StateRootEnforceFromHeight)
	}
	if err := n.Start(); err != nil {
		panic(err)
	}

	// Expose RPC for status, balances, tx submission, and on-demand block proposals.
	srv := rpc.NewServer(ch, st, n)
	srv.CommunicatorToken = strings.TrimSpace(os.Getenv("SYNTHOS_COMMUNICATOR_TOKEN"))
	if srv.CommunicatorToken == "" {
		log.Printf("communicator: SYNTHOS_COMMUNICATOR_TOKEN not set -- /communicator/send is disabled for this node")
	}
	srv.ProposeBlockToken = strings.TrimSpace(os.Getenv("SYNTHOS_PROPOSE_BLOCK_TOKEN"))
	if srv.ProposeBlockToken == "" {
		log.Printf("propose-block: SYNTHOS_PROPOSE_BLOCK_TOKEN not set -- /proposeBlock is disabled for this node (the automatic block-producer loop, if enabled, is unaffected)")
	}
	srv.SetPeerURLs(cfg.HTTPPeers)
	srv.SetConsensusPeerURLs(cfg.ConsensusPeers)
	srv.ConsensusToken = consensusToken
	if len(cfg.ConsensusPeers) > 0 && !consensusEnabled {
		log.Printf("consensus: SYNTHOS_CONSENSUS_PEERS is set but SYNTHOS_CONSENSUS_TOKEN is not -- real consensus rounds cannot authenticate to peers, falling back to single-sequencer behavior")
	}
	// SYNTHOS_CONSENSUS_PEER_TOKENS, when set, is a JSON object mapping
	// each ConsensusPeers URL to the specific outbound secret this node
	// should present when calling that peer -- see Server.
	// ConsensusPeerTokens' doc comment for why per-peer secrets exist
	// instead of one shared SYNTHOS_CONSENSUS_TOKEN value on every node.
	// Only meaningful on a block-producing node (the only one that ever
	// calls out to ConsensusPeerURLs); harmless to leave unset everywhere
	// else. Absent or empty, every peer falls back to plain
	// SYNTHOS_CONSENSUS_TOKEN, unchanged from before this existed.
	if raw := strings.TrimSpace(os.Getenv("SYNTHOS_CONSENSUS_PEER_TOKENS")); raw != "" {
		var peerTokens map[string]string
		if err := json.Unmarshal([]byte(raw), &peerTokens); err != nil {
			panic(fmt.Errorf("parsing SYNTHOS_CONSENSUS_PEER_TOKENS as a JSON object of peer-url -> token: %w", err))
		}
		srv.ConsensusPeerTokens = peerTokens
		for _, peer := range cfg.ConsensusPeers {
			if _, ok := peerTokens[peer]; !ok {
				log.Printf("consensus: SYNTHOS_CONSENSUS_PEER_TOKENS is set but has no entry for configured peer %s -- it will fall back to this node's own SYNTHOS_CONSENSUS_TOKEN for that peer", peer)
			}
		}
	}
	srv.StartPeerSync(15 * time.Second)
	startRegistryHeartbeat(cfg.NodeID, ch.ChainID, keys.Public)
	startBlockProducer(n, ch, srv)
	fmt.Printf("synthosd: RPC listening on %s (data dir %s, node_id=%s)\n", cfg.RPCListen, dataDir, cfg.NodeID)
	if err := http.ListenAndServe(cfg.RPCListen, srv.Handler()); err != nil {
		panic(err)
	}
}

// resolveConsensusValidators decides the roster this node's own Consensus
// Engine uses for its local self-vote quorum (see node.Node.
// ProposeBlockHash/SelfVote and consensus.Engine.RequiredForFinality).
//
// It deliberately ignores cfg.Validators entirely unless consensusEnabled
// is true, falling back to just this node's own ID (today's exact
// behavior) otherwise. This is what makes rolling out real multi-party
// consensus safe: new code and a config naming a real multi-validator
// roster can be deployed to every participating node ahead of time and
// stay completely inert -- each node's own Engine still requires only its
// own single self-vote to finalize, exactly like before -- right up until
// SYNTHOS_CONSENSUS_TOKEN is deliberately turned on, on every participating
// node together, as one clean step. Without this gate, merely deploying a
// config that names 3 validators would raise this node's own local quorum
// requirement to 2 immediately, before any real vote-collecting transport
// existed yet to ever satisfy it, silently halting block production.
func resolveConsensusValidators(cfg *config.NodeConfig, consensusEnabled bool, selfID string) []string {
	validators := cfg.Validators
	if !consensusEnabled {
		validators = nil
	}
	if len(validators) == 0 && cfg.IsValidator {
		validators = []string{selfID}
	}
	return validators
}

// resolveChainQuorum decides the roster and quorum threshold Chain itself
// independently enforces on every block it accepts (see chain.Chain.
// SetValidatorSet) -- the real safety net for both the HTTP consensus path
// and ordinary gossip-push/catch-up. trustedValidators is cfg.
// TrustedValidators; validators is this node's own Engine roster (see
// resolveConsensusValidators); engineQuorum is that Engine's own
// RequiredForFinality(). Returns (chainValidators, chainQuorum).
//
// When trustedValidators is empty, this falls back to validators/
// engineQuorum unchanged (today's exact behavior for a deployment that
// never set trusted_validators at all). When trustedValidators is set, the
// real threshold applies only once consensusEnabled is true -- otherwise
// this deliberately stays at 1 (only the proposer's own self-approval),
// since no real vote-collecting transport exists yet to gather more than
// that, regardless of how many keys are registered as trusted signers.
func resolveChainQuorum(trustedValidators []string, validators []string, consensusEnabled bool, engineQuorum int) (chainValidators []string, chainQuorum int) {
	chainValidators = trustedValidators
	chainQuorum = engineQuorum
	if len(chainValidators) == 0 {
		return validators, engineQuorum
	}
	if !consensusEnabled {
		chainQuorum = 1
	}
	return chainValidators, chainQuorum
}

// buildValidatorKeySet resolves the public key for every ID in the
// validator roster, so Chain.SetValidatorSet can verify proposer and quorum
// signatures against the real keys already configured for this node: this
// node's own key for its own ID, and cfg.PeerKeys (the same source
// n.AddPeer already uses) for every other validator.
func buildValidatorKeySet(validators []string, selfID string, selfPub ed25519.PublicKey, peerKeys map[string]string) (map[string]ed25519.PublicKey, error) {
	out := make(map[string]ed25519.PublicKey, len(validators))
	for _, id := range validators {
		if id == selfID {
			out[id] = selfPub
			continue
		}
		hexKey, ok := peerKeys[id]
		if !ok {
			return nil, fmt.Errorf("no public key configured for validator %q (add it to cfg.PeerKeys)", id)
		}
		pubBytes, err := synthoscrypto.PublicKeyBytes(hexKey)
		if err != nil {
			return nil, fmt.Errorf("invalid public key for validator %q: %w", id, err)
		}
		if len(pubBytes) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("public key for validator %q must be %d bytes, got %d", id, ed25519.PublicKeySize, len(pubBytes))
		}
		out[id] = ed25519.PublicKey(pubBytes)
	}
	return out, nil
}

// startBlockProducer runs the automatic block-proposal loop on the single
// designated sequencer. Enable it on exactly ONE validator via
// SYNTHOS_BLOCK_PRODUCER=true; the others follow via HTTP peer catch-up (and,
// when ConsensusPeers is configured -- see below -- also actually vote on
// each proposal). Running the producer loop itself on more than one node at
// once would still fork the chain; that part is unchanged.
//
// Env:
//
//	SYNTHOS_BLOCK_PRODUCER=true          enable the loop on this node
//	SYNTHOS_BLOCK_INTERVAL_SECONDS=10    how often to check/produce (default 10)
//	SYNTHOS_PRODUCE_EMPTY_BLOCKS=true    also produce empty blocks for liveness
//	SYNTHOS_CONSENSUS_PEERS              (via NodeConfig.ConsensusPeers) other
//	                                      validators to collect real votes from
//	SYNTHOS_CONSENSUS_TOKEN              shared secret for /consensus/propose
//
// When srv has real consensus peers AND a consensus token configured, each
// tick runs a genuine round: build a candidate, ask every consensus peer to
// independently validate and vote on it over HTTPS (rpc.Server.
// ProposeBlockWithConsensus), and only finalize once real quorum -- not just
// this node's own say-so -- is reached. If quorum isn't reached in time
// (a peer is down, slow, or disagrees), this tick simply doesn't advance the
// chain; the next tick tries again at the same height. That's a liveness
// trade-off, not a safety one: Chain itself independently re-verifies every
// signature before ever accepting a block, so a bug in the consensus
// wiring can stall production but can never finalize bad data (see
// chain.Chain.verifyBlockAuthorizationLocked).
//
// Without consensus peers configured (today's default for every deployment
// except the ones explicitly wired for it), behavior is unchanged from
// before: propose, self-approve, finalize immediately.
func startBlockProducer(n *node.Node, ch *chain.Chain, srv *rpc.Server) {
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
	realConsensus := srv != nil && len(srv.ConsensusPeerURLs) > 0 && srv.ConsensusToken != ""
	if realConsensus {
		log.Printf("Block producer enabled: interval=%s produce_empty=%v (REAL multi-validator consensus, %d peer(s), quorum required)", interval, produceEmpty, len(srv.ConsensusPeerURLs))
	} else {
		log.Printf("Block producer enabled: interval=%s produce_empty=%v (single-sequencer)", interval, produceEmpty)
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if !produceEmpty && len(ch.MempoolSnapshot()) == 0 {
				continue
			}
			if realConsensus {
				hash, finalized, err := srv.ProposeBlockWithConsensus(consensusRoundTimeout(interval))
				if err != nil {
					log.Printf("auto-propose (consensus) failed: %v", err)
					continue
				}
				if !finalized {
					log.Printf("auto-propose (consensus): quorum not reached this round, hash=%s height=%d -- will retry next tick", hash, ch.Height()+1)
					continue
				}
				log.Printf("auto-proposed block (real quorum reached): height=%d", ch.Height())
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

// consensusRoundTimeout bounds how long a single consensus round (proposing
// and collecting peer votes) is allowed to take, leaving meaningful headroom
// below the block-producer's own tick interval so a round that can't reach
// quorum in time fails fast rather than backing up ticks.
func consensusRoundTimeout(interval time.Duration) time.Duration {
	t := interval / 2
	if t < 2*time.Second {
		t = 2 * time.Second
	}
	if t > 8*time.Second {
		t = 8 * time.Second
	}
	return t
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

// initGovernance wires real treasury governance for this node, if this
// deployment has configured a founder address. The founder address is
// deliberately NOT hardcoded anywhere in this binary -- it's read from
// either an env var (operator override, useful for key rotation without
// touching genesis) or the genesis file's metadata, so which address can
// create treasury proposals is an explicit, auditable, per-deployment
// choice rather than a value baked into the code. Deployments that set
// neither simply have no Governor endpoint available (the RPC layer reports
// "governance not configured" rather than accepting requests against an
// empty address).
func initGovernance(n *node.Node, gen chain.Genesis) {
	founder := strings.TrimSpace(os.Getenv("SYNTHOS_FOUNDER_ADDRESS"))
	if founder == "" {
		founder = metadataString(gen.Metadata, "founder_address")
	}
	if founder == "" {
		log.Printf("governance: no founder address configured (SYNTHOS_FOUNDER_ADDRESS or genesis metadata.founder_address) -- Governor RPC endpoints disabled for this node")
		return
	}
	treasury := strings.TrimSpace(os.Getenv("SYNTHOS_TREASURY_ADDRESS"))
	if treasury == "" {
		treasury = metadataString(gen.Metadata, "treasury_address")
	}
	if treasury == "" {
		log.Printf("governance: founder address configured but no treasury address (SYNTHOS_TREASURY_ADDRESS or genesis metadata.treasury_address) -- proposals can be created and voted on, but Execute will fail until a treasury is set")
	}
	n.InitGovernance(chain.Address(founder), chain.Address(treasury))
	log.Printf("governance: initialized (founder=%s treasury=%s)", founder, treasury)

	// Citizen staking rewards (internal/chain/citizen.go) draw from this
	// exact same treasury balance, so keep State.TreasuryAddress in sync
	// with whatever value governance actually resolved above -- an env var
	// override should not leave Citizen rewards pointed at a stale genesis
	// address. Genesis.ToState already set a default from genesis metadata;
	// this only overrides it when an env var was actually given.
	if treasury != "" && os.Getenv("SYNTHOS_TREASURY_ADDRESS") != "" {
		n.Chain.State.TreasuryAddress = chain.Address(treasury)
	}
	if rateRaw := strings.TrimSpace(os.Getenv("SYNTHOS_CITIZEN_REWARD_RATE_BPS_PER_YEAR")); rateRaw != "" {
		rate, err := strconv.ParseUint(rateRaw, 10, 64)
		if err != nil {
			log.Printf("governance: invalid SYNTHOS_CITIZEN_REWARD_RATE_BPS_PER_YEAR=%q, ignoring: %v", rateRaw, err)
		} else {
			n.Chain.State.CitizenRewardRateBpsPerYear = rate
		}
	}
	log.Printf("citizen staking: treasury=%s reward_rate_bps_per_year=%d", n.Chain.State.TreasuryAddress, n.Chain.State.CitizenRewardRateBpsPerYear)
}

func metadataString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	v, ok := meta[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
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
