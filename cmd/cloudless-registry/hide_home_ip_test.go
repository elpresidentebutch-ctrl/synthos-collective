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
	s.homeIPs = []string{"203.0.113.10"}
	s.fleetServers = []string{"198.51.100.7"}

	now := time.Now().UnixMilli()
	s.state.Peers["home-pc-1"] = peer{
		Name:         "home-pc-1",
		HouseholdIP:  "203.0.113.10",
		LastSeen:     now,
		RegisteredAt: now,
	}
	s.state.Peers["home-pc-2"] = peer{
		Name:         "home-pc-2",
		HouseholdIP:  "203.0.113.10",
		LastSeen:     now,
		RegisteredAt: now + 1000,
	}
	s.state.Peers["fleet-node-1"] = peer{
		Name:         "synthos-hosteons-1",
		Cloud:        "hosteons",
		HouseholdIP:  "198.51.100.7",
		LastSeen:     now,
		RegisteredAt: now + 2000,
	}
	s.state.Peers["fleet-ip-node"] = peer{
		Name:         "validator-vps",
		Cloud:        "operator",
		HouseholdIP:  "198.51.100.7",
		LastSeen:     now,
		RegisteredAt: now + 2500,
	}
	s.state.Peers["outside-node-1"] = peer{
		Name:         "community-node-1",
		Cloud:        "operator",
		HouseholdIP:  "198.51.100.42",
		LastSeen:     now,
		RegisteredAt: now + 3000,
	}
	s.state.Peers["desktop-7x9k2m"] = peer{
		Name:         "desktop-7x9k2m",
		Cloud:        "operator",
		HouseholdIP:  "198.51.100.43",
		LastSeen:     now,
		RegisteredAt: now + 4000,
	}
	s.state.Peers["synthos-user-1"] = peer{
		Name:         "synthos-user-1",
		Cloud:        "operator",
		HouseholdIP:  "198.51.100.44",
		LastSeen:     now,
		RegisteredAt: now + 5000,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	s.handleAPINodes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Raw response body must not contain any IP address or household_ip field
	body := rec.Body.String()
	for _, leaked := range []string{"203.0.113.10", "198.51.100.7", "198.51.100.42", "198.51.100.43", "198.51.100.44"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("response body leaked IP address %s: %s", leaked, body)
		}
	}
	if strings.Contains(body, "household_ip") {
		t.Fatalf("response body contains household_ip key: %s", body)
	}
	if !strings.Contains(body, `"outside":`) {
		t.Fatalf("response body missing outside key: %s", body)
	}

	// Shared household correlation and outside classification must still be marked correctly
	var resp struct {
		Peers []peer `json:"peers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Peers) != 7 {
		t.Fatalf("got %d peers, want 7", len(resp.Peers))
	}
	peerMap := make(map[string]peer)
	for _, p := range resp.Peers {
		if p.HouseholdIP != "" {
			t.Errorf("peer %s exposed HouseholdIP: %s", p.Name, p.HouseholdIP)
		}
		peerMap[p.Name] = p
	}

	if peerMap["home-pc-1"].Outside {
		t.Errorf("home-pc-1 Outside = true, want false")
	}
	if peerMap["home-pc-2"].Outside {
		t.Errorf("home-pc-2 Outside = true, want false")
	}
	if peerMap["synthos-hosteons-1"].Outside {
		t.Errorf("synthos-hosteons-1 Outside = true, want false")
	}
	if peerMap["validator-vps"].Outside {
		t.Errorf("validator-vps (configured fleet IP) Outside = true, want false")
	}
	if !peerMap["community-node-1"].Outside {
		t.Errorf("community-node-1 Outside = false, want true")
	}
	if !peerMap["desktop-7x9k2m"].Outside {
		t.Errorf("desktop-7x9k2m from unknown IP Outside = false, want true (desktop- prefix must not be treated as internal fleet)")
	}
	if !peerMap["synthos-user-1"].Outside {
		t.Errorf("synthos-user-1 from unknown IP Outside = false, want true (synthos- prefix must not be treated as internal fleet)")
	}
	// home-pc-2 should still detect shared household with home-pc-1
	if peerMap["home-pc-2"].SharedHouseholdWith != "home-pc-1" {
		t.Errorf("home-pc-2 SharedHouseholdWith = %q, want home-pc-1", peerMap["home-pc-2"].SharedHouseholdWith)
	}
}

func TestIsOutsideNoDefaultIPs(t *testing.T) {
	s := newEmptyRegistry()
	if len(s.homeIPs) != 0 {
		t.Fatalf("default homeIPs must be empty, got %v", s.homeIPs)
	}
	if len(s.fleetServers) != 0 {
		t.Fatalf("default fleetServers must be empty, got %v", s.fleetServers)
	}
	if !s.isOutside(peer{Name: "desktop-abcd", Cloud: "operator", HouseholdIP: "198.51.100.99"}) {
		t.Fatal("desktop-abcd from unknown IP must be outside=true when no home/fleet IPs match")
	}
	if !s.isOutside(peer{Name: "synthos-node-9", Cloud: "operator", HouseholdIP: "198.51.100.99"}) {
		t.Fatal("synthos-node-9 from unknown IP must be outside=true")
	}
	if s.isOutside(peer{Name: "desktop-hosteons", Cloud: "hosteons", HouseholdIP: "198.51.100.99"}) {
		t.Fatal("node on fleet cloud hosteons must be outside=false")
	}
}
