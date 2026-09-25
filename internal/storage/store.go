package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"synthos-collective/internal/chain"
)

// Store persists chain data to disk in a simple JSON form.
// This is intentionally minimal to keep the codebase auditable; we can later
// migrate to BoltDB/Badger/RocksDB without changing the Chain API.
//
// On disk, Store keeps two things:
//   - state.json: chain ID, current State, and hot-window bookkeeping. Cheap
//     to write (proportional to account count, not chain length), so it's
//     safe to rewrite on nearly every Save call the way the old single-file
//     format always did.
//   - blocks/<height>.json: one file per finalized block, written once and
//     (outside a small reorg-able tail near the tip) never rewritten. This
//     is both Save's incremental block archive and ColdBlockAt's read path
//     for a height a Chain has trimmed out of its own memory (see
//     chain.Chain.SetHotWindow) -- so a chain can bound its RAM use while
//     still being able to answer a resyncing peer's request for any block
//     all the way back to genesis.
//
// A directory holding the old, single-file chain.json format (chain ID +
// every block ever finalized + state, all in one JSON blob) is still read
// correctly by Load -- see loadLegacyFormat. The first Save call after that
// migrates it forward automatically: every block not yet archived under
// blocks/ gets written once, and state.json starts getting written from
// then on. No separate migration step is needed.
type Store struct {
	Dir string
}

var ErrNoDir = errors.New("store dir required")

// reorgTailSize is how many of the most recent blocks archiveBlocks always
// rewrites, even if a file already exists for that height. Chain.TryReorg's
// only production caller only ever contests a height at or very near the
// current tip (see its doc comment), so a block that far back can still be
// legitimately replaced by a fork-choice reorg after it was first written;
// anything older than that is immutable once finalized, so it's only
// written the first time its file is missing.
const reorgTailSize = 8

func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, ErrNoDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "blocks"), 0o755); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

type snapshot struct {
	ChainID   string         `json:"chain_id"`
	TxChainID uint64         `json:"tx_chain_id"`
	Blocks    []*chain.Block `json:"blocks"`
	State     *chain.State   `json:"state"`

	// HotWindowStart/HotWindowBaseState/HotWindowBaseBlock are zero-valued
	// for a legacy-format load (nothing was ever trimmed), which is exactly
	// the right default -- see chain.Chain.RestoreHotWindow.
	HotWindowStart     uint64       `json:"hot_window_start,omitempty"`
	HotWindowBaseState *chain.State `json:"hot_window_base_state,omitempty"`
	HotWindowBaseBlock *chain.Block `json:"hot_window_base_block,omitempty"`
}

// Snapshot is the exported view for loading.
type Snapshot = snapshot

// stateFile is state.json's on-disk shape: everything about a chain except
// its block history, which lives under blocks/ instead (see archiveBlocks).
type stateFile struct {
	ChainID            string       `json:"chain_id"`
	TxChainID          uint64       `json:"tx_chain_id"`
	State              *chain.State `json:"state"`
	HotWindowStart     uint64       `json:"hot_window_start"`
	HotWindowBaseState *chain.State `json:"hot_window_base_state,omitempty"`
	HotWindowBaseBlock *chain.Block `json:"hot_window_base_block,omitempty"`
}

// Save archives every block currently in c's in-memory hot window (see
// chain.Chain.SetHotWindow) plus its current state. IMPORTANT: this only
// archives blocks c still holds in memory -- it relies on being called
// again soon after every new block is finalized (exactly how every
// existing caller already uses it: internal/rpc/server.go calls Save right
// after every successful FinalizeBlock/TryReorg) so each block gets
// archived at least once while it's still hot, before enough later blocks
// accumulate to trim it out of memory. A caller that finalizes many blocks
// in a row without ever calling Save in between can lose the ability to
// archive an old block once it's trimmed out from under it -- don't do
// that; save after every block the way production already does.
func (s *Store) Save(c *chain.Chain) error {
	chainID, txChainID, blocks, state := c.SnapshotData()
	if err := s.archiveBlocks(blocks); err != nil {
		return fmt.Errorf("archive blocks: %w", err)
	}

	hw := c.HotWindowInfo()
	sf := stateFile{
		ChainID:            chainID,
		TxChainID:          txChainID,
		State:              state,
		HotWindowStart:     hw.Start,
		HotWindowBaseState: hw.BaseState,
		HotWindowBaseBlock: hw.BaseBlock,
	}
	b, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.Dir, "state.json.tmp")
	final := filepath.Join(s.Dir, "state.json")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// archiveBlocks writes any block in blocks that isn't already durably
// archived under blocks/<height>.json, always (re)writing the last
// reorgTailSize of them regardless -- see reorgTailSize's doc comment.
// blocks is normally just a chain's current in-memory hot window (a few
// thousand entries at most, bounded regardless of total chain height), so
// this is cheap and its cost never grows with chain length -- unlike the
// old format's full-history re-marshal on every save.
func (s *Store) archiveBlocks(blocks []*chain.Block) error {
	if err := os.MkdirAll(filepath.Join(s.Dir, "blocks"), 0o755); err != nil {
		return err
	}
	n := len(blocks)
	for i, blk := range blocks {
		if blk == nil {
			continue
		}
		force := i >= n-reorgTailSize
		if err := s.archiveOneBlock(blk, force); err != nil {
			return fmt.Errorf("archive block at height %d: %w", blk.Header.Height, err)
		}
	}
	return nil
}

func (s *Store) blockPath(height uint64) string {
	return filepath.Join(s.Dir, "blocks", fmt.Sprintf("%d.json", height))
}

func (s *Store) archiveOneBlock(blk *chain.Block, force bool) error {
	path := s.blockPath(blk.Header.Height)
	if !force {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
	}
	b, err := json.Marshal(blk)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ColdBlockAt implements chain.ColdBlockReader, reading back a single
// archived block by height on demand -- the fallback chain.Chain.BlockAt
// and chain.Chain.BlocksFrom use for any height a chain has trimmed out of
// its own in-memory hot window. Returns false (never an error) for a
// missing or corrupt file, matching ColdBlockReader's contract that a miss
// just means "not found".
func (s *Store) ColdBlockAt(height uint64) (*chain.Block, bool) {
	b, err := os.ReadFile(s.blockPath(height))
	if err != nil {
		return nil, false
	}
	var blk chain.Block
	if err := json.Unmarshal(b, &blk); err != nil {
		return nil, false
	}
	return &blk, true
}

func (s *Store) Load() (*Snapshot, error) {
	sf, ok, err := s.loadStateFile()
	if err != nil {
		return nil, err
	}
	if ok {
		return s.loadSplitFormat(sf)
	}
	return s.loadLegacyFormat()
}

func (s *Store) loadStateFile() (*stateFile, bool, error) {
	path := filepath.Join(s.Dir, "state.json")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var sf stateFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return nil, false, err
	}
	return &sf, true, nil
}

// loadSplitFormat reconstructs the chain's hot-window blocks by reading
// blocks/<height>.json sequentially from HotWindowStart until a height is
// missing -- which, thanks to Save's tmp-file-then-rename writes, only ever
// happens exactly at the true current tip (a crash mid-write leaves either
// the old file intact or a stray .tmp, never a corrupt final file). This
// deliberately does NOT load the chain's full history into memory: only the
// hot window state.json actually recorded, keeping startup memory bounded
// the same way steady-state operation is.
func (s *Store) loadSplitFormat(sf *stateFile) (*Snapshot, error) {
	var blocks []*chain.Block
	for h := sf.HotWindowStart; ; h++ {
		blk, ok := s.ColdBlockAt(h)
		if !ok {
			break
		}
		blocks = append(blocks, blk)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("state.json present but no block files found from height %d onward under %s", sf.HotWindowStart, filepath.Join(s.Dir, "blocks"))
	}
	return &Snapshot{
		ChainID:            sf.ChainID,
		TxChainID:          sf.TxChainID,
		Blocks:             blocks,
		State:              sf.State,
		HotWindowStart:     sf.HotWindowStart,
		HotWindowBaseState: sf.HotWindowBaseState,
		HotWindowBaseBlock: sf.HotWindowBaseBlock,
	}, nil
}

// loadLegacyFormat reads the original, pre-split single-file chain.json
// (chain ID, every block ever finalized, and state, all in one JSON blob)
// unchanged, for any data directory that hasn't been saved under the new
// format yet. The returned Snapshot's HotWindowStart/HotWindowBase* are
// left at their zero values, which correctly means "nothing trimmed yet" --
// the caller's chain.Chain ends up with the same unbounded-in-memory
// behavior it always had, until the next Save archives everything under
// blocks/ and starts writing state.json (see this package's doc comment).
func (s *Store) loadLegacyFormat() (*Snapshot, error) {
	path := filepath.Join(s.Dir, "chain.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}
