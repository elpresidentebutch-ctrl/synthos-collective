package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNodeKeys_PersistsIdentityAcrossRestarts(t *testing.T) {
	dataDir := t.TempDir()

	first, err := nodeKeys("", dataDir)
	if err != nil {
		t.Fatalf("first nodeKeys call failed: %v", err)
	}

	keyFile := filepath.Join(dataDir, "node_identity.json")
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("expected node_identity.json to be created: %v", err)
	}

	// Simulate a process restart: call nodeKeys again with the same dataDir
	// and an empty config private_key, as synthosd does on every boot.
	second, err := nodeKeys("", dataDir)
	if err != nil {
		t.Fatalf("second nodeKeys call failed: %v", err)
	}

	if string(first.Public) != string(second.Public) || string(first.Private) != string(second.Private) {
		t.Fatalf("expected the same identity across restarts, got different keys:\n first=%x\nsecond=%x", first.Public, second.Public)
	}

	// A different data dir (a different node) must get its own identity.
	other, err := nodeKeys("", t.TempDir())
	if err != nil {
		t.Fatalf("nodeKeys for a different data dir failed: %v", err)
	}
	if string(first.Public) == string(other.Public) {
		t.Fatalf("expected different node identities for different data dirs, got the same key")
	}

	// An explicit config private_key still overrides persistence entirely.
	explicit, err := nodeKeys("302e020100300506032b657004220420"+"00000000000000000000000000000000000000000000000000000000", dataDir)
	if err == nil {
		t.Fatalf("expected malformed explicit private_key to error, got success with %x", explicit.Public)
	}
}
