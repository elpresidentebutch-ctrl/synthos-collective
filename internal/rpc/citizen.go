package rpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"synthos-collective/internal/chain"
)

// -----------------------------------------------------------------------
// Citizen: real, signature-gated staking and reward claims (see
// docs/AGENTS_SPECIFICATION.md role 7, and internal/chain/citizen.go for
// the state-mutation logic and the reward/treasury design notes).
//
// stake and unstake move a real address's own spendable balance, so both
// require the same real ed25519 proof-of-control governance/propose and
// governance/vote already use (verifySignedGovernanceRequest is
// deliberately generic, not governance-specific, so it's reused here
// as-is rather than duplicated). claim-rewards only ever pays the staker's
// own address, so it's open the same way /governance/execute is: the real
// gate is the state itself (how much has actually accrued), not who calls
// the endpoint.
// -----------------------------------------------------------------------

type citizenStakeRequest struct {
	Amount    uint64 `json:"amount"`
	Timestamp int64  `json:"timestamp"`
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
}

func (r citizenStakeRequest) signingPayload() []byte {
	return []byte(fmt.Sprintf("synthos-citizen-stake|%d|%d", r.Amount, r.Timestamp))
}

type citizenUnstakeRequest struct {
	Amount    uint64 `json:"amount"`
	Timestamp int64  `json:"timestamp"`
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
}

func (r citizenUnstakeRequest) signingPayload() []byte {
	return []byte(fmt.Sprintf("synthos-citizen-unstake|%d|%d", r.Amount, r.Timestamp))
}

func (s *Server) handleCitizenStake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req citizenStakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Amount == 0 {
		http.Error(w, "amount must be positive", http.StatusBadRequest)
		return
	}
	caller, err := verifySignedGovernanceRequest(req.PublicKey, req.Signature, req.Timestamp, req.signingPayload())
	if err != nil {
		http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}
	if err := s.Chain.State.StakeCitizen(caller, req.Amount, req.Timestamp); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.Store != nil {
		_ = s.Store.Save(s.Chain)
	}
	stake := s.Chain.State.GetCitizenStake(caller)
	writeJSON(w, map[string]any{"ok": true, "address": caller, "stake": stake})
}

func (s *Server) handleCitizenUnstake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req citizenUnstakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Amount == 0 {
		http.Error(w, "amount must be positive", http.StatusBadRequest)
		return
	}
	caller, err := verifySignedGovernanceRequest(req.PublicKey, req.Signature, req.Timestamp, req.signingPayload())
	if err != nil {
		http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}
	if err := s.Chain.State.UnstakeCitizen(caller, req.Amount); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.Store != nil {
		_ = s.Store.Save(s.Chain)
	}
	stake := s.Chain.State.GetCitizenStake(caller)
	writeJSON(w, map[string]any{"ok": true, "address": caller, "stake": stake})
}

func (s *Server) handleCitizenClaimRewards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Address string `json:"address"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Address == "" {
		http.Error(w, "missing address", http.StatusBadRequest)
		return
	}
	paid, err := s.Chain.State.ClaimCitizenRewards(chain.Address(req.Address), time.Now().Unix())
	if err != nil {
		if err == chain.ErrCitizenRewardsNotConfigured {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.Store != nil && paid > 0 {
		_ = s.Store.Save(s.Chain)
	}
	writeJSON(w, map[string]any{"ok": true, "address": req.Address, "paid": paid})
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
