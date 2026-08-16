package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAENStatusReportsDutiesAndRanksOperators(t *testing.T) {
	now := time.Now()
	s := &server{
		state: registryState{
			Peers: map[string]peer{
				"syn-good": {
					Name:             "syn-good",
					Kind:             "validator",
					Role:             "validator_candidate",
					Status:           "proving",
					PublicKey:        "pub-good",
					Capabilities:     coreNodeCapabilities(),
					RegisteredAt:     now.Add(-time.Hour).UnixMilli(),
					LastSeen:         now.UnixMilli(),
					FirstHeartbeatAt: now.Add(-time.Hour).UnixMilli(),
					LastHeartbeatAt:  now.UnixMilli(),
					ValidHeartbeats:  8,
					VerifiedUptimeMS: int64(time.Hour / time.Millisecond),
					LastNonce:        "nonce-good",
					Height:           10,
					Tip:              "tip-good",
					StateRoot:        "state-good",
				},
				"syn-stale": {
					Name:               "syn-stale",
					Kind:               "validator",
					Role:               "validator_candidate",
					Status:             "registered",
					PublicKey:          "pub-stale",
					RegisteredAt:       now.Add(-time.Hour).UnixMilli(),
					LastSeen:           now.Add(-time.Hour).UnixMilli(),
					LastHeartbeatAt:    now.Add(-time.Hour).UnixMilli(),
					ValidHeartbeats:    0,
					VerifiedUptimeMS:   0,
					HostedProofSession: true,
				},
			},
			Mailbox:             map[string][]mailboxMessage{},
			Contacts:            []contactMessage{},
			EarlyAccessPayments: map[string]earlyAccessPaymentIntent{},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/aen/status", nil)
	rec := httptest.NewRecorder()
	s.handleAPIAENStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Duties                []map[string]any `json:"duties"`
		OperatorRankings      []map[string]any `json:"operator_rankings"`
		StaleNodes            []map[string]any `json:"stale_nodes"`
		FakeHeartbeatSuspects []map[string]any `json:"fake_heartbeat_suspects"`
		PublicReportAvailable bool             `json:"public_report_available"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Duties) != 7 {
		t.Fatalf("duties = %d, want 7", len(body.Duties))
	}
	if len(body.OperatorRankings) != 2 {
		t.Fatalf("operator rankings = %d, want 2", len(body.OperatorRankings))
	}
	if body.OperatorRankings[0]["node_id"] != "syn-good" {
		t.Fatalf("top ranked node = %v, want syn-good", body.OperatorRankings[0]["node_id"])
	}
	if len(body.StaleNodes) != 1 {
		t.Fatalf("stale nodes = %d, want 1", len(body.StaleNodes))
	}
	if len(body.FakeHeartbeatSuspects) != 1 {
		t.Fatalf("fake heartbeat suspects = %d, want 1", len(body.FakeHeartbeatSuspects))
	}
	if !body.PublicReportAvailable {
		t.Fatal("public report should be available")
	}
}
