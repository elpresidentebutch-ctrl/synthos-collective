package rpc

import (
	"net/http"

	"synthos-collective/internal/chain"
)

// -----------------------------------------------------------------------
// Economist: a real, chain-derived economic snapshot over RPC.
//
// Every field here is computed from live state on each request (account
// balances, DEX pool reserves, mempool size) or from the same constants the
// state-transition code itself enforces (chain.MAX_SUPPLY,
// chain.BURN_PERCENT) -- never a static/self-reported number. "outstanding"
// is MAX_SUPPLY minus the real current sum of every balance
// (chain.State.TotalStake), which is the same math the code already uses
// for governance quorum; it's not tracked by a separate cumulative-burn
// counter (none exists in the ledger), so it also includes any genesis
// allocation that was never distributed, and callers should read it as
// "coins not currently sitting in any account", not strictly "coins burned
// by fees".
// -----------------------------------------------------------------------

func (s *Server) handleEconomyStats(w http.ResponseWriter, r *http.Request) {
	totalStake := s.Chain.State.TotalStake()
	outstanding := uint64(0)
	if chain.MAX_SUPPLY > totalStake {
		outstanding = chain.MAX_SUPPLY - totalStake
	}

	body := map[string]any{
		"ok":                     true,
		"chain_id":               s.Chain.ChainID,
		"height":                 s.Chain.Height(),
		"max_supply":             uint64(chain.MAX_SUPPLY),
		"circulating_supply":     totalStake,
		"uncirculated_or_burned": outstanding,
		"burn_percent_of_fees":   chain.BURN_PERCENT,
		"account_count":          s.Chain.State.AccountCount(),
		"mempool_size":           len(s.Chain.MempoolSnapshot()),
		"dex_pools":              s.Chain.DEX.ListPools(),
	}

	if s.Node != nil && s.Node.Governance != nil {
		body["treasury_address"] = s.Node.Governance.TreasuryAddr
		if s.Node.Governance.TreasuryAddr != "" {
			body["treasury_balance"] = s.Chain.State.Get(s.Node.Governance.TreasuryAddr).Balance
		}
		body["governance_quorum_basis_points"] = chain.GovernanceQuorumBasisPoints
	}

	writeJSON(w, body)
}
