package rpc

import (
	"bytes"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	"synthos-collective/internal/node"
	"synthos-collective/internal/storage"
)

type Server struct {
	Chain       *chain.Chain
	Store       *storage.Store
	Node        *node.Node
	RateLimiter *RateLimiter
	MaxBodySize int64
	PeerURLs    []string
	HTTPClient  *http.Client

	// CommunicatorToken gates /communicator/send (see handleCommunicatorSend's
	// audit fix comment). Empty (the default) disables the endpoint
	// entirely -- deliberately not "allow everyone," since that's exactly
	// the open-relay behavior this field exists to close. Set it via
	// cmd/synthosd/main.go from SYNTHOS_COMMUNICATOR_TOKEN.
	CommunicatorToken string

	// ProposeBlockToken gates /proposeBlock (see handleProposeBlock's audit
	// fix comment). Same disabled-unless-configured default as
	// CommunicatorToken, set via cmd/synthosd/main.go from
	// SYNTHOS_PROPOSE_BLOCK_TOKEN.
	ProposeBlockToken string

	// ConsensusPeerURLs are the base URLs of the OTHER validators this node
	// runs a REAL multi-party consensus round with (see
	// ProposeBlockWithConsensus): each one is asked to validate and vote on
	// every proposal before it's finalized. Deliberately distinct from
	// PeerURLs (used only for after-the-fact catch-up/gossip-push to
	// however many read-only followers exist) -- a node not listed here is
	// never asked to vote, even if it's a trusted catch-up peer.
	ConsensusPeerURLs []string

	// ConsensusToken gates /consensus/propose the same way ProposeBlockToken
	// gates /proposeBlock: a shared operator secret, checked in constant
	// time, disabled by default. It exists to keep random internet traffic
	// off the endpoint cheaply -- it is NOT what makes a proposal
	// trustworthy (that's the block's own ProposerSignature, independently
	// verified against a registered validator key -- see node.Node.
	// HandleProposal), so every consensus-participating node must be
	// configured with the same value.
	ConsensusToken string

	// consensusClient is used for the outbound propose-and-collect-votes
	// calls in ProposeBlockWithConsensus. Kept separate from HTTPClient
	// (used for catch-up/gossip-push) so its timeout can be tuned
	// independently -- a consensus round needs to fit well inside the
	// block-producer loop's tick interval.
	consensusClient *http.Client
}

func NewServer(c *chain.Chain, st *storage.Store, n *node.Node) *Server {
	return &Server{
		Chain:       c,
		Store:       st,
		Node:        n,
		RateLimiter: NewRateLimiter(1000), // Default 1000 RPS
		MaxBodySize: 1024 * 1024,          // Default 1MB
		HTTPClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

func NewServerWithConfig(c *chain.Chain, st *storage.Store, n *node.Node, rps int, maxBodySize int64) *Server {
	return &Server{
		Chain:       c,
		Store:       st,
		Node:        n,
		RateLimiter: NewRateLimiter(rps),
		MaxBodySize: maxBodySize,
		HTTPClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Server) SetPeerURLs(urls []string) {
	s.PeerURLs = sanitizePeerURLs(urls)
}

// SetConsensusPeerURLs configures the other validators this node runs a
// real multi-party consensus round with (see ProposeBlockWithConsensus).
func (s *Server) SetConsensusPeerURLs(urls []string) {
	s.ConsensusPeerURLs = sanitizePeerURLs(urls)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/sync/status", s.handleSyncStatus)
	mux.HandleFunc("/account", s.handleAccount)
	mux.HandleFunc("/balance", s.handleBalance)
	mux.HandleFunc("/mempool", s.handleMempool)
	mux.HandleFunc("/blocks", s.handleBlocks)
	mux.HandleFunc("/dex/pools", s.handleDEXPools)
	mux.HandleFunc("/dex/quote", s.handleDEXQuote)
	mux.HandleFunc("/dex/swap", s.handleDEXSwap)
	mux.HandleFunc("/immune/status", s.handleImmuneStatus)
	mux.HandleFunc("/bridge/status", s.handleBridgeStatus)
	mux.HandleFunc("/bridge/events", s.handleBridgeEvents)
	mux.HandleFunc("/aen/status", s.handleAENStatus)
	mux.HandleFunc("/capabilities", s.handleCapabilities)
	mux.HandleFunc("/peers", s.handlePeers)
	mux.HandleFunc("/submitTx", s.handleSubmitTx)
	mux.HandleFunc("/proposeBlock", s.handleProposeBlock)
	mux.HandleFunc("/gossip/block", s.handleGossipBlock)
	mux.HandleFunc("/consensus/propose", s.handleConsensusPropose)
	mux.HandleFunc("/governance/propose", s.handleGovernancePropose)
	mux.HandleFunc("/governance/vote", s.handleGovernanceVote)
	mux.HandleFunc("/governance/execute", s.handleGovernanceExecute)
	mux.HandleFunc("/governance/proposals", s.handleGovernanceProposals)
	mux.HandleFunc("/simulate/tx", s.handleSimulateTx)
	mux.HandleFunc("/simulate/block", s.handleSimulateBlock)
	mux.HandleFunc("/economy/stats", s.handleEconomyStats)
	mux.HandleFunc("/communicator/send", s.handleCommunicatorSend)
	mux.HandleFunc("/communicator/inbox", s.handleCommunicatorInbox)
	mux.HandleFunc("/citizen/stake", s.handleCitizenStake)
	mux.HandleFunc("/citizen/unstake", s.handleCitizenUnstake)
	mux.HandleFunc("/citizen/claim-rewards", s.handleCitizenClaimRewards)
	mux.HandleFunc("/citizen/status", s.handleCitizenStatus)
	mux.HandleFunc("/enforcer/status", s.handleEnforcerStatus)

	// Wrap with rate limiting and input size limit middleware
	handler := s.RateLimiter.Middleware(mux)
	handler = s.bodyLimitMiddleware(handler)
	handler = s.corsMiddleware(handler)
	return handler
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bodyLimitMiddleware enforces maximum request body size
func (s *Server) bodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, s.MaxBodySize)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "service": "synthos-rpc"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	// immune.ActiveImmuneNodes comes straight from chain state -- the real,
	// verified count of nodes that have actually completed immune-node
	// bootstrap. It used to get silently overwritten with
	// live.ImmuneCapableNodes (how many currently-reachable peers merely
	// self-report the immune_node capability label) whenever that number was
	// bigger, which made this endpoint report a number nobody had actually
	// earned. live.ImmuneCapableNodes is still reported below, under "live",
	// honestly labeled as a live/self-reported count, not folded into the
	// verified figure.
	immune := s.Chain.State.ImmuneStatus()
	live := s.liveStatusSnapshot()
	body := map[string]any{
		"chain_id":    s.Chain.ChainID,
		"tx_chain_id": s.Chain.TxChainID,
		"height":      s.Chain.Height(),
		"tip":         s.Chain.Tip().Hash,
		"state_root":  s.Chain.State.Root(),
		"immune":      immune,
		"peers":       s.PeerURLs,
		"live":        live,
	}
	if s.Node != nil && s.Node.Agent != nil {
		body["agent"] = map[string]any{
			"id":           s.Node.Agent.Identity.AgentID,
			"public_key":   s.Node.Agent.Identity.PublicKey,
			"capabilities": s.Node.Agent.CoreCapabilities(),
		}
		body["capabilities"] = s.Node.Agent.CoreCapabilities()
		body["immune_capable"] = true
	}
	writeJSON(w, body)
}

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{
		"chain_id":   s.Chain.ChainID,
		"height":     s.Chain.Height(),
		"tip":        s.Chain.Tip().Hash,
		"state_root": s.Chain.State.Root(),
	}
	if s.Node != nil && s.Node.Agent != nil {
		body["capabilities"] = s.Node.Agent.CoreCapabilities()
		body["immune_capable"] = hasCapability(s.Node.Agent.CoreCapabilities(), "immune_node")
	}
	writeJSON(w, body)
}

type liveStatus struct {
	SelfHeight         uint64            `json:"self_height"`
	HighestHeight      uint64            `json:"highest_height"`
	ReachablePeers     int               `json:"reachable_peers"`
	ConfiguredPeers    int               `json:"configured_peers"`
	ValidatorsLive     int               `json:"validators_live"`
	ImmuneCapableNodes int               `json:"immune_capable_nodes"`
	ConvergedHeight    bool              `json:"converged_height"`
	PeerHeights        map[string]uint64 `json:"peer_heights"`
}

func (s *Server) liveStatusSnapshot() liveStatus {
	selfHeight := s.Chain.Height()
	out := liveStatus{
		SelfHeight:      selfHeight,
		HighestHeight:   selfHeight,
		ConfiguredPeers: len(s.PeerURLs),
		ConvergedHeight: true,
		PeerHeights:     make(map[string]uint64),
	}
	if s.Node != nil && s.Node.Agent != nil {
		if s.Node.IsValidator(s.Node.Agent.Identity.AgentID) {
			out.ValidatorsLive++
		}
		if hasCapability(s.Node.Agent.CoreCapabilities(), "immune_node") {
			out.ImmuneCapableNodes++
		}
	}

	for _, peer := range s.PeerURLs {
		status, err := s.probePeerStatus(peer, 1500*time.Millisecond)
		if err != nil {
			out.ConvergedHeight = false
			continue
		}
		out.ReachablePeers++
		out.ValidatorsLive++
		out.PeerHeights[peer] = status.Height
		if status.Height > out.HighestHeight {
			out.HighestHeight = status.Height
		}
		if status.Height != selfHeight {
			out.ConvergedHeight = false
		}
		if status.ImmuneCapable || hasCapability(status.Capabilities, "immune_node") {
			out.ImmuneCapableNodes++
		}
	}
	return out
}

func hasCapability(capabilities []string, want string) bool {
	for _, capability := range capabilities {
		if capability == want {
			return true
		}
	}
	return false
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.Node == nil || s.Node.Agent == nil {
		writeJSON(w, map[string]any{"capabilities": []string{}})
		return
	}
	writeJSON(w, map[string]any{
		"agent_id":       s.Node.Agent.Identity.AgentID,
		"public_key":     s.Node.Agent.Identity.PublicKey,
		"capabilities":   s.Node.Agent.CoreCapabilities(),
		"immune_capable": true,
	})
}

func (s *Server) handleAENStatus(w http.ResponseWriter, r *http.Request) {
	if s.Node == nil || s.Node.Agent == nil {
		writeJSON(w, map[string]any{
			"ok":    false,
			"ready": false,
			"error": "agent not attached",
		})
		return
	}
	writeJSON(w, map[string]any{
		"ok":           true,
		"ready":        true,
		"network":      "Agent Execution Network",
		"model":        "agents_and_nodes_are_the_same",
		"node_id":      s.Node.Agent.Identity.AgentID,
		"agent_id":     s.Node.Agent.Identity.AgentID,
		"public_key":   s.Node.Agent.Identity.PublicKey,
		"chain_id":     s.Chain.ChainID,
		"height":       s.Chain.Height(),
		"tip":          s.Chain.Tip().Hash,
		"state_root":   s.Chain.State.Root(),
		"peers":        s.PeerURLs,
		"capabilities": s.Node.Agent.CoreCapabilities(),
	})
}

func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"self":  r.Host,
		"peers": s.PeerURLs,
		"total": len(s.PeerURLs),
	})
}

func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		http.Error(w, "missing address", http.StatusBadRequest)
		return
	}
	bal := s.Chain.State.Get(chain.Address(addr)).Balance
	writeJSON(w, map[string]any{
		"address": addr,
		"balance": bal,
	})
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		http.Error(w, "missing address", http.StatusBadRequest)
		return
	}
	account := s.Chain.State.Get(chain.Address(addr))
	writeJSON(w, map[string]any{
		"address": addr,
		"balance": account.Balance,
		"nonce":   account.Nonce,
		"assets":  account.Assets,
	})
}

func (s *Server) handleMempool(w http.ResponseWriter, r *http.Request) {
	mempool := s.Chain.MempoolSnapshot()
	writeJSON(w, map[string]any{
		"size": len(mempool),
		"tx":   mempool,
	})
}

func (s *Server) handleBlocks(w http.ResponseWriter, r *http.Request) {
	from := 0
	if raw := r.URL.Query().Get("from"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			http.Error(w, "invalid from", http.StatusBadRequest)
			return
		}
		from = parsed
	}
	blocks := s.Chain.BlocksFrom(from)
	// Optional cap so a caller (a human debugging via curl, or a peer
	// catching up over HTTP) can request a bounded page instead of the
	// entire remaining chain in one response. Unbounded was fine while the
	// chain was small; at tens of thousands of blocks a single response is
	// many MB, which risks slow requests and timeouts for no benefit --
	// catch-up applies blocks one at a time regardless, so it doesn't need
	// them all in one round trip. Omitting limit keeps the old unbounded
	// behavior so nothing else relying on this endpoint breaks.
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		if limit < len(blocks) {
			blocks = blocks[:limit]
		}
	}
	writeJSON(w, map[string]any{
		"blocks": blocks,
		"count":  len(blocks),
	})
}

func (s *Server) handleDEXPools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ok":    true,
		"pools": s.Chain.DEX.ListPools(),
	})
}

func (s *Server) handleDEXQuote(w http.ResponseWriter, r *http.Request) {
	asset := r.URL.Query().Get("asset")
	amount, err := parseUintQuery(r, "amount")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fromSyn := r.URL.Query().Get("from") != "asset"
	pool := s.Chain.DEX.ListPools()[asset]
	if pool == nil {
		http.Error(w, "pool not found", http.StatusNotFound)
		return
	}

	var out uint64
	if fromSyn {
		out, err = s.Chain.DEX.GetAmountOut(amount, pool.SynReserve, pool.AssetReserve)
	} else {
		out, err = s.Chain.DEX.GetAmountOut(amount, pool.AssetReserve, pool.SynReserve)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{
		"ok":         true,
		"asset":      asset,
		"amount_in":  amount,
		"amount_out": out,
		"from_syn":   fromSyn,
		"fee_bps":    30,
	})
}

func (s *Server) handleDEXSwap(w http.ResponseWriter, r *http.Request) {
	http.Error(
		w,
		"DEX mutation is disabled until swaps are signed transactions finalized by consensus",
		http.StatusNotImplemented,
	)
}

func (s *Server) handleImmuneStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Chain.State.ImmuneStatus())
}

func (s *Server) handleBridgeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{
		"ok":      true,
		"chain":   s.Chain.ChainID,
		"height":  s.Chain.Height(),
		"bridge":  s.Chain.State.BridgeStatus(),
		"updated": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleBridgeEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	eventType := strings.TrimSpace(r.URL.Query().Get("type"))
	events := s.Chain.State.BridgeEventsSnapshot()
	filtered := make([]chain.BridgeRecord, 0, len(events))
	for _, event := range events {
		if eventType != "" && event.Type != eventType {
			continue
		}
		filtered = append(filtered, event)
		if len(filtered) >= limit {
			break
		}
	}
	writeJSON(w, map[string]any{
		"ok":     true,
		"chain":  s.Chain.ChainID,
		"height": s.Chain.Height(),
		"events": filtered,
		"count":  len(filtered),
	})
}

func (s *Server) handleSubmitTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var tx chain.Tx
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		// Check if it's a "request body too large" error
		if err.Error() == "http: request body too large" {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.Chain.SubmitTx(tx); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.Store != nil {
		_ = s.Store.Save(s.Chain) // best-effort persistence
	}
	writeJSON(w, map[string]any{"ok": true, "tx_id": tx.ID})
}

// handleProposeBlock used to have no authentication at all: any caller on
// the internet who could reach this node's RPC port could make it build,
// sign, finalize, persist, and broadcast a brand-new block on demand --
// with no rate limit beyond the server-wide one shared with every other
// endpoint. In normal production operation this is never needed:
// startBlockProducer (cmd/synthosd/main.go) already runs an automatic
// proposal loop on the one designated block-producer node, so "the chain
// advances on its own -- no manual /proposeBlock call needed" (see that
// function's own doc comment). Left open, this endpoint was both a cheap,
// unbounded-amplification DoS lever (one small HTTP POST triggers a full
// block build+sign+finalize+persist+broadcast cycle) and, more seriously,
// a consensus-forking one: Node.ProposeBlockHash only requires the calling
// node to be *a* validator, not *the* block producer, so hitting this on a
// follower validator (rather than the one running the automatic loop)
// could make it independently propose and finalize its own competing
// block at the same height -- exactly the two-producers-at-once scenario
// that same doc comment warns "would fork the chain." Fixed the same way
// /communicator/send was: a shared operator token, checked in constant
// time, with the endpoint disabled by default (no token configured) rather
// than open.
func (s *Server) handleProposeBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.ProposeBlockToken == "" {
		http.Error(w, "propose-block is disabled for this deployment (no SYNTHOS_PROPOSE_BLOCK_TOKEN configured)", http.StatusServiceUnavailable)
		return
	}
	given := r.Header.Get("X-Propose-Block-Token")
	if given == "" || subtle.ConstantTimeCompare([]byte(given), []byte(s.ProposeBlockToken)) != 1 {
		http.Error(w, "unauthorized: missing or incorrect X-Propose-Block-Token", http.StatusUnauthorized)
		return
	}
	if s.Node == nil {
		http.Error(w, "node not available", http.StatusServiceUnavailable)
		return
	}
	hash, err := s.Node.ProposeBlockHash()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.Store != nil {
		_ = s.Store.Save(s.Chain)
	}
	s.pushBlockToPeers(s.Chain.Tip())
	writeJSON(w, map[string]any{"ok": true, "block_hash": hash})
}

func (s *Server) handleGossipBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Block *chain.Block `json:"block"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	applied, err := s.applyPeerBlock(body.Block)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{
		"ok":      true,
		"applied": applied,
		"height":  s.Chain.Height(),
		"tip":     s.Chain.Tip().Hash,
	})
}

func (s *Server) applyPeerBlock(b *chain.Block) (bool, error) {
	if b == nil {
		return false, errors.New("missing block")
	}
	tip := s.Chain.Tip()
	if tip != nil && b.Header.Height <= tip.Header.Height {
		// This block is for a height we've already finalized. Rather than
		// silently dropping it (the old behavior, which meant a real fork was
		// never detected or resolved), let Chain.TryReorg decide: it only
		// ever switches to this block if it is fully valid and
		// cryptographically heavier -- more verified validator approvals --
		// than what we already have, so an unauthenticated or under-signed
		// block from a malicious or merely out-of-date peer can never cause a
		// rollback.
		existing := s.Chain.BlockAt(b.Header.Height)
		if existing != nil && existing.Hash == b.Hash {
			return false, nil
		}
		reorged, err := s.Chain.TryReorg(b.Header.Height, []*chain.Block{b})
		if err != nil || !reorged {
			return false, err
		}
		if s.Store != nil {
			_ = s.Store.Save(s.Chain)
		}
		return true, nil
	}
	if err := s.Chain.FinalizeBlock(b); err != nil {
		return false, err
	}
	if s.Store != nil {
		_ = s.Store.Save(s.Chain)
	}
	return true, nil
}

// handleConsensusPropose is the receiving side of real multi-party
// consensus: a validator running ProposeBlockWithConsensus POSTs a
// candidate block here, this node independently validates it (via
// node.Node.HandleProposal, which cryptographically checks the block's own
// ProposerSignature against a registered validator key -- see that
// method's doc comment for why no other authentication of the caller is
// needed) and returns the signed vote it casts. This is the actual live
// transport for consensus in production; the raw-TCP gossip path in
// node.Node.Start/handleRaw exists but has no real network wired to it in
// any current deployment (see cmd/synthosd/main.go).
func (s *Server) handleConsensusPropose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.ConsensusToken == "" {
		http.Error(w, "consensus is disabled for this deployment (no SYNTHOS_CONSENSUS_TOKEN configured)", http.StatusServiceUnavailable)
		return
	}
	given := r.Header.Get("X-Consensus-Token")
	if given == "" || subtle.ConstantTimeCompare([]byte(given), []byte(s.ConsensusToken)) != 1 {
		http.Error(w, "unauthorized: missing or incorrect X-Consensus-Token", http.StatusUnauthorized)
		return
	}
	if s.Node == nil {
		http.Error(w, "node not available", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Proposal consensus.BlockProposal `json:"proposal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	b := body.Proposal.Block
	vote, err := s.Node.HandleProposal(&b)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "vote": vote})
}

// ProposeBlockWithConsensus builds a new block on top of the current tip,
// asks every configured ConsensusPeerURLs peer to independently validate
// and vote on it over HTTPS, and finalizes + broadcasts it only once real
// quorum -- this node's own vote plus enough independently-verified peer
// votes, per node.Node.Consensus.RequiredForFinality() -- is reached.
//
// If quorum isn't reached within timeout (a peer is down, slow, or
// disagrees), this returns finalized=false and does NOT finalize anything.
// That is by design, not a bug to route around: the caller (the
// block-producer loop) should simply try again at the next tick. Chain
// itself independently re-verifies every proposer and quorum signature
// before ever accepting a block (see chain.Chain.
// verifyBlockAuthorizationLocked) regardless of what this method or its
// caller believes -- so a bug in this orchestration can stall block
// production, but it can never finalize a block that didn't genuinely earn
// real quorum approval.
func (s *Server) ProposeBlockWithConsensus(timeout time.Duration) (hash string, finalized bool, err error) {
	if s.Node == nil {
		return "", false, errors.New("node not available")
	}
	b, err := s.Node.BuildAndSignProposal()
	if err != nil {
		return "", false, fmt.Errorf("building proposal: %w", err)
	}
	if _, err := s.Node.SelfVote(b); err != nil {
		return b.Hash, false, fmt.Errorf("self-vote: %w", err)
	}

	finalized = s.collectConsensusVotes(b, timeout)
	if !finalized {
		return b.Hash, false, nil
	}
	if err := s.Node.TryFinalize(b.Hash); err != nil {
		return b.Hash, false, fmt.Errorf("finalize: %w", err)
	}
	if s.Chain.Tip() == nil || s.Chain.Tip().Hash != b.Hash {
		// TryFinalize is a no-op if quorum evaporated somehow (e.g. a
		// concurrent reorg) between collectConsensusVotes and here -- don't
		// claim finalized if the tip didn't actually move to this block.
		return b.Hash, false, nil
	}
	if s.Store != nil {
		_ = s.Store.Save(s.Chain)
	}
	s.pushBlockToPeers(s.Chain.Tip())
	return b.Hash, true, nil
}

// collectConsensusVotes asks every configured consensus peer to vote on b,
// in parallel, and feeds every independently-verified response into this
// node's local Consensus tally. Returns whether that tally reached real
// quorum (it already includes this node's own SelfVote, cast by the
// caller before this runs). A peer that's unreachable, slow past timeout,
// or returns a vote that doesn't check out cryptographically against its
// own registered key is simply skipped -- it does not error the round,
// matching normal BFT behavior for a peer that's down or misbehaving.
func (s *Server) collectConsensusVotes(b *chain.Block, timeout time.Duration) (finalized bool) {
	var wg sync.WaitGroup
	votes := make(chan consensus.BlockVote, len(s.ConsensusPeerURLs))
	for _, peer := range s.ConsensusPeerURLs {
		peer := peer
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := s.requestConsensusVote(peer, b, timeout)
			if err != nil {
				log.Printf("consensus: vote request to %s failed: %v", peer, err)
				return
			}
			if !s.verifyPeerVote(v, b) {
				log.Printf("consensus: discarding vote from %s that failed independent verification (claimed voter_id=%s)", peer, v.VoterID)
				return
			}
			votes <- v
		}()
	}
	wg.Wait()
	close(votes)

	for v := range votes {
		f, err := s.Node.HandleVote(v)
		if err != nil {
			continue
		}
		if f {
			finalized = true
		}
	}
	return finalized
}

// requestConsensusVote POSTs a candidate block to one consensus peer's
// /consensus/propose and returns the vote it casts, or an error if the
// peer is unreachable, times out, or rejects the proposal.
func (s *Server) requestConsensusVote(peer string, b *chain.Block, timeout time.Duration) (consensus.BlockVote, error) {
	body, err := json.Marshal(map[string]any{
		"proposal": consensus.BlockProposal{Block: *b, Height: b.Header.Height},
	})
	if err != nil {
		return consensus.BlockVote{}, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(peer, "/")+"/consensus/propose", bytes.NewReader(body))
	if err != nil {
		return consensus.BlockVote{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.ConsensusToken != "" {
		req.Header.Set("X-Consensus-Token", s.ConsensusToken)
	}
	client := s.consensusClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return consensus.BlockVote{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return consensus.BlockVote{}, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	var out struct {
		Vote consensus.BlockVote `json:"vote"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return consensus.BlockVote{}, err
	}
	return out.Vote, nil
}

// verifyPeerVote independently checks that a vote returned by a consensus
// peer really is a valid ed25519 signature, by the claimed voter's own
// registered public key, over this exact block's QuorumApprovalMessage.
// This is defense-in-depth, not the actual safety boundary: Chain
// re-verifies every QuorumSignatures entry itself before ever accepting a
// block (see chain.Chain.verifyBlockAuthorizationLocked), so a vote that
// slipped past this check could still never finalize a block on its own.
// What this buys is keeping this node's local Consensus tally (and
// therefore its decision about when to even attempt TryFinalize) honest,
// rather than lettable a malformed or spoofed HTTP response nudge that
// local bookkeeping around for no reason.
func (s *Server) verifyPeerVote(v consensus.BlockVote, b *chain.Block) bool {
	if v.Vote != 1 || v.BlockHash != b.Hash || v.Height != b.Header.Height || v.Signature == "" {
		return false
	}
	pub, ok := s.Node.PeerPublicKey(v.VoterID)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sigHex := strings.TrimPrefix(v.Signature, "0x")
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), b.QuorumApprovalMessage(), sig)
}

func (s *Server) StartPeerSync(interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if err := s.CatchUpOnce(); err != nil {
				log.Printf("http peer catch-up: %v", err)
			}
		}
	}()
}

// catchUpBatchSize bounds how many blocks CatchUpOnce requests in a single
// HTTP round trip. The chain only grows, so an unbounded "give me
// everything from height N" request (the old behavior) gets slower and
// larger forever; fetching in bounded pages keeps each request's size
// roughly constant regardless of how far behind a catching-up node is or
// how long the chain has been running.
const catchUpBatchSize = 500

func (s *Server) CatchUpOnce() error {
	myHeight := s.Chain.Height()
	for _, peer := range s.PeerURLs {
		status, err := s.peerStatus(peer)
		if err != nil {
			continue
		}
		if status.Height <= myHeight {
			continue
		}
		anyApplied := false
		for {
			blocks, err := s.peerBlocks(peer, int(myHeight+1), catchUpBatchSize)
			if err != nil {
				log.Printf("http peer catch-up: fetching blocks from %s (from=%d): %v", peer, myHeight+1, err)
				break
			}
			if len(blocks) == 0 {
				break
			}
			log.Printf("http peer catch-up: fetched %d block(s) from %s starting at height %d", len(blocks), peer, myHeight+1)
			batchApplied := 0
			stop := false
			for _, block := range blocks {
				ok, err := s.applyPeerBlock(block)
				if err != nil {
					h := uint64(0)
					if block != nil {
						h = block.Header.Height
					}
					log.Printf("http peer catch-up: rejecting block height=%d from %s: %v", h, peer, err)
					stop = true
					break
				}
				if ok {
					batchApplied++
					anyApplied = true
					myHeight = s.Chain.Height()
				}
			}
			if stop || len(blocks) < catchUpBatchSize {
				break
			}
		}
		if anyApplied {
			return nil
		}
	}
	return nil
}

func (s *Server) pushBlockToPeers(block *chain.Block) {
	if block == nil || len(s.PeerURLs) == 0 {
		return
	}
	body, _ := json.Marshal(map[string]any{"block": block})
	for _, peer := range s.PeerURLs {
		peer := peer
		go func() {
			req, err := http.NewRequest(http.MethodPost, strings.TrimRight(peer, "/")+"/gossip/block", bytes.NewReader(body))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := s.client().Do(req)
			if err == nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
		}()
	}
}

type peerStatus struct {
	Height        uint64   `json:"height"`
	ChainID       string   `json:"chain_id"`
	Tip           string   `json:"tip"`
	StateRoot     string   `json:"state_root"`
	ImmuneCapable bool     `json:"immune_capable"`
	Capabilities  []string `json:"capabilities"`
}

func (s *Server) peerStatus(peer string) (peerStatus, error) {
	return s.probePeerStatus(peer, 10*time.Second)
}

func (s *Server) probePeerStatus(peer string, timeout time.Duration) (peerStatus, error) {
	var out peerStatus
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(strings.TrimRight(peer, "/") + "/sync/status")
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, errors.New(resp.Status)
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

func (s *Server) peerBlocks(peer string, from int, limit int) ([]*chain.Block, error) {
	url := strings.TrimRight(peer, "/") + "/blocks?from=" + strconv.Itoa(from)
	if limit > 0 {
		url += "&limit=" + strconv.Itoa(limit)
	}
	resp, err := s.client().Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	var out struct {
		Blocks []*chain.Block `json:"blocks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Blocks, nil
}

func (s *Server) client() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return http.DefaultClient
}

func sanitizePeerURLs(urls []string) []string {
	out := make([]string, 0, len(urls))
	seen := map[string]bool{}
	for _, url := range urls {
		url = strings.TrimRight(strings.TrimSpace(url), "/")
		if url == "" || seen[url] {
			continue
		}
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			out = append(out, url)
			seen[url] = true
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func parseUintQuery(r *http.Request, name string) (uint64, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, errors.New("missing " + name)
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return 0, errors.New("invalid " + name)
	}
	return value, nil
}

var _ = errors.New // keep import stable for future error mapping
