package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNetworkStatus_PrefersRealRPCHeightOverSelfReportedPeerMax reproduces
// the live incident this fix addresses: the public /nodes page's "Chain
// height (network)" stat came from the max Height any peer self-reported in
// its heartbeat -- and since most P-O-U-T candidates just sign heartbeats
// from a browser tab with no real chain data (Height always 0), that max
// stayed stuck at 0 (or far behind) even while the real chain was healthy
// and climbing. A registered validator peer's own self-reported height
// should NOT be what this field reports once a real RPC endpoint is
// reachable -- the real chain height must win.
func TestNetworkStatus_PrefersRealRPCHeightOverSelfReportedPeerMax(t *testing.T) {
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"height":     31337,
			"tip":        "0xrealtip",
			"state_root": "0xrealroot",
		})
	}))
	defer rpc.Close()
	t.Setenv("SYNTHOS_NATIVE_RPC_URL", rpc.URL)

	now := time.Now()
	s := &server{
		state: registryState{
			Peers: map[string]peer{
				// A real validator that heartbeats in with SOME self-reported
				// height -- but nowhere near the real chain's actual tip,
				// exactly like the live incident (self-reported max stuck
				// far behind reality).
				"synthos-validator-12": {
					Name:         "synthos-validator-12",
					Kind:         "validator",
					Status:       "running",
					RegisteredAt: now.Add(-time.Hour).UnixMilli(),
					LastSeen:     now.UnixMilli(),
					Height:       0,
				},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/network/status", nil)
	rec := httptest.NewRecorder()
	s.handleAPINetworkStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got, want := out["highest_height"], float64(31337); got != want {
		t.Errorf("highest_height = %v, want %v (the real RPC height, not the self-reported peer max)", got, want)
	}
	if got, want := out["self_reported_peer_height"], float64(0); got != want {
		t.Errorf("self_reported_peer_height = %v, want %v (the old self-reported max, kept for reference)", got, want)
	}
	if got, want := out["tip"], "0xrealtip"; got != want {
		t.Errorf("tip = %v, want %v", got, want)
	}
	if got, want := out["state_root"], "0xrealroot"; got != want {
		t.Errorf("state_root = %v, want %v", got, want)
	}
}

// TestNetworkStatus_FallsBackToSelfReportedWhenRPCUnreachable ensures the
// fix fails soft: if the RPC endpoint can't be reached (down, misconfigured,
// or SYNTHOS_NATIVE_RPC_URL unset in some environment), the endpoint must
// still respond successfully using the old self-reported-peer-max behavior,
// rather than erroring the whole request or silently reporting zero.
func TestNetworkStatus_FallsBackToSelfReportedWhenRPCUnreachable(t *testing.T) {
	// A server that immediately closes, so requests to it fail outright --
	// simulates the RPC endpoint being unreachable.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	t.Setenv("SYNTHOS_NATIVE_RPC_URL", deadURL)

	now := time.Now()
	s := &server{
		state: registryState{
			Peers: map[string]peer{
				"synthos-validator-12": {
					Name:         "synthos-validator-12",
					Kind:         "validator",
					Status:       "running",
					RegisteredAt: now.Add(-time.Hour).UnixMilli(),
					LastSeen:     now.UnixMilli(),
					Height:       4242,
				},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/network/status", nil)
	rec := httptest.NewRecorder()
	s.handleAPINetworkStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got, want := out["highest_height"], float64(4242); got != want {
		t.Errorf("highest_height = %v, want %v (fallback to self-reported max when RPC unreachable)", got, want)
	}
}
