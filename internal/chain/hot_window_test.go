package chain

import (
	"crypto/ed25519"
	"testing"
)

// fakeColdStore is a minimal in-memory stand-in for storage.Store's
// on-disk block archive, used only to prove Chain's hot-window trimming
// and cold-fallback logic against a real ColdBlockReader implementation
// without touching a filesystem. archive mimics what Store.Save's
// archiveBlocks does in production: record every block it's handed,
// keyed by height, so a later ColdBlockAt can serve it back.
type fakeColdStore struct {
	byHeight map[uint64]*Block
}

func newFakeColdStore() *fakeColdStore {
	return &fakeColdStore{byHeight: make(map[uint64]*Block)}
}

func (f *fakeColdStore) archive(blocks []*Block) {
	for _, b := range blocks {
		if b != nil {
			f.byHeight[b.Header.Height] = b
		}
	}
}

func (f *fakeColdStore) ColdBlockAt(height uint64) (*Block, bool) {
	b, ok := f.byHeight[height]
	return b, ok
}

// finalizeEmptyBlock finalizes a plain, unsigned block on top of the
// current tip (no validator set configured, so no signatures are needed --
// see FinalizeBlock's doc comment). No transactions, so the state root
// never changes block to block, keeping these tests focused purely on the
// hot-window mechanics rather than economics/state.
func finalizeEmptyBlock(t *testing.T, c *Chain, proposerID string) *Block {
	t.Helper()
	tip := c.Tip()
	b := &Block{
		Header: BlockHeader{
			Height:       tip.Header.Height + 1,
			ParentHash:   tip.Hash,
			ProposerID:   proposerID,
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []Tx{},
	}
	if _, err := b.ComputeHash(); err != nil {
		t.Fatalf("compute hash: %v", err)
	}
	if err := c.FinalizeBlock(b); err != nil {
		t.Fatalf("finalize block at height %d: %v", b.Header.Height, err)
	}
	return b
}

func newTestChain(t *testing.T) *Chain {
	t.Helper()
	c, err := NewChain(Genesis{ChainID: "hot-window-test", Alloc: map[Address]uint64{"0xgenesis": 1}})
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	return c
}

// TestHotWindow_DefaultIsUnboundedAndUnchanged proves that a chain which
// never calls SetHotWindow behaves exactly as it did before this feature
// existed: Blocks keeps every finalized block, hotWindowStart never
// advances, and BlockAt keeps using a plain direct index. This is the
// backward-compatibility guarantee every existing test, tool
// (cmd/l1check), and not-yet-upgraded deployment depends on.
func TestHotWindow_DefaultIsUnboundedAndUnchanged(t *testing.T) {
	c := newTestChain(t)
	const n = 50
	for i := 0; i < n; i++ {
		finalizeEmptyBlock(t, c, "validator-1")
	}
	if got := len(c.Blocks); got != n+1 { // +1 for genesis
		t.Fatalf("expected unbounded Blocks to hold all %d blocks, got %d", n+1, got)
	}
	if c.hotWindowStart != 0 {
		t.Fatalf("expected hotWindowStart to stay 0 when SetHotWindow was never called, got %d", c.hotWindowStart)
	}
	for h := uint64(0); h <= n; h++ {
		if c.BlockAt(h) == nil {
			t.Fatalf("expected BlockAt(%d) to still be served directly from memory, got nil", h)
		}
	}
}

// TestHotWindow_TrimsOldBlocksAndServesFromColdReader is the core positive
// case: once SetHotWindow is configured, memory stops growing past the
// bound, and a height that got trimmed out is still fully retrievable --
// both individually (BlockAt) and as part of a range (BlocksFrom) -- via
// the configured ColdBlockReader, exactly like a resyncing peer catching
// up from genesis would need.
func TestHotWindow_TrimsOldBlocksAndServesFromColdReader(t *testing.T) {
	c := newTestChain(t)
	cold := newFakeColdStore()
	const maxHot = 5
	c.SetHotWindow(maxHot, cold)
	cold.archive(c.Blocks) // genesis

	const totalNewBlocks = 20
	for i := 0; i < totalNewBlocks; i++ {
		b := finalizeEmptyBlock(t, c, "validator-1")
		// Mimic Store.Save's archive step running after every finalize --
		// production always archives the current hot window on every save,
		// which by the time a block is old enough to be trimmed has long
		// since covered it.
		cold.archive([]*Block{b})
	}

	if got := len(c.Blocks); got > maxHot {
		t.Fatalf("expected Blocks to stay bounded at %d, got %d", maxHot, got)
	}
	if c.hotWindowStart == 0 {
		t.Fatal("expected hotWindowStart to have advanced past 0 after exceeding the hot window bound")
	}

	tipHeight := c.Height()
	if tipHeight != totalNewBlocks {
		t.Fatalf("expected tip height %d, got %d", totalNewBlocks, tipHeight)
	}

	// Every height from genesis to tip must still be retrievable, whether
	// it's still hot in memory or had to fall through to cold storage.
	for h := uint64(0); h <= tipHeight; h++ {
		blk := c.BlockAt(h)
		if blk == nil {
			t.Fatalf("BlockAt(%d) returned nil after trimming, even with a cold reader configured", h)
		}
		if blk.Header.Height != h {
			t.Fatalf("BlockAt(%d) returned a block for height %d instead", h, blk.Header.Height)
		}
	}

	// A resyncing peer's request for the full range from genesis must come
	// back complete and in order, seamlessly spanning the cold/hot boundary.
	all := c.BlocksFrom(0)
	if len(all) != int(tipHeight)+1 {
		t.Fatalf("expected BlocksFrom(0) to return all %d blocks, got %d", tipHeight+1, len(all))
	}
	for i, blk := range all {
		if blk.Header.Height != uint64(i) {
			t.Fatalf("BlocksFrom(0)[%d] has height %d, expected %d (out of order or gap)", i, blk.Header.Height, i)
		}
	}

	// And BlockAt for a trimmed height with NO cold reader configured must
	// fail safe (nil), not panic or silently fabricate anything.
	c2 := newTestChain(t)
	c2.SetHotWindow(maxHot, nil)
	for i := 0; i < totalNewBlocks; i++ {
		finalizeEmptyBlock(t, c2, "validator-1")
	}
	if blk := c2.BlockAt(0); blk != nil {
		t.Fatalf("expected BlockAt(0) to be nil once trimmed with no cold reader configured, got a block")
	}
}

// TestHotWindow_TryReorgStillWorksWithinTheWindowAfterTrimming proves the
// riskiest part of this change: TryReorg's fork-choice replay (which used
// to always start from genesis) must still correctly reconstruct state and
// switch to a heavier competing block at the current tip after real
// trimming has happened -- using the hot-window checkpoint instead of
// genesis. This is the same scenario as
// TestChain_TryReorg_SwitchesToHeavierFullyValidBranch in
// authorization_test.go, just after enough blocks have been finalized
// first to force genesis itself out of memory.
func TestHotWindow_TryReorgStillWorksWithinTheWindowAfterTrimming(t *testing.T) {
	c := newTestChain(t)
	pub1, priv1 := mustGenerateKey(t)
	pub2, priv2 := mustGenerateKey(t)
	c.SetValidatorSet(map[string]ed25519.PublicKey{"validator-1": pub1, "validator-2": pub2}, 1)

	cold := newFakeColdStore()
	const maxHot = 4
	c.SetHotWindow(maxHot, cold)
	cold.archive(c.Blocks)

	// Finalize enough signed blocks to push genesis well out of the hot
	// window, all approved by validator-1.
	const prefixBlocks = 10
	for i := 0; i < prefixBlocks; i++ {
		tip := c.Tip()
		b := &Block{
			Header: BlockHeader{
				Height:       tip.Header.Height + 1,
				ParentHash:   tip.Hash,
				ProposerID:   "validator-1",
				TxMerkleRoot: EmptyTxMerkleRoot,
				StateRoot:    tip.Header.StateRoot,
			},
			Tx: []Tx{},
		}
		if _, err := b.ComputeHash(); err != nil {
			t.Fatal(err)
		}
		b.ProposerSignature = signHex(priv1, []byte(b.Hash))
		b.QuorumSignatures = map[string]string{"validator-1": signHex(priv1, b.QuorumApprovalMessage())}
		if err := c.FinalizeBlock(b); err != nil {
			t.Fatalf("finalize prefix block %d: %v", i, err)
		}
		cold.archive([]*Block{b})
	}
	if c.hotWindowStart == 0 {
		t.Fatal("expected hotWindowStart to have advanced past genesis before the reorg attempt")
	}

	tip := c.Tip()
	contestedHeight := tip.Header.Height
	parentTip := c.BlockAt(contestedHeight - 1)
	if parentTip == nil {
		t.Fatal("expected the block just below the current tip to still be retrievable")
	}

	heavy := &Block{
		Header: BlockHeader{
			Height:       contestedHeight,
			ParentHash:   parentTip.Hash,
			ProposerID:   "validator-2",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    parentTip.Header.StateRoot,
		},
		Tx: []Tx{},
	}
	if _, err := heavy.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	heavy.ProposerSignature = signHex(priv2, []byte(heavy.Hash))
	happroval := heavy.QuorumApprovalMessage()
	heavy.QuorumSignatures = map[string]string{
		"validator-1": signHex(priv1, happroval),
		"validator-2": signHex(priv2, happroval),
	}

	reorged, err := c.TryReorg(contestedHeight, []*Block{heavy})
	if err != nil {
		t.Fatalf("TryReorg returned error after trimming: %v", err)
	}
	if !reorged {
		t.Fatal("expected TryReorg to switch to the strictly heavier competing block even after genesis was trimmed out of memory")
	}
	if got := c.Tip().Hash; got != heavy.Hash {
		t.Fatalf("expected chain tip to be the heavier block %q, got %q", heavy.Hash, got)
	}
}

// TestHotWindow_TryReorgRefusesForkHeightBelowTheWindow proves the
// deliberate, documented restriction: TryReorg must fail safe (refuse,
// never guess or silently misbehave) when asked to contest a height that's
// already aged out of the retained hot window, since there is no longer
// enough in-memory history to safely replay a competing branch from there.
func TestHotWindow_TryReorgRefusesForkHeightBelowTheWindow(t *testing.T) {
	c := newTestChain(t)
	pub1, priv1 := mustGenerateKey(t)
	c.SetValidatorSet(map[string]ed25519.PublicKey{"validator-1": pub1}, 1)

	cold := newFakeColdStore()
	const maxHot = 3
	c.SetHotWindow(maxHot, cold)
	cold.archive(c.Blocks)

	const totalBlocks = 10
	for i := 0; i < totalBlocks; i++ {
		tip := c.Tip()
		b := &Block{
			Header: BlockHeader{
				Height:       tip.Header.Height + 1,
				ParentHash:   tip.Hash,
				ProposerID:   "validator-1",
				TxMerkleRoot: EmptyTxMerkleRoot,
				StateRoot:    tip.Header.StateRoot,
			},
			Tx: []Tx{},
		}
		if _, err := b.ComputeHash(); err != nil {
			t.Fatal(err)
		}
		b.ProposerSignature = signHex(priv1, []byte(b.Hash))
		b.QuorumSignatures = map[string]string{"validator-1": signHex(priv1, b.QuorumApprovalMessage())}
		if err := c.FinalizeBlock(b); err != nil {
			t.Fatalf("finalize block %d: %v", i, err)
		}
		cold.archive([]*Block{b})
	}

	if c.hotWindowStart < 2 {
		t.Fatalf("test setup problem: expected hotWindowStart comfortably above 1, got %d", c.hotWindowStart)
	}
	staleHeight := c.hotWindowStart - 1

	fake := &Block{Header: BlockHeader{Height: staleHeight, ParentHash: "0xdoesnotmatter", ProposerID: "validator-1", TxMerkleRoot: EmptyTxMerkleRoot}}
	if _, err := fake.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	reorged, err := c.TryReorg(staleHeight, []*Block{fake})
	if reorged {
		t.Fatal("expected TryReorg to refuse a fork height below the retained hot window, not reorg")
	}
	if err == nil {
		t.Fatal("expected TryReorg to return an explanatory error for a fork height below the hot window")
	}
}

// TestHotWindow_SetHotWindowTrimsExistingBacklogImmediately proves
// SetHotWindow isn't only forward-looking: calling it on a chain that
// already holds more than maxHotBlocks (e.g. right after restoring a large
// legacy snapshot -- see storage.Store's migration path) trims it down
// immediately, rather than waiting for the next block to be finalized.
func TestHotWindow_SetHotWindowTrimsExistingBacklogImmediately(t *testing.T) {
	c := newTestChain(t)
	for i := 0; i < 30; i++ {
		finalizeEmptyBlock(t, c, "validator-1")
	}
	if len(c.Blocks) != 31 {
		t.Fatalf("test setup problem: expected 31 blocks before trimming, got %d", len(c.Blocks))
	}

	cold := newFakeColdStore()
	cold.archive(c.Blocks)
	c.SetHotWindow(10, cold)

	if len(c.Blocks) != 10 {
		t.Fatalf("expected SetHotWindow to immediately trim the existing backlog down to 10, got %d", len(c.Blocks))
	}
	if c.hotWindowStart != 21 {
		t.Fatalf("expected hotWindowStart 21 (31 blocks - 10 kept), got %d", c.hotWindowStart)
	}
	// Old heights must still be reachable via the cold reader.
	if blk := c.BlockAt(0); blk == nil || blk.Header.Height != 0 {
		t.Fatal("expected genesis to still be retrievable via the cold reader after an immediate backlog trim")
	}
}

func TestHotWindow_BlocksFromOutOfRangeReturnsNil(t *testing.T) {
	c := newTestChain(t)
	for i := 0; i < 3; i++ {
		finalizeEmptyBlock(t, c, "validator-1")
	}
	if got := c.BlocksFrom(100); got != nil {
		t.Fatalf("expected BlocksFrom for a height past the tip to return nil, got %d blocks", len(got))
	}
}

func TestHotWindow_ColdReaderInterfaceSatisfiedByFakeStore(t *testing.T) {
	var _ ColdBlockReader = (*fakeColdStore)(nil)
}
