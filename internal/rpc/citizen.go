package rpc

import (
	"encoding/json"
	"net/http"
	"time"

	"synthos-collective/internal/chain"
)

// -----------------------------------------------------------------------
// Citizen: real, signature-gated staking and reward claims (see
// docs/AGENTS_SPECIFICATION.md role 7, and internal/chain/citizen.go for
// the state-mutation logic and the reward/treasury design notes).
//
// stake, unstake, and claim-rewards used to mutate s.Chain.State directly,
// the instant this handler ran -- completely outside any transaction or
// block. On the single-node case that's just an architectural wart; on the
// real multi-node deployment this chain runs as, it was a live bug: only
// the one node that happened to receive the HTTP request ever saw the
// change, forking that node's Accounts (which IS hashed into the consensus
// state root) away from every peer, permanently, the moment the next block
// finalized. See the audit fix comments on ApplyTx/applyCitizenGovernanceTx
// in internal/chain/core.go and on State.GovernanceProposals in
// internal/chain/governance.go for the full explanation.
//
// The fix: these endpoints no longer mutate state themselves. Each one now
// requires the caller to submit a real, ed25519-signed chain.Tx (the exact
// same signature scheme and Tx.Verify() every ordinary transfer and bridge
// transaction already uses -- not a bespoke per-endpoint payload) with the
// right tx.Metadata "type", and simply hands it to the same mempool ->
// block pipeline /submitTx does. The action only actually takes effect
// once a block including it is finalized (normally within one heartbeat
// interval), at which point it's applied identically on every node that
// receives that block, not just the one that first saw the HTTP request.
// Callers should poll /citizen/status afterward rather than expecting the
// result in this response.
//
// This also closes a real authorization gap in the previous design:
// claim-rewards took no signature at all and paid out whatever address a
// caller named in the request body. Now the payout always goes to
// tx.From -- the address that actually signed the transaction -- so a
// citizen's accrued rewards can only ever be claimed by triggering it
// themselves.
// -----------------------------------------------------------------------

// decodeAndSubmitCitizenGovernanceTx decodes a chain.Tx from the request
// body, requires its metadata "type" to equal requiredType (so a caller
// can't point a /citizen/stake request at, say, a governance_execute
// transaction), and submits it via the ordinary mempool path. The actual
// authorization and state effect happen later, when the transaction is
// applied inside a finalized block (see applyCitizenGovernanceTx) -- this
// handler's only job is decoding, a type check, and handing off to
// SubmitTx, exactly like handleSubmitTx itself.
func (s *Server) decodeAndSubmitCitizenGovernanceTx(w http.ResponseWriter, r *http.Request, requiredType string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var tx chain.Tx
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		if err.Error() == "http: request body too large" {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	gotType := ""
	for _, kv := range tx.Metadata {
		if kv.Key == "type" {
			gotType = kv.Value
			break
		}
	}
	if gotType != requiredType {
		http.Error(w, "expected a signed transaction with metadata type=\""+requiredType+"\", got \""+gotType+"\"", http.StatusBadRequest)
		return
	}
	if err := s.Chain.SubmitTx(tx); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.Store != nil {
		_ = s.Store.Save(s.Chain)
	}
	writeJSON(w, map[string]any{"ok": true, "tx_id": tx.ID, "status": "submitted, will take effect once mined into a block"})
}

func (s *Server) handleCitizenStake(w http.ResponseWriter, r *http.Request) {
	s.decodeAndSubmitCitizenGovernanceTx(w, r, "citizen_stake")
}

func (s *Server) handleCitizenUnstake(w http.ResponseWriter, r *http.Request) {
	s.decodeAndSubmitCitizenGovernanceTx(w, r, "citizen_unstake")
}

func (s *Server) handleCitizenClaimRewards(w http.ResponseWriter, r *http.Request) {
	s.decodeAndSubmitCitizenGovernanceTx(w, r, "citizen_claim_rewards")
}

func (s *Server) handleCitizenStatus(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		http.Error(w, "missing address", http.StatusBadRequest)
		return
	}
	a := chain.Address(addr)
	stake := s.Chain.State.GetCitizenStake(a)
	pending := s.Chain.State.PendingCitizenRewards(a, time.Now().Unix())
	writeJSON(w, map[string]any{
		"ok":                       true,
		"address":                  addr,
		"stake":                    stake,
		"pending_rewards":          pending,
		"reward_rate_bps_per_year": s.Chain.State.CitizenRewardRateBpsPerYear,
		"rewards_configured":       s.Chain.State.TreasuryAddress != "" && s.Chain.State.CitizenRewardRateBpsPerYear != 0,
	})
}
