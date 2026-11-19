package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// pruneInactivePeers used to call persist while still holding s.mu's write
// lock. persist takes s.mu.RLock to snapshot the state, and RWMutex is not
// reentrant, so the first prune that removed a peer froze the registry.
func TestPruneInactivePeersPersistsWithoutDeadlock(t *testing.T) {
	s := newEmptyRegistry()
	s.stateFile = filepath.Join(t.TempDir(), "registry-state.json")
	old := time.Now().Add(-2 * time.Hour).UnixMilli()
	s.state.Peers["stale-node"] = peer{Name: "stale-node", LastSeen: old, RegisteredAt: old}

	done := make(chan int, 1)
	go func() { done <- s.pruneInactivePeers(time.Minute) }()

	select {
	case pruned := <-done:
		if pruned != 1 {
			t.Fatalf("pruned %d peers, want 1", pruned)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pruneInactivePeers deadlocked while persisting")
	}

	// The registry must still be usable afterwards.
	locked := make(chan struct{})
	go func() { s.mu.Lock(); s.mu.Unlock(); close(locked) }()
	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("registry lock still held after pruning")
	}
}

func TestHandlePrunePeers_RequiresPostAndAuth(t *testing.T) {
	s := newEmptyRegistry()
	s.secret = "super-secret-admin-token"
	s.stateFile = filepath.Join(t.TempDir(), "registry-state.json")

	// 1. GET is rejected with 405 Method Not Allowed
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/peers/prune", nil)
		req.Header.Set("X-Registry-Secret", "super-secret-admin-token")
		s.handlePrunePeers(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET code = %d, want 405 Method Not Allowed", rec.Code)
		}
	}

	// 2. POST without secret is rejected with 401 Unauthorized
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/peers/prune", nil)
		s.handlePrunePeers(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("POST without secret code = %d, want 401 Unauthorized", rec.Code)
		}
	}

	// 3. POST with wrong secret is rejected with 401 Unauthorized
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/peers/prune", nil)
		req.Header.Set("X-Registry-Secret", "wrong-secret")
		s.handlePrunePeers(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("POST with wrong secret code = %d, want 401 Unauthorized", rec.Code)
		}
	}

	// 4. POST with correct secret is accepted with 200 OK
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/peers/prune", nil)
		req.Header.Set("X-Registry-Secret", "super-secret-admin-token")
		s.handlePrunePeers(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST with correct secret code = %d, want 200 OK", rec.Code)
		}
	}
}
