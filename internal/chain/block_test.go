package chain

import "testing"

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
	if len(h1) < 10 {
		t.Fatalf("unexpected hash: %q", h1)
	}
}
