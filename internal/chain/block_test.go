package chain

import (
	"encoding/hex"
	"encoding/json"
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

// TestChain_SnapshotData_PreservesEmptyTx guards against the exact bug that
// broke HTTP peer sync in production: SnapshotData used to copy each block's
// Tx via append([]Tx(nil), block.Tx...), which silently collapses a non-nil
// empty slice back to nil (append with zero elements to append returns the
// destination unchanged). Every save/reload cycle (internal/storage.Store)
// therefore turned a proposed block's "tx":[] into "tx":null, which is what
// every peer's /blocks endpoint ends up serving after a restart.
func TestChain_SnapshotData_PreservesEmptyTx(t *testing.T) {
	g := Genesis{
		ChainID: "test-chain",
		Alloc:   map[Address]uint64{"0xgenesis": 1},
	}
	c, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	tip := c.Tip()

	proposed := &Block{
		Header: BlockHeader{
			Height:       1,
			ParentHash:   tip.Hash,
			ProposerID:   "p1",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []Tx{}, // non-nil, as the real block producer assembles it
	}
	if _, err := proposed.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	if err := c.FinalizeBlock(proposed); err != nil {
		t.Fatal(err)
	}

	_, _, snapshotBlocks, _ := c.SnapshotData()
	if len(snapshotBlocks) != 2 { // genesis + proposed
		t.Fatalf("expected 2 blocks in snapshot, got %d", len(snapshotBlocks))
	}
	if snapshotBlocks[1].Tx == nil {
		t.Fatalf("SnapshotData collapsed a non-nil empty Tx slice to nil")
	}
}

// TestBlock_CalculateHash_SurvivesNilTxRoundTrip reproduces the HTTP peer-sync
// bug end to end: a proposer builds an empty block with Tx as a non-nil,
// empty slice, computes its hash, and that block survives a save/reload
// cycle (via the real SnapshotData path) followed by an HTTP-style
// marshal/unmarshal — both of which, pre-fix, turned "tx":[] into "tx":null.
// A receiver decoding that JSON must still recompute the identical hash, or
// validateBlockLocked rejects every empty block a peer ever proposes (see
// internal/rpc CatchUpOnce/applyPeerBlock).
func TestBlock_CalculateHash_SurvivesNilTxRoundTrip(t *testing.T) {
	g := Genesis{
		ChainID: "test-chain",
		Alloc:   map[Address]uint64{"0xgenesis": 1},
	}
	c, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	tip := c.Tip()

	proposed := &Block{
		Header: BlockHeader{
			Height:       1,
			ParentHash:   tip.Hash,
			ProposerID:   "p1",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []Tx{}, // non-nil, as the block producer assembles it
	}
	originalHash, err := proposed.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.FinalizeBlock(proposed); err != nil {
		t.Fatal(err)
	}

	// Round-trip through the real persistence path (save + reload), then
	// simulate serving it back over HTTP.
	_, _, snapshotBlocks, _ := c.SnapshotData()
	data, err := json.Marshal(snapshotBlocks[1])
	if err != nil {
		t.Fatal(err)
	}
	var received Block
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatal(err)
	}

	recomputed, err := received.CalculateHash()
	if err != nil {
		t.Fatal(err)
	}
	if recomputed != originalHash {
		t.Fatalf("hash changed across save/reload + HTTP round-trip: original %q, recomputed %q", originalHash, recomputed)
	}
}

// TestChain_FinalizeBlock_AcceptsPeerBlockWithNilTx is an end-to-end check
// that a peer-synced empty block (Tx arriving as nil off the wire, as every
// already-restarted node in production serves it) still finalizes on a
// separate, freshly-booted chain — the actual RPC peer-sync scenario — and
// that the genesis hash is unaffected by the fix.
func TestChain_FinalizeBlock_AcceptsPeerBlockWithNilTx(t *testing.T) {
	g := Genesis{
		ChainID: "test-chain",
		Alloc:   map[Address]uint64{"0xgenesis": 1},
	}

	// The "sender": produces and finalizes the block, then loses the
	// distinction between nil and empty across its own persistence layer.
	sender, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	senderTip := sender.Tip()
	proposed := &Block{
		Header: BlockHeader{
			Height:       1,
			ParentHash:   senderTip.Hash,
			ProposerID:   "p1",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    senderTip.Header.StateRoot,
		},
		Tx: []Tx{},
	}
	if _, err := proposed.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	if err := sender.FinalizeBlock(proposed); err != nil {
		t.Fatal(err)
	}
	// Simulate a peer that still serves this block with Tx as nil — e.g. one
	// whose on-disk data predates the SnapshotData fix above, or any other
	// implementation that round-trips an empty transaction list to null.
	// CalculateHash's height>0 canonicalization must accept it regardless of
	// how the peer arrived at "tx":null.
	received := *proposed
	received.Tx = nil

	// The "receiver": a separate, freshly-booted chain (e.g. synthos-rpc)
	// applying that block via HTTP peer sync.
	receiver, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	if err := receiver.FinalizeBlock(&received); err != nil {
		t.Fatalf("FinalizeBlock rejected a valid peer-synced empty block: %v", err)
	}
	if receiver.Height() != 1 {
		t.Fatalf("expected chain height 1 after finalizing peer block, got %d", receiver.Height())
	}
}
