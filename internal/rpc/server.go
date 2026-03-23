package rpc

import (
	"encoding/json"
	"errors"
	"net/http"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/node"
	"synthos-collective/internal/storage"
)

type Server struct {
	Chain *chain.Chain
	Store *storage.Store
	Node  *node.Node
}

func NewServer(c *chain.Chain, st *storage.Store, n *node.Node) *Server {
	return &Server{Chain: c, Store: st, Node: n}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/balance", s.handleBalance)
	mux.HandleFunc("/mempool", s.handleMempool)
	mux.HandleFunc("/submitTx", s.handleSubmitTx)
	mux.HandleFunc("/proposeBlock", s.handleProposeBlock)
	return mux
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

func (s *Server) handleSubmitTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var tx chain.Tx
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
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

