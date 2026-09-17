package rpc

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"synthos-collective/internal/chain"
	synthoscrypto "synthos-collective/internal/crypto"
)

// -----------------------------------------------------------------------
// Governor: real, signature-gated treasury governance over RPC.
//
// "Founder-gated" only means something if the RPC layer actually proves the
// caller controls the founder's private key. Earlier designs that would
// accept a bare address string in a JSON body let anyone claim to be the
// founder just by typing their address in the request. Every state-changing
// governance call here instead requires a real ed25519 signature; the
// caller's address is derived from the public key that produced that
// signature (never from a client-supplied field), and chain.TreasuryGovernance
// independently re-checks founder identity on top of that for proposal
// creation. Voting is signature-gated the same way, so a vote's stake
// weight can only ever be cast by whoever actually holds that address's key
// -- not by anyone who happens to know the address.
// -----------------------------------------------------------------------

const governanceSignatureFreshness = 5 * time.Minute

// verifySignedGovernanceRequest checks that sig is a real ed25519 signature
// by pubHex over payload, and that the request timestamp is fresh (bounding
// replay of a captured request to a 5 minute window). It returns the
// chain.Address derived from the verified public key -- this is the only
// address the caller is ever treated as acting for.
func verifySignedGovernanceRequest(pubHex, sigHex string, timestamp int64, payload []byte) (chain.Address, error) {
	if pubHex == "" || sigHex == "" {
		return "", errors.New("missing public_key or signature")
	}
	now := time.Now().Unix()
	skew := int64(governanceSignatureFreshness / time.Second)
	if timestamp <= 0 || timestamp < now-skew || timestamp > now+skew {
		return "", errors.New("request timestamp is missing or outside the freshness window")
	}
	pubBytes, err := synthoscrypto.PublicKeyBytes(pubHex)
	if err != nil || len(pubBytes) != 32 {
		return "", errors.New("invalid public key")
	}
	sigBytes, err := hex.DecodeString(strings.TrimPrefix(sigHex, "0x"))
	if err != nil || len(sigBytes) != 64 {
		return "", errors.New("invalid signature")
	}
	if !synthoscrypto.Verify(pubBytes, payload, sigBytes) {
		return "", errors.New("signature does not verify")
	}
	return chain.AddressFromPublicKey(pubBytes), nil
}

type governanceProposeRequest struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Amount      uint64 `json:"amount"`
	Recipient   string `json:"recipient"`
	Timestamp   int64  `json:"timestamp"`
	PublicKey   string `json:"public_key"`
	Signature   string `json:"signature"`
}

// signingPayload is the canonical byte string the caller must sign. Field
// order and delimiters are fixed, so the server and any client compute the
// exact same bytes.
func (r governanceProposeRequest) signingPayload() []byte {
	return []byte(fmt.Sprintf("synthos-governance-propose|%s|%s|%d|%s|%d",
		r.ID, r.Description, r.Amount, r.Recipient, r.Timestamp))
}

type governanceVoteRequest struct {
	ProposalID string `json:"proposal_id"`
	InFavor    bool   `json:"in_favor"`
	Timestamp  int64  `json:"timestamp"`
	PublicKey  string `json:"public_key"`
	Signature  string `json:"signature"`
}

func (r governanceVoteRequest) signingPayload() []byte {
	return []byte(fmt.Sprintf("synthos-governance-vote|%s|%t|%d",
		r.ProposalID, r.InFavor, r.Timestamp))
}

type governanceExecuteRequest struct {
	ProposalID string `json:"proposal_id"`
}

func (s *Server) handleGovernancePropose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.Node == nil || s.Node.Governance == nil {
		http.Error(w, "governance not configured for this deployment", http.StatusServiceUnavailable)
		return
	}
	var req governanceProposeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID == "" || req.Recipient == "" || req.Amount == 0 {
		http.Error(w, "id, recipient, and a non-zero amount are required", http.StatusBadRequest)
		return
	}
	caller, err := verifySignedGovernanceRequest(req.PublicKey, req.Signature, req.Timestamp, req.signingPayload())
	if err != nil {
		http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}
	if err := s.Node.Governance.CreateProposal(caller, req.ID, req.Description, req.Amount, chain.Address(req.Recipient)); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": req.ID, "proposer": caller})
}

func (s *Server) handleGovernanceVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.Node == nil || s.Node.Governance == nil {
		http.Error(w, "governance not configured for this deployment", http.StatusServiceUnavailable)
		return
	}
	var req governanceVoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ProposalID == "" {
		http.Error(w, "missing proposal_id", http.StatusBadRequest)
		return
	}
	voter, err := verifySignedGovernanceRequest(req.PublicKey, req.Signature, req.Timestamp, req.signingPayload())
	if err != nil {
		http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}
	if err := s.Node.Governance.Vote(voter, req.ProposalID, req.InFavor); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "proposal_id": req.ProposalID, "voter": voter, "in_favor": req.InFavor})
}

// handleGovernanceExecute pays out a proposal that has already passed and
// reached quorum. Deliberately not signature-gated to a specific caller --
// chain.TreasuryGovernance.Execute itself is the real gate (must be active,
// not already executed, majority FOR, and at quorum against the chain's
// real current total stake), so anyone observing that a proposal has passed
// can trigger the payout, same as anyone can call /proposeBlock once
// conditions are met. It cannot be used to move funds a proposal wasn't
// already, legitimately voted to release.
func (s *Server) handleGovernanceExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.Node == nil || s.Node.Governance == nil {
		http.Error(w, "governance not configured for this deployment", http.StatusServiceUnavailable)
		return
	}
	var req governanceExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ProposalID == "" {
		http.Error(w, "missing proposal_id", http.StatusBadRequest)
		return
	}
	totalStake := s.Chain.State.TotalStake()
	if err := s.Node.Governance.Execute(req.ProposalID, s.Chain.State, totalStake); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.Store != nil {
		_ = s.Store.Save(s.Chain)
	}
	writeJSON(w, map[string]any{"ok": true, "proposal_id": req.ProposalID})
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
