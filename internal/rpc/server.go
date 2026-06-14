package rpc

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

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
}

func NewServer(c *chain.Chain, st *storage.Store, n *node.Node) *Server {
	return &Server{
		Chain:       c,
		Store:       st,
		Node:        n,
		RateLimiter: NewRateLimiter(1000), // Default 1000 RPS
		MaxBodySize: 1024 * 1024,          // Default 1MB
	}
}

func NewServerWithConfig(c *chain.Chain, st *storage.Store, n *node.Node, rps int, maxBodySize int64) *Server {
	return &Server{
		Chain:       c,
		Store:       st,
		Node:        n,
		RateLimiter: NewRateLimiter(rps),
		MaxBodySize: maxBodySize,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/balance", s.handleBalance)
	mux.HandleFunc("/mempool", s.handleMempool)
	mux.HandleFunc("/dex/pools", s.handleDEXPools)
	mux.HandleFunc("/dex/quote", s.handleDEXQuote)
	mux.HandleFunc("/dex/swap", s.handleDEXSwap)
	mux.HandleFunc("/immune/status", s.handleImmuneStatus)
	mux.HandleFunc("/submitTx", s.handleSubmitTx)
	mux.HandleFunc("/proposeBlock", s.handleProposeBlock)

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
	writeJSON(w, map[string]any{
		"chain_id":   s.Chain.ChainID,
		"height":     s.Chain.Height(),
		"tip":        s.Chain.Tip().Hash,
		"state_root": s.Chain.State.Root(),
		"immune":     s.Chain.State.ImmuneStatus(),
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

func (s *Server) handleMempool(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"size": len(s.Chain.Mempool),
		"tx":   s.Chain.Mempool,
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
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Asset   string `json:"asset"`
		Amount  uint64 `json:"amount"`
		FromSyn bool   `json:"from_syn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.Chain.DEX.Swap(req.Asset, req.Amount, req.FromSyn)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.Store != nil {
		_ = s.Store.Save(s.Chain)
	}
	writeJSON(w, map[string]any{
		"ok":         true,
		"asset":      req.Asset,
		"amount_in":  req.Amount,
		"amount_out": out,
		"from_syn":   req.FromSyn,
		"pools":      s.Chain.DEX.ListPools(),
	})
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
	writeJSON(w, map[string]any{"ok": true, "block_hash": hash})
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
