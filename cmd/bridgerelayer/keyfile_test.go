package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadPrivateKeyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.key")
	if err := os.WriteFile(path, []byte("  abcd1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := readPrivateKeyFile(path)
	if err != nil || key != "abcd1234" {
		t.Fatalf("got %q, %v", key, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readPrivateKeyFile(path); err == nil {
			t.Fatal("accepted a key file other users can read")
		}
	}
	empty := filepath.Join(dir, "empty.key")
	_ = os.WriteFile(empty, nil, 0o600)
	if _, err := readPrivateKeyFile(empty); err == nil {
		t.Fatal("accepted an empty key file")
	}
}
