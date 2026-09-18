package rpc

import (
	"encoding/json"
	"net/http"

	"synthos-collective/internal/network"
)

// -----------------------------------------------------------------------
// Communicator (docs/AGENTS_SPECIFICATION.md role 4): real peer-to-peer
// messaging over this node's own agent transport.
//
// discover_peers is already served by the existing /peers endpoint (the
// real, authenticated peer registry every other role reads from too), so
// it isn't duplicated here. broadcast_transaction/relay_block are already
// /submitTx and /gossip/block. send_message and handle_incoming_message
// are the two methods that had no RPC surface before this file: sending
// goes out over this node's own real signed transport
// (Agent.BuildEnvelope + SendEnvelope, the same primitives block
// proposals/votes use); receiving is recorded by node.go's handleRaw
// "communicator_message" case into Node.Inbox and read back here.
// -----------------------------------------------------------------------

type communicatorSendRequest struct {
	ToAgentID string `json:"to_agent_id"`
	Body      string `json:"body"`
}

func (s *Server) handleCommunicatorSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.Node == nil || s.Node.Agent == nil {
		http.Error(w, "no agent attached to this node", http.StatusServiceUnavailable)
		return
	}
	var req communicatorSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ToAgentID == "" {
		http.Error(w, "missing to_agent_id", http.StatusBadRequest)
		return
	}
	if req.Body == "" {
		http.Error(w, "missing body", http.StatusBadRequest)
		return
	}
	if _, known := s.Node.Peers[req.ToAgentID]; !known {
		http.Error(w, "unknown peer: "+req.ToAgentID+" (not in this node's peer registry)", http.StatusBadRequest)
		return
	}
	env, err := s.Node.Agent.BuildEnvelope(network.MessageCommunicator, req.ToAgentID, "", network.CommunicatorPayload{Body: req.Body})
	if err != nil {
		http.Error(w, "failed to build message: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.Node.Agent.SendEnvelope(env); err != nil {
		http.Error(w, "failed to send: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "to_agent_id": req.ToAgentID})
}

func (s *Server) handleCommunicatorInbox(w http.ResponseWriter, r *http.Request) {
	if s.Node == nil {
		writeJSON(w, map[string]any{"ok": true, "messages": []any{}})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "messages": s.Node.Inbox()})
}
