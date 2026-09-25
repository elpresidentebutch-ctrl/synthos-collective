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
	"sort"
	"strconv"
	"strings"
	"sync"
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
	maxHotBlocks := maxHotBlocksFromEnv()

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
		// A one-time legacy-format snapshot loads ch.Blocks with the chain's
		// ENTIRE history (see storage.Store's loadLegacyFormat), not yet
		// bounded by any hot window. Archive all of it now, while maxHotBlocks
		// is still unset (0, unbounded) and every block is still in memory, so
		// nothing gets trimmed away before it's ever durably archived under
		// blocks/ -- this is the one-time migration step storage.Store's own
		// doc comment describes. For an already-split-format snapshot this is
		// a cheap no-op re-save (everything's already archived). Only after
		// this does RestoreHotWindow enable trimming going forward.
		_ = st.Save(ch)
		// Restore how much history had already been trimmed out of memory
		// (0/nil/nil for a legacy snapshot, meaning "nothing trimmed yet")
		// and (re)apply the configured bound -- see internal/chain's
		// SetHotWindow/RestoreHotWindow and internal/storage's split-format
		// Load for why this keeps RAM use bounded regardless of how large
		// the chain's full history has grown.
		ch.RestoreHotWindow(snap.HotWindowStart, snap.HotWindowBaseState, snap.HotWindowBaseBlock, maxHotBlocks, st)
	} else {
		ch, err = chain.NewChain(gen)
		if err != nil {
			panic(err)
		}
		// Ensure ChainID matches genesis when bootstrapping.
		ch.ChainID = gen.ChainID
		ch.SetHotWindow(maxHotBlocks, st)
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
	// Hoisted out of the block below so startValidatorRosterSync (wired up
	// further down, once srv exists) can reuse the exact same baseline
	// chainValidators/valKeys that ch.SetValidatorSet was just called with
	// here -- that parity is what makes an empty registry roster a true
	// no-op on top of this startup state, not an accidental second source
	// of truth. Left nil when len(validators) == 0 (consensus disabled or
	// this node isn't a validator at all), in which case
	// startValidatorRosterSync's own consensusEnabled gate keeps it inert
	// too.
	var chainValidators []string
	var valKeys map[string]ed25519.PublicKey
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
		var chainQuorum int
		var err error
		chainValidators, chainQuorum = resolveChainQuorum(cfg.TrustedValidators, validators, consensusEnabled, eng.RequiredForFinality())
		valKeys, err = buildValidatorKeySet(chainValidators, a.Identity.AgentID, keys.Public, cfg.PeerKeys)
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
	// Reused by startValidatorRosterSync, the mailbox relay's producer side
	// (rpc.Server's RegistryURL/RegistrySecret, set once here before any
	// goroutine that reads them starts -- see those fields' doc comments),
	// and startMailboxRelayListener below. Already set on every real
	// deployment for startRegistryHeartbeat's own heartbeat.
	registryURL := strings.TrimRight(os.Getenv("SYNTHOS_REGISTRY_URL"), "/")
	registrySecret := os.Getenv("SYNTHOS_REGISTRY_SECRET")
	srv.RegistryURL = registryURL
	srv.RegistrySecret = registrySecret

	srv.StartPeerSync(15 * time.Second)
	startRegistryHeartbeat(cfg.NodeID, ch.ChainID, keys.Public)
	// Picks up validators approved through the registry's Phase-1 queue
	// (cmd/cloudless-registry/main.go's handleAPIAdminValidatorByID)
	// without requiring a redeploy. Reuses SYNTHOS_REGISTRY_URL -- already
	// set on every real deployment for startRegistryHeartbeat above -- so
	// no new configuration is needed to activate this. See
	// docs/VALIDATOR_ONBOARDING.md's Phase 2 and startValidatorRosterSync's
	// own doc comment for the full design and safety gating.
	startValidatorRosterSync(
		ch, n, srv, a.Identity.AgentID,
		chainValidators, valKeys, validators, cfg.ConsensusPeers,
		registryURL,
		consensusEnabled,
		60*time.Second,
	)
	// Phase 3: lets THIS node vote in consensus even when it has no
	// reachable public URL of its own (approved via Phase 1, relayed via
	// Phase 2's roster sync into someone else's MailboxRelayPeers) --
	// polling its own mailbox for a proposal instead of waiting for an
	// inbound /consensus/propose call nothing could ever make. See
	// mailbox_relay.go and docs/VALIDATOR_ONBOARDING.md's Phase 3. Safe to
	// run on every node unconditionally: a directly-reachable validator's
	// mailbox simply stays empty, since no producer ever has a reason to
	// list it in MailboxRelayPeers.
	startMailboxRelayListener(n, a.Identity.AgentID, registryURL, registrySecret, consensusEnabled, 2*time.Second)

	// SYNTHOS_PRODUCER_ROTATION_LOCK_SECONDS wires the safety-critical
	// cross-round vote lock (see node.Node.SetProducerRotationLockTimeout's
	// doc comment) -- this must be set on EVERY validator in a
	// rotation-enabled deployment, not just the one(s) with
	// SYNTHOS_PRODUCER_ROTATION=true below, since a pure follower still
	// votes and still needs the lock the moment more than one validator can
	// legitimately propose. Left unset (0), the lock stays fully disabled
	// and behavior is unchanged from today.
	if v := strings.TrimSpace(os.Getenv("SYNTHOS_PRODUCER_ROTATION_LOCK_SECONDS")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			n.SetProducerRotationLockTimeout(time.Duration(secs) * time.Second)
			log.Printf("producer rotation: cross-round vote lock enabled, timeout=%ds", secs)
		} else {
			log.Printf("producer rotation: SYNTHOS_PRODUCER_ROTATION_LOCK_SECONDS=%q is not a positive integer, ignoring (lock stays disabled)", v)
		}
	}

	startBlockProducer(n, ch, srv, a.Identity.AgentID, validators)
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

// rosterEntry is one validator entry from the registry's public
// GET /api/validators/roster endpoint (see cmd/cloudless-registry/
// main.go's handleAPIValidatorsRoster). Only the fields this node needs
// are decoded; unknown fields are ignored.
type rosterEntry struct {
	NodeID    string `json:"node_id"`
	PublicKey string `json:"public_key"`
	PublicURL string `json:"public_url"`
	Reachable bool   `json:"reachable"`
}

// fetchValidatorRoster fetches the current approved-validator roster from
// the registry. A non-2xx response or a body that doesn't decode as
// expected is returned as an error -- callers should treat that as "skip
// this tick, keep the current validator set" (see startValidatorRosterSync),
// never as "the roster is now empty."
func fetchValidatorRoster(client *http.Client, registryURL string) ([]rosterEntry, error) {
	req, err := http.NewRequest(http.MethodGet, registryURL+"/api/validators/roster", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("registry returned status %d fetching validator roster", resp.StatusCode)
	}
	var body struct {
		Validators []rosterEntry `json:"validators"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding validator roster response: %w", err)
	}
	return body.Validators, nil
}

// mergeValidatorRoster computes the validator ID list, validator public-key
// map, and consensus-peer URL list that should be active right now, given
// this node's static config baseline and the latest fetched registry
// roster. See docs/VALIDATOR_ONBOARDING.md's Phase 2 for the full design.
//
// It is pure and deterministic: the same inputs always produce the same
// output. Calling it repeatedly with an unchanged roster reproduces the
// identical result every time (idempotent, so it's safe to call on a fixed
// timer indefinitely), and an empty roster reproduces exactly the
// static-config-only baseline this node already runs today -- callers that
// want fail-soft behavior on a registry outage should simply not call this
// at all on a fetch error, rather than calling it with an empty roster
// (which would be indistinguishable from "the registry legitimately has no
// approved validators," not from "the registry is unreachable").
//
// A roster entry is skipped -- never causes an error -- if its node_id is
// empty or equal to selfID, or its public_key doesn't decode to a valid
// Ed25519 key: a malformed or self-referential registry entry must never
// be able to halt a running validator's refresh loop. A roster entry only
// contributes to the returned consensus-peer URL list when it is marked
// Reachable and has a non-empty PublicURL -- an approved validator with
// neither (the common case for a home/laptop operator) becomes a trusted
// signer (added to validators/keys) but is not yet actively asked to vote;
// closing that gap is Phase 3 (the mailbox relay), not this function.
func mergeValidatorRoster(
	staticValidators []string,
	staticKeys map[string]ed25519.PublicKey,
	staticConsensusPeerURLs []string,
	selfID string,
	roster []rosterEntry,
) (validators []string, keys map[string]ed25519.PublicKey, consensusPeerURLs []string) {
	validatorSet := make(map[string]struct{}, len(staticValidators))
	for _, id := range staticValidators {
		if _, ok := validatorSet[id]; !ok {
			validatorSet[id] = struct{}{}
			validators = append(validators, id)
		}
	}

	keys = make(map[string]ed25519.PublicKey, len(staticKeys))
	for id, pub := range staticKeys {
		keys[id] = pub
	}

	peerSet := make(map[string]struct{}, len(staticConsensusPeerURLs))
	for _, url := range staticConsensusPeerURLs {
		if _, ok := peerSet[url]; !ok {
			peerSet[url] = struct{}{}
			consensusPeerURLs = append(consensusPeerURLs, url)
		}
	}

	for _, entry := range roster {
		id := strings.TrimSpace(entry.NodeID)
		if id == "" || id == selfID {
			continue
		}
		pubBytes, err := synthoscrypto.PublicKeyBytes(entry.PublicKey)
		if err != nil || len(pubBytes) != ed25519.PublicKeySize {
			continue
		}
		if _, ok := validatorSet[id]; !ok {
			validatorSet[id] = struct{}{}
			validators = append(validators, id)
		}
		keys[id] = ed25519.PublicKey(pubBytes)

		url := strings.TrimSpace(entry.PublicURL)
		if entry.Reachable && url != "" {
			if _, ok := peerSet[url]; !ok {
				peerSet[url] = struct{}{}
				consensusPeerURLs = append(consensusPeerURLs, url)
			}
		}
	}

	sort.Strings(validators)
	sort.Strings(consensusPeerURLs)
	return validators, keys, consensusPeerURLs
}

// mailboxRelayPeersFromRoster returns the validator IDs from roster that
// are approved (every entry in the roster response already is -- see
// handleAPIValidatorsRoster) but have no reachable direct URL: the
// home/laptop case mergeValidatorRoster deliberately leaves out of
// consensusPeerURLs. These are exactly the validators Phase 3's mailbox
// relay (see internal/rpc/mailbox_relay.go) can still reach -- through the
// registry's mailbox instead of a direct HTTPS call.
//
// Pure and deterministic, matching mergeValidatorRoster's own validation:
// an entry is skipped, never causes an error, if its node_id is empty or
// equal to selfID, or its public_key doesn't decode to a valid Ed25519 key
// -- a malformed or self-referential registry entry must never be able to
// halt or corrupt a running validator's relay-peer list. Checks both
// Reachable and PublicURL rather than trusting Reachable alone, since a
// future registry response that sets one without the other should still
// resolve safely here rather than accidentally trying to mailbox-relay to
// (or drop) a validator that's actually directly reachable.
func mailboxRelayPeersFromRoster(roster []rosterEntry, selfID string) []string {
	var peers []string
	for _, entry := range roster {
		id := strings.TrimSpace(entry.NodeID)
		if id == "" || id == selfID {
			continue
		}
		pubBytes, err := synthoscrypto.PublicKeyBytes(entry.PublicKey)
		if err != nil || len(pubBytes) != ed25519.PublicKeySize {
			continue
		}
		if entry.Reachable && strings.TrimSpace(entry.PublicURL) != "" {
			continue
		}
		peers = append(peers, id)
	}
	sort.Strings(peers)
	return peers
}

// startValidatorRosterSync periodically fetches the registry's public
// validator roster and folds any newly-approved validators into this
// node's live trust state, so a Phase-1 approval (see
// cmd/cloudless-registry/main.go's handleAPIAdminValidatorByID) actually
// takes effect on a running network -- without a redeploy. See
// docs/VALIDATOR_ONBOARDING.md's Phase 2 for the full design.
//
// Deliberately gated on consensusEnabled and registryURL being non-empty:
// with no real vote-collecting transport (consensusEnabled false) or no
// registry to ask (registryURL empty), silently growing the quorum this
// node demands would only ever stall block production for no benefit --
// the same rollout-safety reasoning resolveConsensusValidators/
// resolveChainQuorum already apply to the static config path. Returns
// immediately, doing nothing, when either gate fails; callers do not need
// to check these themselves first.
//
// Fail-soft: a fetch error or unreachable registry simply logs and skips
// that tick, leaving whatever validator set is already active untouched --
// a registry outage must never be able to shrink or strand an
// already-working validator set. Every tick recomputes the full target
// state from staticValidators/staticKeys/staticConsensusPeerURLs (this
// node's original config baseline, captured once at startup) merged with
// the latest roster, via mergeValidatorRoster -- never by incrementally
// mutating whatever the previous tick left behind -- so a validator that's
// later revoked from the registry (see handleAPIAdminValidatorByID's
// revoke action) actually drops back out on the next successful tick,
// rather than staying trusted forever once added.
//
// Safe to run on every validator (producer and followers alike), not just
// one: chain.SetValidatorSet and Node.SetValidators are exactly what
// startup already calls once; this only re-calls them periodically with a
// possibly-larger roster. SetConsensusPeerURLs is safe to call from this
// goroutine concurrently with the block-producer loop's own use of
// ConsensusPeerURLs -- see rpc.Server's consensusPeerURLsMu doc comment.
//
// Also calls SetMailboxRelayPeers every tick (Phase 3): a roster entry
// that's trusted but has no reachable URL doesn't just sit there uncalled
// forever -- it's handed to the mailbox relay (internal/rpc/
// mailbox_relay.go) so a home/laptop validator's vote can still actually
// reach the producer.
func startValidatorRosterSync(
	ch *chain.Chain,
	n *node.Node,
	srv *rpc.Server,
	selfID string,
	staticChainValidators []string,
	staticChainKeys map[string]ed25519.PublicKey,
	staticEngineValidators []string,
	staticConsensusPeerURLs []string,
	registryURL string,
	consensusEnabled bool,
	interval time.Duration,
) {
	if !consensusEnabled || registryURL == "" {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	go func() {
		for {
			time.Sleep(interval)
			roster, err := fetchValidatorRoster(client, registryURL)
			if err != nil {
				log.Printf("validator roster sync: %v -- keeping current validator set", err)
				continue
			}

			// Two separate merges, deliberately: chainValidators/staticChainKeys
			// and staticEngineValidators can legitimately differ (see
			// resolveChainQuorum's doc comment -- a single-sequencer deployment
			// trusts more IDs at the Chain level than it counts toward this
			// node's own local self-vote quorum). Applying the same roster
			// growth to each baseline separately, rather than unifying them
			// into one merge, preserves that distinction instead of silently
			// erasing it.
			chainValidators, chainKeys, consensusPeerURLs := mergeValidatorRoster(
				staticChainValidators, staticChainKeys, staticConsensusPeerURLs, selfID, roster,
			)
			engineValidators, _, _ := mergeValidatorRoster(staticEngineValidators, nil, nil, selfID, roster)

			for id, pub := range chainKeys {
				if id == selfID {
					continue
				}
				if err := n.AddPeer(id, hex.EncodeToString(pub)); err != nil {
					log.Printf("validator roster sync: registering peer %q: %v", id, err)
				}
			}
			n.SetValidators(engineValidators)
			quorum := 1
			if n.Consensus != nil {
				quorum = n.Consensus.RequiredForFinality()
			}
			ch.SetValidatorSet(chainKeys, quorum)
			srv.SetConsensusPeerURLs(consensusPeerURLs)
			srv.SetMailboxRelayPeers(mailboxRelayPeersFromRoster(roster, selfID))
			if len(chainValidators) != len(staticChainValidators) {
				log.Printf("validator roster sync: active validator set is now %d (started with %d): %v", len(chainValidators), len(staticChainValidators), chainValidators)
			}
		}
	}()
}

// startBlockProducer runs the automatic block-proposal loop. Enable it via
// SYNTHOS_BLOCK_PRODUCER=true; the others follow via HTTP peer catch-up (and,
// when ConsensusPeers is configured -- see below -- also actually vote on
// each proposal). Running the producer loop unconditionally on more than
// one node at once would still fork the chain -- that's still true, and is
// exactly what SYNTHOS_PRODUCER_ROTATION (below) exists to do safely
// instead of just not doing it.
//
// Env:
//
//	SYNTHOS_BLOCK_PRODUCER=true          enable the loop on this node
//	SYNTHOS_BLOCK_INTERVAL_SECONDS=10    how often to check/produce (default 10)
//	SYNTHOS_PRODUCE_EMPTY_BLOCKS=true    also produce empty blocks for liveness
//	SYNTHOS_CONSENSUS_PEERS              (via NodeConfig.ConsensusPeers) other
//	                                      validators to collect real votes from
//	SYNTHOS_CONSENSUS_TOKEN              shared secret for /consensus/propose
//	SYNTHOS_PRODUCER_ROTATION=true       opt into round-robin multi-producer
//	                                      rotation (see below); default off
//	SYNTHOS_PRODUCER_ROUND_SECONDS       how long the height's scheduled
//	                                      producer gets before the next
//	                                      validator in rotation also becomes
//	                                      eligible to try (default 3x interval)
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
//
// SYNTHOS_PRODUCER_ROTATION=true turns this from "the single designated
// sequencer" into one of several validators taking scheduled turns: every
// tick, this node computes (from rotation, the shared validators list in
// the exact order every node's config agrees on, and how long it's been
// since the chain's height last moved) whose turn it currently is via
// consensus.ExpectedProposer/CurrentRound, and only attempts to propose
// when that's itself. If the scheduled producer for a height doesn't
// deliver within its round window, the schedule mechanically advances to
// the next validator in rotation -- no explicit "detect the old one is
// down" step needed, since every rotation-enabled node is independently
// running the exact same clock-driven computation.
//
// This scheduling is a LIVENESS/fairness mechanism only; it deliberately
// does not need to be perfectly synchronized across nodes to stay SAFE.
// The actual safety boundary against two different validators each
// gathering quorum for two different blocks at one height (a real fork)
// is node.Node's cross-round vote lock (see SetProducerRotationLockTimeout,
// wired from SYNTHOS_PRODUCER_ROTATION_LOCK_SECONDS in main) on the
// RECEIVING side of every node -- that holds regardless of any bug,
// clock-skew, or race in this scheduling logic. Every validator in a
// rotation-enabled deployment must have that lock timeout set, not just
// the ones with SYNTHOS_PRODUCER_ROTATION=true here (a pure follower still
// votes, and still needs the lock the moment more than one validator can
// legitimately propose).
func startBlockProducer(n *node.Node, ch *chain.Chain, srv *rpc.Server, selfID string, rotation []string) {
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
	// ConsensusPeerURLsSnapshot, not the field directly: main() calls
	// startValidatorRosterSync (whose goroutine can call
	// SetConsensusPeerURLs as soon as its first tick fires) before this
	// function, so by the time this reads the peer count -- once here, and
	// again in the two log lines below -- a concurrent writer may already
	// exist. See consensusPeerURLsMu's doc comment.
	var consensusPeers []string
	if srv != nil {
		consensusPeers = srv.ConsensusPeerURLsSnapshot()
	}
	realConsensus := srv != nil && len(consensusPeers) > 0 && srv.ConsensusToken != ""

	rotationEnabled := os.Getenv("SYNTHOS_PRODUCER_ROTATION") == "true" && len(rotation) > 1
	roundInterval := 3 * interval
	if v := os.Getenv("SYNTHOS_PRODUCER_ROUND_SECONDS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			roundInterval = time.Duration(secs) * time.Second
		}
	}
	var scheduleMu sync.Mutex
	lastHeight := ch.Height()
	lastHeightAt := time.Now()
	// isMyTurn lazily resets the round clock whenever it notices the
	// chain's height has moved since it last checked -- see
	// consensus.CurrentRound's doc comment for why this local,
	// unsynchronized clock only needs to be roughly right, not exactly
	// agreed with any other node's.
	isMyTurn := func() (mine bool, target uint64, expected string, round int) {
		scheduleMu.Lock()
		defer scheduleMu.Unlock()
		h := ch.Height()
		if h != lastHeight {
			lastHeight = h
			lastHeightAt = time.Now()
		}
		// The actual decision is consensus.ShouldPropose (unit-tested in
		// internal/consensus/rotation_test.go); this closure's only job is
		// tracking the local, unsynchronized "how long has this height
		// been open" clock the doc comment above describes.
		return consensus.ShouldPropose(selfID, rotation, h, time.Since(lastHeightAt), roundInterval)
	}

	if rotationEnabled {
		log.Printf("Block producer enabled: interval=%s produce_empty=%v (REAL multi-validator consensus, %d peer(s), quorum required, ROTATION enabled: %d validators, round=%s)", interval, produceEmpty, len(consensusPeers), len(rotation), roundInterval)
	} else if realConsensus {
		log.Printf("Block producer enabled: interval=%s produce_empty=%v (REAL multi-validator consensus, %d peer(s), quorum required)", interval, produceEmpty, len(consensusPeers))
	} else {
		log.Printf("Block producer enabled: interval=%s produce_empty=%v (single-sequencer)", interval, produceEmpty)
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if rotationEnabled {
				mine, target, expected, round := isMyTurn()
				if !mine {
					if round > 0 && n != nil {
						// Best-effort bookkeeping only (real downtime
						// tracking, not a safety mechanism) -- see
						// Node.NoteMissedSlot's doc comment.
						n.NoteMissedSlot(expected, target)
					}
					continue
				}
			}
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

// defaultMaxHotBlocks bounds how many of the most recent finalized blocks
// stay in memory once SYNTHOS_MAX_HOT_BLOCKS isn't set to something else --
// see chain.Chain.SetHotWindow. Chosen to comfortably exceed any realistic
// reorg depth (TryReorg's only production caller only ever contests a
// height at or very near the current tip) while still bounding memory well
// below what full, ever-growing chain history would otherwise cost.
const defaultMaxHotBlocks = 2000

// maxHotBlocksFromEnv reads SYNTHOS_MAX_HOT_BLOCKS, falling back to
// defaultMaxHotBlocks if unset or not a positive integer. A value <= 0
// (explicitly configured) disables trimming entirely, restoring the
// original unbounded-in-memory behavior -- an escape hatch, not the
// intended steady-state configuration for a long-running node.
func maxHotBlocksFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("SYNTHOS_MAX_HOT_BLOCKS"))
	if raw == "" {
		return defaultMaxHotBlocks
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultMaxHotBlocks
	}
	return n
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
