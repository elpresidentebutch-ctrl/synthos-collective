package chain

import (
	"crypto/sha256"
	"testing"
)

// TestBuildMerkleRoot_OddLeafIsDuplicatedAndHashed is a regression test for
// a real divergence between this function and TxMerkleRoot (merkle.go),
// which is the reference/correct implementation. buildMerkleRoot used to
// pass a trailing odd leaf through UNHASHED to the next tree level instead
// of duplicating it and hashing the pair, meaning a lone odd leaf skipped a
// full hashing round relative to its siblings -- a different (and
// non-standard) Merkle construction from the one TxMerkleRoot already used
// for the exact same "odd number of leaves" case.
func TestBuildMerkleRoot_OddLeafIsDuplicatedAndHashed(t *testing.T) {
	leaf := func(b byte) []byte {
		h := sha256.Sum256([]byte{b})
		return h[:]
	}

	// Three leaves: a real odd-leaf case at the top level.
	l0, l1, l2 := leaf(0), leaf(1), leaf(2)

	got := buildMerkleRoot([][]byte{
		append([]byte{}, l0...),
		append([]byte{}, l1...),
		append([]byte{}, l2...),
	})

	// Expected, computed by hand using the duplicate-and-hash rule:
	//   level1 = [H(l0||l1), H(l2||l2)]
	//   root   = H(level1[0] || level1[1])
	h01 := sha256.Sum256(append(append([]byte{}, l0...), l1...))
	h22 := sha256.Sum256(append(append([]byte{}, l2...), l2...))
	wantSum := sha256.Sum256(append(append([]byte{}, h01[:]...), h22[:]...))
	want := wantSum[:]

	if string(got) != string(want) {
		t.Fatalf("buildMerkleRoot with odd leaf count: got %x, want %x (odd leaf must be duplicated and hashed, not passed through unhashed)", got, want)
	}

	// Sanity check against the OLD (buggy) behavior: the old code returned
	// l2 unhashed as the second level-1 node, giving H(H(l0||l1) || l2)
	// instead. Confirm the fixed function does NOT produce that value.
	oldBuggy := sha256.Sum256(append(append([]byte{}, h01[:]...), l2...))
	if string(got) == string(oldBuggy[:]) {
		t.Fatal("buildMerkleRoot still exhibits the old unhashed-odd-leaf bug")
	}
}

// TestBuildMerkleRoot_EvenLeavesUnaffected confirms the fix doesn't change
// behavior for the (already-correct) even-leaf-count case.
func TestBuildMerkleRoot_EvenLeavesUnaffected(t *testing.T) {
	leaf := func(b byte) []byte {
		h := sha256.Sum256([]byte{b})
		return h[:]
	}
	l0, l1 := leaf(0), leaf(1)

	got := buildMerkleRoot([][]byte{
		append([]byte{}, l0...),
		append([]byte{}, l1...),
	})

	want := sha256.Sum256(append(append([]byte{}, l0...), l1...))
	if string(got) != string(want[:]) {
		t.Fatalf("buildMerkleRoot with two leaves: got %x, want %x", got, want)
	}
}
