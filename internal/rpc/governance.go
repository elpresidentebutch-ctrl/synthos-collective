package rpc

import (
	"net/http"

	"synthos-collective/internal/chain"
)

// -----------------------------------------------------------------------
// Governor: real, signature-gated treasury governance over RPC.
//
// propose/vote/execute used to call straight through to
// *chain.TreasuryGovernance, which mutated its own node-local Proposals map
// (and, for execute, s.Chain.State.Accounts directly) the instant this
// handler ran -- completely outside any transaction or block. See the
// audit fix comments on State.GovernanceProposals (internal/chain/
// governance.go) and applyCitizenGovernanceTx (internal/chain/core.go) for
// why that broke consensus on this chain's real multi-node deployment the
// same way the immune-node bootstrap bug and the bridge quorum bypass did.
//
// The fix follows internal/rpc/citizen.go's: these endpoints now require a
// real, ed25519-signed chain.Tx (Tx.Verify(), not a bespoke payload) with
// the matching tx.Metadata "type", and just hand it to the ordinary
// mempool -> block pipeline. Founder identity for governance_propose,
// vote weight for governance_vote, and quorum for governance_execute are
// all still fully enforced -- just at block-application time (see
// createGovernanceProposal/voteGovernance/executeGovernanceProposal in
// governance.go) rather than synchronously in this handler, which now only
// does a type/shape check before submitting.
// -----------------------------------------------------------------------

func (s *Server) handleGovernancePropose(w http.ResponseWriter, r *http.Request) {
	if s.Node == nil || s.Node.Governance == nil {
		http.Error(w, "governance not configured for this deployment", http.StatusServiceUnavailable)
		return
	}
	s.decodeAndSubmitCitizenGovernanceTx(w, r, "governance_propose")
}

func (s *Server) handleGovernanceVote(w http.ResponseWriter, r *http.Request) {
	if s.Node == nil || s.Node.Governance == nil {
		http.Error(w, "governance not configured for this deployment", http.StatusServiceUnavailable)
		return
	}
	s.decodeAndSubmitCitizenGovernanceTx(w, r, "governance_vote")
}

// handleGovernanceExecute pays out a proposal that has already passed and
// reached quorum. Deliberately not gated to a specific caller identity --
// executeGovernanceProposal itself is the real gate (must be active, not
// already executed, majority FOR, and at quorum against the chain's real
// current total stake), so any account that can pay the transaction fee
// can submit the transaction that triggers payout, same as anyone could
// call this endpoint before. It cannot be used to move funds a proposal
// wasn't already, legitimately voted to release.
func (s *Server) handleGovernanceExecute(w http.ResponseWriter, r *http.Request) {
	if s.Node == nil || s.Node.Governance == nil {
		http.Error(w, "governance not configured for this deployment", http.StatusServiceUnavailable)
		return
	}
	s.decodeAndSubmitCitizenGovernanceTx(w, r, "governance_execute")
}

func (s *Server) handleGovernanceProposals(w http.ResponseWriter, r *http.Request) {
	if s.Node == nil || s.Node.Governance == nil {
		writeJSON(w, map[string]any{"ok": true, "proposals": []chain.Proposal{}, "governance_configured": false})
		return
	}
	totalStake := s.Chain.State.TotalStake()
	if id := r.URL.Query().Get("id"); id != "" {
		p, ok := s.Node.Governance.Get(id)
		if !ok {
			http.Error(w, "proposal not found", http.StatusNotFound)
			return
		}
		passed, _ := s.Node.Governance.Passed(id, totalStake)
		writeJSON(w, map[string]any{
			"ok":                  true,
			"proposal":            p,
			"passed":              passed,
			"total_stake":         totalStake,
			"quorum_basis_points": chain.GovernanceQuorumBasisPoints,
		})
		return
	}
	writeJSON(w, map[string]any{
		"ok":                    true,
		"governance_configured": true,
		"proposals":             s.Node.Governance.Snapshot(),
		"total_stake":           totalStake,
		"quorum_basis_points":   chain.GovernanceQuorumBasisPoints,
		"founder_address":       s.Node.Governance.FounderAddress,
		"treasury_address":      s.Node.Governance.TreasuryAddr,
	})
}
