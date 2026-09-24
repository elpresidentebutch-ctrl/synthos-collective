package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// eligibleCandidatePeer builds a peer that has genuinely earned validator
// promotion eligibility: a real registered key, a full verified month of
// uptime (VerifiedUptimeMS >= rewardEpoch), a fresh heartbeat, and the
// validator_candidate role -- exactly what nodeStatus's "accepted"
// (rewardEligible) computation requires. See proofStatus/nodeStatus in
// main.go.
func eligibleCandidatePeer(now time.Time) peer {
	return peer{
		Name:             "home-candidate-1",
		PublicKey:        "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Role:             "validator_candidate",
		Kind:             "validator",
		FirstHeartbeatAt: now.Add(-31 * 24 * time.Hour).UnixMilli(),
		LastHeartbeatAt:  now.Add(-1 * time.Minute).UnixMilli(),
		ValidHeartbeats:  1000,
		VerifiedUptimeMS: int64((31 * 24 * time.Hour) / time.Millisecond),
		LastNonce:        "000001",
	}
}

func doAdminGet(t *testing.T, s *server, path, secretHeader string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if secretHeader != "" {
		r.Header.Set("X-Registry-Secret", secretHeader)
	}
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func doAdminPost(t *testing.T, s *server, path, secretHeader string, body map[string]any, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		raw = b
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	if secretHeader != "" {
		r.Header.Set("X-Registry-Secret", secretHeader)
	}
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func TestAdminValidatorsPending_ListsOnlyGenuinelyEligibleUnapprovedCandidates(t *testing.T) {
	s := newSecurityTestServer()
	now := time.Now()

	eligible := eligibleCandidatePeer(now)
	s.state.Peers[eligible.Name] = eligible

	// Not yet eligible: only a week of uptime.
	tooNew := eligibleCandidatePeer(now)
	tooNew.Name = "home-candidate-2"
	tooNew.VerifiedUptimeMS = int64((7 * 24 * time.Hour) / time.Millisecond)
	tooNew.FirstHeartbeatAt = now.Add(-7 * 24 * time.Hour).UnixMilli()
	s.state.Peers[tooNew.Name] = tooNew

	// Eligible but already approved -- must not show up in "pending" again.
	alreadyApproved := eligibleCandidatePeer(now)
	alreadyApproved.Name = "home-candidate-3"
	alreadyApproved.ValidatorApproved = true
	s.state.Peers[alreadyApproved.Name] = alreadyApproved

	// A hosted proof session (bootstrap browser-tab prover) is explicitly
	// not reward eligible and must not appear either.
	hosted := eligibleCandidatePeer(now)
	hosted.Name = "hosted-1"
	hosted.HostedProofSession = true
	s.state.Peers[hosted.Name] = hosted

	w := doAdminGet(t, s, "/api/admin/validators/pending", "", s.handleAPIAdminValidatorsPending)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (no secret configured), got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Pending []map[string]any `json:"pending"`
		Count   int              `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("expected exactly 1 pending candidate, got %d: %+v", resp.Count, resp.Pending)
	}
	if resp.Pending[0]["node_id"] != "home-candidate-1" {
		t.Fatalf("expected home-candidate-1 to be the only pending entry, got %+v", resp.Pending[0])
	}
}

func TestAdminValidatorsPending_RequiresSecretWhenConfigured(t *testing.T) {
	s := newSecurityTestServer()
	s.secret = "top-secret"
	s.state.Peers["home-candidate-1"] = eligibleCandidatePeer(time.Now())

	w := doAdminGet(t, s, "/api/admin/validators/pending", "", s.handleAPIAdminValidatorsPending)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without the configured secret, got %d: %s", w.Code, w.Body.String())
	}

	w = doAdminGet(t, s, "/api/admin/validators/pending", "top-secret", s.handleAPIAdminValidatorsPending)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with the correct secret, got %d: %s", w.Code, w.Body.String())
	}
}

func TestApproveValidator_GrantsApprovalAndPersistsIt(t *testing.T) {
	s := newSecurityTestServer()
	s.state.Peers["home-candidate-1"] = eligibleCandidatePeer(time.Now())

	w := doAdminPost(t, s, "/api/admin/validators/home-candidate-1/approve", "", map[string]any{
		"public_url": "",
	}, s.handleAPIAdminValidatorByID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 approving an eligible candidate, got %d: %s", w.Code, w.Body.String())
	}

	entry := s.state.Peers["home-candidate-1"]
	if !entry.ValidatorApproved {
		t.Fatal("expected ValidatorApproved to be true after approval")
	}
	if entry.ValidatorApprovedAt == 0 {
		t.Fatal("expected ValidatorApprovedAt to be set after approval")
	}
	if entry.Role != "validator" {
		t.Fatalf("expected role to be promoted to 'validator', got %q", entry.Role)
	}

	// Once approved, it must disappear from the pending queue.
	w = doAdminGet(t, s, "/api/admin/validators/pending", "", s.handleAPIAdminValidatorsPending)
	var resp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Count != 0 {
		t.Fatalf("expected the approved candidate to leave the pending queue, got count=%d", resp.Count)
	}

	// And it must now show up in the public roster.
	w = doAdminGet(t, s, "/api/validators/roster", "", s.handleAPIValidatorsRoster)
	var roster struct {
		Validators []map[string]any `json:"validators"`
		Count      int              `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &roster); err != nil {
		t.Fatalf("decoding roster response: %v", err)
	}
	if roster.Count != 1 || roster.Validators[0]["node_id"] != "home-candidate-1" {
		t.Fatalf("expected the approved validator on the public roster, got %+v", roster.Validators)
	}
	if reachable, _ := roster.Validators[0]["reachable"].(bool); reachable {
		t.Fatal("expected a validator approved with no public_url to be marked unreachable")
	}
}

func TestApproveValidator_RejectsCandidateWithNoRegisteredKey(t *testing.T) {
	s := newSecurityTestServer()
	noKey := eligibleCandidatePeer(time.Now())
	noKey.PublicKey = ""
	s.state.Peers[noKey.Name] = noKey

	w := doAdminPost(t, s, "/api/admin/validators/home-candidate-1/approve", "", nil, s.handleAPIAdminValidatorByID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 approving a candidate with no public key, got %d: %s", w.Code, w.Body.String())
	}
	if s.state.Peers["home-candidate-1"].ValidatorApproved {
		t.Fatal("a rejected approval must not mark the candidate approved")
	}
}

func TestApproveValidator_DoesNotRequireEligibility(t *testing.T) {
	// The admin may approve someone before a full month elapses (a known
	// co-operator, a test node) -- the pending list is a convenience
	// filter, not a hard gate on the approve action itself.
	s := newSecurityTestServer()
	brandNew := peer{
		Name:      "trusted-operator",
		PublicKey: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Role:      "validator_candidate",
	}
	s.state.Peers[brandNew.Name] = brandNew

	w := doAdminPost(t, s, "/api/admin/validators/trusted-operator/approve", "", nil, s.handleAPIAdminValidatorByID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 approving a not-yet-eligible but admin-trusted candidate, got %d: %s", w.Code, w.Body.String())
	}
	if !s.state.Peers["trusted-operator"].ValidatorApproved {
		t.Fatal("expected the admin's direct approval to take effect regardless of eligibility")
	}
}

func TestApproveValidator_RequiresSecretWhenConfigured(t *testing.T) {
	s := newSecurityTestServer()
	s.secret = "top-secret"
	s.state.Peers["home-candidate-1"] = eligibleCandidatePeer(time.Now())

	w := doAdminPost(t, s, "/api/admin/validators/home-candidate-1/approve", "", nil, s.handleAPIAdminValidatorByID)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without the configured secret, got %d: %s", w.Code, w.Body.String())
	}
	if s.state.Peers["home-candidate-1"].ValidatorApproved {
		t.Fatal("an unauthorized approve call must not take effect")
	}
}

func TestRevokeValidator_ClearsApprovalAndRemovesFromRoster(t *testing.T) {
	s := newSecurityTestServer()
	approved := eligibleCandidatePeer(time.Now())
	approved.ValidatorApproved = true
	approved.ValidatorApprovedAt = time.Now().UnixMilli()
	approved.ValidatorPublicURL = "https://home-candidate-1.example.com"
	s.state.Peers[approved.Name] = approved

	w := doAdminPost(t, s, "/api/admin/validators/home-candidate-1/revoke", "", nil, s.handleAPIAdminValidatorByID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 revoking an approved validator, got %d: %s", w.Code, w.Body.String())
	}
	entry := s.state.Peers["home-candidate-1"]
	if entry.ValidatorApproved || entry.ValidatorApprovedAt != 0 || entry.ValidatorPublicURL != "" {
		t.Fatalf("expected revoke to clear all approval fields, got %+v", entry)
	}

	w = doAdminGet(t, s, "/api/validators/roster", "", s.handleAPIValidatorsRoster)
	var roster struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &roster); err != nil {
		t.Fatalf("decoding roster response: %v", err)
	}
	if roster.Count != 0 {
		t.Fatalf("expected a revoked validator to leave the public roster, got count=%d", roster.Count)
	}
}

func TestValidatorsRoster_MarksDirectURLAsReachable(t *testing.T) {
	s := newSecurityTestServer()
	approved := eligibleCandidatePeer(time.Now())
	approved.Name = "synthos-validator-12"
	approved.ValidatorApproved = true
	approved.ValidatorPublicURL = "https://synthos-validator-12.onrender.com"
	s.state.Peers[approved.Name] = approved

	w := doAdminGet(t, s, "/api/validators/roster", "", s.handleAPIValidatorsRoster)
	var roster struct {
		Validators []map[string]any `json:"validators"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &roster); err != nil {
		t.Fatalf("decoding roster response: %v", err)
	}
	if len(roster.Validators) != 1 {
		t.Fatalf("expected exactly one roster entry, got %+v", roster.Validators)
	}
	if reachable, _ := roster.Validators[0]["reachable"].(bool); !reachable {
		t.Fatal("expected a validator with a public_url to be marked reachable")
	}
	if roster.Validators[0]["public_url"] != "https://synthos-validator-12.onrender.com" {
		t.Fatalf("expected the public_url to be echoed back, got %+v", roster.Validators[0])
	}
}
