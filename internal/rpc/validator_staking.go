package rpc

import (
	"net/http"
	"sort"

	"synthos-collective/internal/chain"
)

// -----------------------------------------------------------------------
// Validator staking: read-only view of the on-chain validator registry
// (see internal/chain/validator_staking.go).
//
// Bonding, unbonding and withdrawing are ordinary signed transactions
// (metadata type validator_bond / validator_unbond / validator_withdraw)
// submitted through /submitTx like any other transaction, so there is no
// write endpoint here: nothing about joining as a validator goes through
// an operator-controlled API.
//
// GET /validators/staking                 params, active and authorizing
//                                         sets, all records
// GET /validators/staking?operator=0x...  one validator's record
// -----------------------------------------------------------------------

func (s *Server) handleValidatorStaking(w http.ResponseWriter, r *http.Request) {
	params := s.Chain.ValidatorStakingParams()
	nextHeight := s.Chain.Height() + 1

	if op := r.URL.Query().Get("operator"); op != "" {
		rec, ok := s.Chain.State.GetValidator(chain.Address(op))
		if !ok {
			http.Error(w, "no validator registered for this operator", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "validator": rec})
		return
	}

	records := s.Chain.State.ValidatorRecords()
	sort.Slice(records, func(i, j int) bool { return records[i].Operator < records[j].Operator })

	writeJSON(w, map[string]any{
		"ok":                 true,
		"params":             params,
		"enabled":            params.EnabledAt(nextHeight),
		"next_block_height":  nextHeight,
		"active_set":         s.Chain.ActiveValidatorSet(),
		"authorizing_set":    s.Chain.State.AuthorizingValidatorSet(),
		"validators":         records,
		"total_locked":       s.Chain.State.TotalValidatorLocked(),
		"consensus_uses_set": s.Chain.StakeConsensusActiveAt(nextHeight),
		"note":               "active_set is what stake implies right now; authorizing_set is the snapshot taken at the last epoch boundary, which is what actually authorizes blocks once consensus_from_height is reached.",
	})
}
