package rpc

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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
		MaxBodySize: 1024 * 1024, // Default 1MB
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
	mux.HandleFunc("/submitTx", s.handleSubmitTx)
	mux.HandleFunc("/proposeBlock", s.handleProposeBlock)
	
	// Wrap with rate limiting and input size limit middleware
	handler := s.RateLimiter.Middleware(mux)
	handler = s.bodyLimitMiddleware(handler)
	return handler
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
		"chain_id":  s.Chain.ChainID,
		"height":    s.Chain.Height(),
		"tip":       s.Chain.Tip().Hash,
		"state_root": s.Chain.State.Root(),
	})
}

func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		http.Error(w, "missing address", http.StatusBadRequest)
		return
	}
	// Validate that the address matches the expected format: "0x" + 40 hex chars
	// (20-byte SHA-256 derived address, see chain/address.go).
	if err := validateAddress(addr); err != nil {
		http.Error(w, "invalid address: "+err.Error(), http.StatusBadRequest)
		return
	}
	bal := s.Chain.State.Get(chain.Address(addr)).Balance
	writeJSON(w, map[string]any{
		"address": addr,
		"balance": bal,
	})
}

// validateAddress checks that addr is a well-formed SYNTHOS address ("0x" + 40 lowercase hex chars).
func validateAddress(addr string) error {
	const prefix = "0x"
	const hexLen = 40 // 20 bytes * 2 hex chars each
	if !strings.HasPrefix(addr, prefix) {
		return errors.New("must start with 0x")
	}
	hexPart := addr[len(prefix):]
	if len(hexPart) != hexLen {
		return errors.New("must be exactly 20 bytes (40 hex chars)")
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		return errors.New("contains non-hex characters")
	}
	return nil
}

func (s *Server) handleMempool(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"size": len(s.Chain.Mempool),
		"tx":   s.Chain.Mempool,
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

var _ = errors.New // keep import stable for future error mapping

