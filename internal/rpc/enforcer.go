package rpc

import (
	"net/http"

	"synthos-collective/internal/consensus"
)

// -----------------------------------------------------------------------
// Enforcer: real-time visibility into this node's own invalid-block and
// misbehavior slashing.
//
// The detection and slashing itself happens automatically in
// internal/node.Node.handleRaw (block_proposal case) the moment this node
// receives a validly-signed but genuinely invalid block proposal, and in
// internal/consensus.Engine.OnProposal/OnVote for double-signing and
// equivocation. Nothing here decides guilt or triggers a penalty -- this
// endpoint only reports what this node's own SlashingTracker has already
// independently verified and recorded, so an operator (or another agent)
// can see that Enforcer is doing real work instead of taking it on faith.
// -----------------------------------------------------------------------

// handleEnforcerStatus reports this node's real slashing history: total
// events recorded, and the events themselves (validator, violation type,
// block height, evidence). Every event here was independently verified
// before being recorded -- see the call sites in internal/node and
// internal/consensus -- so this list is real evidence, not a self-report.
func (s *Server) handleEnforcerStatus(w http.ResponseWriter, r *http.Request) {
	if s.Node == nil || s.Node.Slashing == nil {
		writeJSON(w, map[string]any{
			"ok":                 false,
			"enforcer_active":    false,
			"reason":             "this node has no slashing tracker configured",
			"total_slash_events": 0,
		})
		return
	}

	tracker := s.Node.Slashing
	history := tracker.GetSlashingHistory()

	events := make([]map[string]any, 0, len(history))
	for _, ev := range history {
		events = append(events, map[string]any{
			"validator_id": ev.ValidatorID,
			"event_type":   ev.EventType,
			"block_height": ev.BlockHeight,
			"timestamp":    ev.Timestamp,
			"evidence":     ev.Evidence,
			"jailed_now":   tracker.IsJailed(ev.ValidatorID),
		})
	}

	writeJSON(w, map[string]any{
		"ok":                 true,
		"enforcer_active":    true,
		"total_slash_events": tracker.TotalSlashEvents(),
		"detects": []string{
			string(consensus.DoubleSigning),
			string(consensus.InvalidBlock),
			string(consensus.Downtime),
			string(consensus.Equivocation),
		},
		"events": events,
		"note":   "Events are detected and recorded automatically by this node as it observes real signed proposals and votes -- this endpoint only reports them, it does not decide or trigger slashing itself.",
	})
}
