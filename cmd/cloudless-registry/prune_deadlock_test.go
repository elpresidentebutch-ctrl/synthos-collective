package main

import (
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
