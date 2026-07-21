package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"synthos-collective/internal/chain"
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

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/account", s.handleAccount)
	mux.HandleFunc("/balance", s.handleBalance)
	mux.HandleFunc("/mempool", s.handleMempool)
	mux.HandleFunc("/blocks", s.handleBlocks)
	mux.HandleFunc("/dex/pools", s.handleDEXPools)
	mux.HandleFunc("/dex/quote", s.handleDEXQuote)
	mux.HandleFunc("/dex/swap", s.handleDEXSwap)
	mux.HandleFunc("/immune/status", s.handleImmuneStatus)
	mux.HandleFunc("/aen/status", s.handleAENStatus)
	mux.HandleFunc("/capabilities", s.handleCapabilities)
	mux.HandleFunc("/peers", s.handlePeers)
	mux.HandleFunc("/submitTx", s.handleSubmitTx)
	mux.HandleFunc("/proposeBlock", s.handleProposeBlock)
	mux.HandleFunc("/gossip/block", s.handleGossipBlock)

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
	body := map[string]any{
		"chain_id":   s.Chain.ChainID,
		"height":     s.Chain.Height(),
		"tip":        s.Chain.Tip().Hash,
		"state_root": s.Chain.State.Root(),
		"immune":     s.Chain.State.ImmuneStatus(),
		"peers":      s.PeerURLs,
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

func (s *Server) handleProposeBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		return false, nil
	}
	if err := s.Chain.FinalizeBlock(b); err != nil {
		return false, err
	}
	if s.Store != nil {
		_ = s.Store.Save(s.Chain)
	}
	return true, nil
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

func (s *Server) CatchUpOnce() error {
	myHeight := s.Chain.Height()
	for _, peer := range s.PeerURLs {
		status, err := s.peerStatus(peer)
		if err != nil || status.Height <= myHeight {
			continue
		}
		blocks, err := s.peerBlocks(peer, int(myHeight+1))
		if err != nil {
			continue
		}
		applied := 0
		for _, block := range blocks {
			ok, err := s.applyPeerBlock(block)
			if err != nil {
				break
			}
			if ok {
				applied++
				myHeight = s.Chain.Height()
			}
		}
		if applied > 0 {
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
	Height uint64 `json:"height"`
}

func (s *Server) peerStatus(peer string) (peerStatus, error) {
	var out peerStatus
	resp, err := s.client().Get(strings.TrimRight(peer, "/") + "/status")
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

func (s *Server) peerBlocks(peer string, from int) ([]*chain.Block, error) {
	resp, err := s.client().Get(strings.TrimRight(peer, "/") + "/blocks?from=" + strconv.Itoa(from))
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
