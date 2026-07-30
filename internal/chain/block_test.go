package chain

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestBlock_ComputeHash_Deterministic(t *testing.T) {
	b := Block{
		Header: BlockHeader{
			Height:     1,
			ParentHash: "0x00",
			ProposerID: "p1",
			StateRoot:  "0x01",
		},
		Tx: []Tx{},
	}
	h1, err := b.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	h2, err := b.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("hash not stable: %q vs %q", h1, h2)
	}
	if len(h1) != 66 {
		t.Fatalf("expected 0x-prefixed 32-byte hash, got length %d: %q", len(h1), h1)
	}
	if !strings.HasPrefix(h1, "0x") {
		t.Fatalf("expected 0x-prefixed hash: %q", h1)
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(h1, "0x"))
	if err != nil {
		t.Fatalf("hash is not valid hex: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("expected 32 decoded hash bytes, got %d", len(decoded))
	}
}
