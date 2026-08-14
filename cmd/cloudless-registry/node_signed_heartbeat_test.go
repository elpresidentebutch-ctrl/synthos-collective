package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBrowserNodeMustSendRealSignedHeartbeat(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{
		state: registryState{
			Peers:               map[string]peer{},
			Mailbox:             map[string][]mailboxMessage{},
			Contacts:            []contactMessage{},
			EarlyAccessPayments: map[string]earlyAccessPaymentIntent{},
		},
	}

	registerBody := map[string]any{
		"publicId":   "syn-signed-browser",
		"public_key": hex.EncodeToString(publicKey),
		"mode":       "public-validator",
		"endpoint":   "https://synthos-collective.onrender.com/nodes",
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
	postJSON(t, s.handleAPINodeRegister, "/api/nodes/register", registerBody, http.StatusOK)

	registered := getNodeStatus(t, s, "/api/nodes/syn-signed-browser/status")
	if registered.Node.Health {
		t.Fatal("registered node should not be healthy before a signed heartbeat")
	}
	if registered.Node.ValidHeartbeats != 0 {
		t.Fatalf("heartbeats before signing = %d", registered.Node.ValidHeartbeats)
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	nonce := "00000000000000000001"
	message := canonicalHeartbeatMessage("syn-signed-browser", 1, "tip-1", "state-1", timestamp, nonce)
	heartbeatBody := map[string]any{
		"node_id":      "syn-signed-browser",
		"height":       1,
		"tip":          "tip-1",
		"state_root":   "state-1",
		"timestamp":    timestamp,
		"nonce":        nonce,
		"signature":    hex.EncodeToString(ed25519.Sign(privateKey, message)),
		"capabilities": registerBody["capabilities"],
	}
	postJSON(t, s.handleAPINodeHeartbeat, "/api/nodes/heartbeat", heartbeatBody, http.StatusOK)

	status := getNodeStatus(t, s, "/api/nodes/syn-signed-browser/status")
	if status.Node.ProofStatus != "proving" {
		t.Fatalf("proof status = %q, want proving", status.Node.ProofStatus)
	}
	if status.Node.HostedProofSession {
		t.Fatal("real signed browser runner must not be marked as hosted proof")
	}
	if !status.Node.RealSignedHeartbeat {
		t.Fatal("real_signed_heartbeat should be true after accepted Ed25519 heartbeat")
	}
	if !status.Node.Health {
		t.Fatal("node should be healthy after signed heartbeat")
	}
	if status.Node.ValidHeartbeats != 1 {
		t.Fatalf("valid heartbeats = %d, want 1", status.Node.ValidHeartbeats)
	}
	if status.Node.Reward.Status != "proving_uptime" {
		t.Fatalf("reward status = %q", status.Node.Reward.Status)
	}
}

func postJSON(t *testing.T, handler http.HandlerFunc, path string, body map[string]any, wantStatus int) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s status = %d, want %d, body = %s", path, rec.Code, wantStatus, rec.Body.String())
	}
}

func getNodeStatus(t *testing.T, s *server, path string) struct {
	Node struct {
		ProofStatus         string  `json:"proof_status"`
		HostedProofSession  bool    `json:"hosted_proof_session"`
		RealSignedHeartbeat bool    `json:"real_signed_heartbeat"`
		Health              bool    `json:"health"`
		ValidHeartbeats     uint64  `json:"valid_heartbeats"`
		UptimePercent       float64 `json:"uptime_percent"`
		Reward              struct {
			Eligible bool   `json:"eligible"`
			Status   string `json:"status"`
		} `json:"reward"`
	} `json:"node"`
} {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.handleAPINodeByID(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	var status struct {
		Node struct {
			ProofStatus         string  `json:"proof_status"`
			HostedProofSession  bool    `json:"hosted_proof_session"`
			RealSignedHeartbeat bool    `json:"real_signed_heartbeat"`
			Health              bool    `json:"health"`
			ValidHeartbeats     uint64  `json:"valid_heartbeats"`
			UptimePercent       float64 `json:"uptime_percent"`
			Reward              struct {
				Eligible bool   `json:"eligible"`
				Status   string `json:"status"`
			} `json:"reward"`
		} `json:"node"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	return status
}
