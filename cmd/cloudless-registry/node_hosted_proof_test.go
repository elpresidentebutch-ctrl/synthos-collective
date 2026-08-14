package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowserNodeRegistrationStartsHostedProofSession(t *testing.T) {
	s := &server{
		state: registryState{
			Peers:               map[string]peer{},
			Mailbox:             map[string][]mailboxMessage{},
			Contacts:            []contactMessage{},
			EarlyAccessPayments: map[string]earlyAccessPaymentIntent{},
		},
	}
	body := map[string]any{
		"publicId": "syn-smoke-hosted",
		"mode":     "public-validator",
		"endpoint": "https://synthos-collective.onrender.com/nodes",
		"capabilities": []string{
			"ed25519",
			"canonical_serialization",
			"validator_registry",
			"proposal_rotation",
			"quorum",
			"replay_protection",
			"persistent_storage",
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/register", bytes.NewReader(payload))
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	s.handleAPINodeRegister(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status = %d, body = %s", rec.Code, rec.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/nodes/syn-smoke-hosted/status", nil)
	statusRec := httptest.NewRecorder()
	s.handleAPINodeByID(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", statusRec.Code, statusRec.Body.String())
	}
	var status struct {
		Node struct {
			ProofStatus        string `json:"proof_status"`
			HostedProofSession bool   `json:"hosted_proof_session"`
			Health             bool   `json:"health"`
			ValidHeartbeats    uint64 `json:"valid_heartbeats"`
			Reward             struct {
				Eligible bool   `json:"eligible"`
				Status   string `json:"status"`
			} `json:"reward"`
		} `json:"node"`
	}
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Node.ProofStatus != "proving" {
		t.Fatalf("proof status = %q, want proving", status.Node.ProofStatus)
	}
	if !status.Node.HostedProofSession {
		t.Fatal("node was not marked as hosted proof session")
	}
	if !status.Node.Health {
		t.Fatal("hosted proof session should be healthy immediately after registration")
	}
	if status.Node.ValidHeartbeats == 0 {
		t.Fatal("hosted proof session should have at least one heartbeat")
	}
	if status.Node.Reward.Eligible {
		t.Fatal("hosted proof session must not be reward eligible")
	}
	if status.Node.Reward.Status != "hosted_bootstrap_not_reward_eligible" {
		t.Fatalf("reward status = %q", status.Node.Reward.Status)
	}
}
