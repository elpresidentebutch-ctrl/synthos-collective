package rpc

import (
	"encoding/json"
	"net/http"
	"strconv"

	"synthos-collective/internal/chain"
)

// -----------------------------------------------------------------------
// Simulator: real dry-run capability over RPC.
//
// Both endpoints below reuse the exact state-transition code the live chain
// runs (chain.Chain.SimulateTx and chain.Chain.BuildBlock), applied to a
// throwaway clone of the current state. Nothing here can ever mutate
// c.State, c.Mempool, or the real chain -- that's what makes this a real
// simulator rather than a label with no behavior behind it.
// -----------------------------------------------------------------------

// handleSimulateTx dry-runs a single transaction. It requires the same real
// signature and nonce a live submission would (chain.Chain.SimulateTx calls
// tx.Verify internally), so the result reflects what actually signing and
// submitting this transaction would do -- not a hypothetical with the
// checks turned off.
func (s *Server) handleSimulateTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var tx chain.Tx
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	result, err := s.Chain.SimulateTx(tx)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "result": result})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "result": result})
}

// handleSimulateBlock dry-runs building the next block from the current
// mempool against a clone of the current state (chain.Chain.BuildBlock is
// already non-mutating: it clones state internally and never touches
// c.Mempool or c.State). The block returned is never finalized, gossiped,
// or persisted -- calling this as many times as you like has no effect on
// the real chain.
func (s *Server) handleSimulateBlock(w http.ResponseWriter, r *http.Request) {
	proposerID := r.URL.Query().Get("proposer_id")
	if proposerID == "" {
		proposerID = "simulator"
	}
	maxTx := 0
	if raw := r.URL.Query().Get("max_tx"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxTx = parsed
		}
	}
	b, err := s.Chain.BuildBlock(proposerID, "", maxTx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{
		"ok":                   true,
		"simulated":            true,
		"height":               b.Header.Height,
		"tx_count":             len(b.Tx),
		"tx_merkle_root":       b.Header.TxMerkleRoot,
		"resulting_state_root": b.Header.StateRoot,
		"block_hash":           b.Hash,
	})
}
