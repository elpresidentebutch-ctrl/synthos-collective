package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWritePeerListStripsHouseholdIP(t *testing.T) {
	s := newEmptyRegistry()
	now := time.Now().UnixMilli()
	s.state.Peers["home-pc-1"] = peer{
		Name:         "home-pc-1",
		HouseholdIP:  "76.158.149.182",
		LastSeen:     now,
		RegisteredAt: now,
	}
	s.state.Peers["home-pc-2"] = peer{
		Name:         "home-pc-2",
		HouseholdIP:  "76.158.149.182",
		LastSeen:     now,
		RegisteredAt: now + 1000,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	s.handleAPINodes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Raw response body must not contain the IP address or household_ip field
	body := rec.Body.String()
	if strings.Contains(body, "76.158.149.182") {
		t.Fatalf("response body leaked home IP address: %s", body)
	}
	if strings.Contains(body, "household_ip") {
		t.Fatalf("response body contains household_ip key: %s", body)
	}

	// Shared household correlation must still be marked correctly
	var resp struct {
		Peers []peer `json:"peers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Peers) != 2 {
		t.Fatalf("got %d peers, want 2", len(resp.Peers))
	}
	for _, p := range resp.Peers {
		if p.HouseholdIP != "" {
			t.Errorf("peer %s exposed HouseholdIP: %s", p.Name, p.HouseholdIP)
		}
	}
	// home-pc-2 should still detect shared household with home-pc-1
	var pc2 peer
	for _, p := range resp.Peers {
		if p.Name == "home-pc-2" {
			pc2 = p
		}
	}
	if pc2.SharedHouseholdWith != "home-pc-1" {
		t.Errorf("home-pc-2 SharedHouseholdWith = %q, want home-pc-1", pc2.SharedHouseholdWith)
	}
}
