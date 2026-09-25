package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"synthos-collective/internal/chain"
)

func newTestChain(t *testing.T) *chain.Chain {
	t.Helper()
	c, err := chain.NewChain(chain.Genesis{ChainID: "store-test", Alloc: map[chain.Address]uint64{"0xgenesis": 1}})
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	return c
}

func finalizeEmptyBlock(t *testing.T, c *chain.Chain) *chain.Block {
	t.Helper()
	tip := c.Tip()
	b := &chain.Block{
		Header: chain.BlockHeader{
			Height:       tip.Header.Height + 1,
			ParentHash:   tip.Hash,
			ProposerID:   "validator-1",
			TxMerkleRoot: chain.EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []chain.Tx{},
	}
	if _, err := b.ComputeHash(); err != nil {
		t.Fatalf("compute hash: %v", err)
	}
	if err := c.FinalizeBlock(b); err != nil {
		t.Fatalf("finalize block at height %d: %v", b.Header.Height, err)
	}
	return b
}

// TestStore_SplitFormat_RoundTrip proves the new format's basic contract:
// whatever a chain currently holds in memory (its hot window) survives a
// Save/Load cycle intact, along with the hot-window bookkeeping needed to
// pick up exactly where it left off after a restart.
func TestStore_SplitFormat_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := newTestChain(t)
	c.SetHotWindow(3, st)
	if err := st.Save(c); err != nil { // archives genesis before it can ever be trimmed
		t.Fatalf("initial Save: %v", err)
	}
	for i := 0; i < 10; i++ {
		finalizeEmptyBlock(t, c)
		// Save after every block, matching how every real caller uses this
		// (internal/rpc/server.go saves right after every FinalizeBlock) --
		// see Save's doc comment for why that's required for every block to
		// get archived before it can be trimmed out of memory.
		if err := st.Save(c); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatalf("expected state.json to exist after Save: %v", err)
	}

	snap, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.ChainID != c.ChainID {
		t.Fatalf("expected chain ID %q, got %q", c.ChainID, snap.ChainID)
	}
	hw := c.HotWindowInfo()
	if snap.HotWindowStart != hw.Start {
		t.Fatalf("expected hot window start %d, got %d", hw.Start, snap.HotWindowStart)
	}
	if len(snap.Blocks) != 3 {
		t.Fatalf("expected Load to return exactly the 3-block hot window, got %d", len(snap.Blocks))
	}
	for i, blk := range snap.Blocks {
		wantHeight := hw.Start + uint64(i)
		if blk.Header.Height != wantHeight {
			t.Fatalf("snap.Blocks[%d] has height %d, expected %d", i, blk.Header.Height, wantHeight)
		}
	}
	if snap.State == nil || snap.State.Root() != c.State.Root() {
		t.Fatal("expected loaded state root to match the saved chain's state root")
	}

	// Every height, including ones trimmed out of the chain's own memory,
	// must still be individually recoverable via ColdBlockAt -- this is
	// what lets a resyncing peer rebuild full history via BlocksFrom.
	for h := uint64(0); h <= c.Height(); h++ {
		if _, ok := st.ColdBlockAt(h); !ok {
			t.Fatalf("expected ColdBlockAt(%d) to find an archived block after Save", h)
		}
	}
}

// TestStore_LegacyFormat_StillLoads proves a data directory holding only
// the original single-file chain.json (no state.json, no blocks/ dir) --
// i.e. every directory as it exists today, before any process has saved
// under the new format -- still loads correctly and unchanged.
func TestStore_LegacyFormat_StillLoads(t *testing.T) {
	dir := t.TempDir()
	c := newTestChain(t)
	for i := 0; i < 5; i++ {
		finalizeEmptyBlock(t, c)
	}

	type legacySnapshot struct {
		ChainID   string         `json:"chain_id"`
		TxChainID uint64         `json:"tx_chain_id"`
		Blocks    []*chain.Block `json:"blocks"`
		State     *chain.State   `json:"state"`
	}
	_, txChainID, blocks, state := c.SnapshotData()
	legacy := legacySnapshot{ChainID: c.ChainID, TxChainID: txChainID, Blocks: blocks, State: state}
	b, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy snapshot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chain.json"), b, 0o600); err != nil {
		t.Fatalf("write legacy chain.json: %v", err)
	}

	st, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap, err := st.Load()
	if err != nil {
		t.Fatalf("Load legacy format: %v", err)
	}
	if snap.ChainID != c.ChainID {
		t.Fatalf("expected chain ID %q, got %q", c.ChainID, snap.ChainID)
	}
	if len(snap.Blocks) != 6 { // genesis + 5
		t.Fatalf("expected all 6 legacy blocks, got %d", len(snap.Blocks))
	}
	if snap.HotWindowStart != 0 {
		t.Fatalf("expected HotWindowStart 0 for a legacy snapshot (nothing ever trimmed), got %d", snap.HotWindowStart)
	}
	if snap.HotWindowBaseState != nil || snap.HotWindowBaseBlock != nil {
		t.Fatal("expected no hot-window checkpoint in a legacy snapshot")
	}
}

// TestStore_MigratesLegacyFormatOnNextSave proves the whole point of the
// migration design: loading an old-format directory, reconstructing a
// chain from it exactly the way cmd/synthosd's main() does, and calling
// Save once is enough to fully transition that data directory to the new
// split format -- every historical block archived under blocks/, and
// state.json now the source of truth -- with no separate migration step.
func TestStore_MigratesLegacyFormatOnNextSave(t *testing.T) {
	dir := t.TempDir()
	original := newTestChain(t)
	for i := 0; i < 8; i++ {
		finalizeEmptyBlock(t, original)
	}
	_, txChainID, blocks, state := original.SnapshotData()
	type legacySnapshot struct {
		ChainID   string         `json:"chain_id"`
		TxChainID uint64         `json:"tx_chain_id"`
		Blocks    []*chain.Block `json:"blocks"`
		State     *chain.State   `json:"state"`
	}
	b, err := json.MarshalIndent(legacySnapshot{ChainID: original.ChainID, TxChainID: txChainID, Blocks: blocks, State: state}, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy snapshot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chain.json"), b, 0o600); err != nil {
		t.Fatalf("write legacy chain.json: %v", err)
	}

	st, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Reconstruct exactly the way cmd/synthosd/main.go does.
	restored := &chain.Chain{
		ChainID:   snap.ChainID,
		TxChainID: snap.TxChainID,
		State:     snap.State,
		DEX:       chain.NewDEX(),
		Oracle:    chain.NewOracle(),
		Blocks:    snap.Blocks,
		Mempool:   make(map[string]chain.Tx),
	}
	restored.SeedGenesisState(original.State) // any genesis-equivalent state works for this check

	// Archive the full legacy history FIRST, while maxHotBlocks is still
	// unset (0, unbounded) and every block is still in memory -- exactly
	// the order cmd/synthosd and cmd/rpcnode's main() use, and required so
	// nothing gets trimmed away before it's ever archived under blocks/.
	if err := st.Save(restored); err != nil {
		t.Fatalf("Save after legacy load: %v", err)
	}
	restored.RestoreHotWindow(snap.HotWindowStart, snap.HotWindowBaseState, snap.HotWindowBaseBlock, 3, st)
	if err := st.Save(restored); err != nil {
		t.Fatalf("Save after RestoreHotWindow: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatalf("expected state.json to exist after migrating Save: %v", err)
	}
	for h := uint64(0); h <= 8; h++ {
		if _, ok := st.ColdBlockAt(h); !ok {
			t.Fatalf("expected every legacy block (height %d) to be archived after the migrating Save", h)
		}
	}

	// A fresh Store pointed at the same directory must now load the new
	// format and see the trimmed hot window RestoreHotWindow/SetHotWindow
	// established, not the full legacy history.
	st2, err := New(dir)
	if err != nil {
		t.Fatalf("New (second store): %v", err)
	}
	reloaded, err := st2.Load()
	if err != nil {
		t.Fatalf("Load after migration: %v", err)
	}
	if len(reloaded.Blocks) != 3 {
		t.Fatalf("expected the post-migration load to see the bounded 3-block hot window, got %d blocks", len(reloaded.Blocks))
	}
}

func TestStore_ColdBlockAt_MissingReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := st.ColdBlockAt(999); ok {
		t.Fatal("expected ColdBlockAt for a never-archived height to return false")
	}
}

// TestStore_ArchiveBlocks_DoesNotRewriteOldArchivedBlocks proves Save's
// core efficiency property: once a block is safely behind the reorg-able
// tail, its archive file is written once and never touched again on
// subsequent saves -- this is what keeps Save's cost bounded by how much
// is genuinely new, instead of by total chain length.
func TestStore_ArchiveBlocks_DoesNotRewriteOldArchivedBlocks(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := newTestChain(t)
	c.SetHotWindow(50, st)
	for i := 0; i < 20; i++ {
		finalizeEmptyBlock(t, c)
	}
	if err := st.Save(c); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	oldBlockPath := st.blockPath(1) // well behind reorgTailSize from a tip at height 20
	before, err := os.Stat(oldBlockPath)
	if err != nil {
		t.Fatalf("expected archived block file for height 1: %v", err)
	}

	for i := 0; i < 3; i++ {
		finalizeEmptyBlock(t, c)
	}
	if err := st.Save(c); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	after, err := os.Stat(oldBlockPath)
	if err != nil {
		t.Fatalf("expected archived block file for height 1 to still exist: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("expected height 1's archive file to be untouched by a later Save (old: %v, new: %v)", before.ModTime(), after.ModTime())
	}
}
