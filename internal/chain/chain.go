package chain

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Chain is a minimal L1 ledger: blocks + state + mempool.
// Consensus is intentionally left as a pluggable component (agents coordinate it).
type Chain struct {
	mu sync.RWMutex

	ChainID string
	// TxChainID is the signed transaction domain. It must be unique per network.
	// Zero is treated as 1 only for backwards-compatible local snapshots.
	TxChainID uint64
	State     *State
	DEX       *DEX
	Oracle    *Oracle

	Blocks  []*Block
	Mempool map[string]Tx

	// validatorKeys maps a registered validator's ProposerID/VoterID to their
	// ed25519 public key. When non-empty, block acceptance (validateBlockLocked)
	// requires every non-genesis block to carry a valid ProposerSignature from
	// a key in this set, plus QuorumSignatures from at least requiredQuorum
	// distinct keys in this set -- see SetValidatorSet. Left empty, a chain
	// accepts unsigned blocks exactly as before (needed so existing
	// single-node/dev/test setups that don't configure a validator set keep
	// working); a production deployment is only protected once
	// SetValidatorSet is actually called (see cmd/synthosd, cmd/rpcnode).
	validatorKeys  map[string]ed25519.PublicKey
	requiredQuorum int

	// genesisState is a clone of State immediately after genesis (before any
	// block is applied). It lets TryReorg safely replay an alternative branch
	// from the fork point without touching the live State until the
	// alternative branch is fully verified. Chains built via NewChain set this
	// automatically; a chain restored from a storage snapshot must call
	// SeedGenesisState explicitly before TryReorg is usable.
	genesisState *State

	// authEnforceFromHeight is the lowest block height at which
	// validateBlockLocked requires ProposerSignature/QuorumSignatures (see
	// SetAuthEnforceFromHeight). It defaults to 0, meaning "enforce from
	// height 1 onward" -- i.e. every non-genesis block once validatorKeys is
	// configured, exactly as SetValidatorSet originally behaved. It exists
	// so a chain that already has a long history predating this
	// authentication feature can grandfather that existing history in
	// (those blocks were never signed -- the fields didn't exist yet -- so
	// requiring signatures on them would make it permanently impossible for
	// any node to sync past that point) while still strictly requiring
	// signatures on every block from the given height onward.
	authEnforceFromHeight uint64

	// stateRootEnforceFromHeight is the lowest block height at which
	// validateBlockLocked/FinalizeBlock require the replayed state root to
	// match Header.StateRoot exactly (see SetStateRootEnforceFromHeight).
	// Below it, that one check is skipped -- hash, tx-merkle-root, parent
	// linkage, timestamp, and (once enforced) signature checks still apply in
	// full. This exists for the same reason as authEnforceFromHeight: a prior
	// version of State.Root() folded in self-attested, node-local immune/
	// sovereign-proof data (see core.go's Root()), so blocks produced while
	// that was true have a declared StateRoot no independently-bootstrapped
	// node can ever reproduce, through no fault of their real transaction
	// history. Grandfathering that range in is the only way a new or
	// resyncing node can ever get past it.
	stateRootEnforceFromHeight uint64

	// stateCorrections are one-time, explicit, height-gated adjustments to
	// State applied deterministically during that height's validation and
	// finalization, before the resulting state's Root() is checked or
	// committed (see SetIrregularStateCorrections's doc comment).
	stateCorrections []StateCorrection

	// --- Bounded in-memory block retention (see SetHotWindow) ---
	//
	// Blocks holds every finalized block from genesis by default (index ==
	// height), which is how every pre-existing caller and test still
	// behaves: maxHotBlocks starts at 0 ("unbounded"), so none of this new
	// machinery does anything until SetHotWindow/RestoreHotWindow is
	// actually called. A long-running chain that does opt in trims the
	// oldest entries out of Blocks once it grows past maxHotBlocks, so RAM
	// use stops growing once it reaches the configured window instead of
	// growing forever with chain height.

	// hotWindowStart is the height of Blocks[0]. Zero for a chain that has
	// never trimmed anything (Blocks still holds full history from genesis,
	// exactly as before this field existed). Every direct height->index
	// lookup into Blocks (blockAtLocked, BlocksFrom, TryReorg's prefix
	// build) must subtract this before indexing.
	hotWindowStart uint64

	// maxHotBlocks bounds how many of the most recent finalized blocks stay
	// in Blocks; <= 0 (the default) means unbounded. Set via SetHotWindow.
	maxHotBlocks int

	// coldBlocks, when non-nil, is consulted by blockAtLocked/BlocksFrom for
	// any height below hotWindowStart that trimming has evicted from
	// memory. Set via SetHotWindow/RestoreHotWindow, normally to the same
	// *storage.Store the chain is periodically saved to -- see
	// storage.Store.ColdBlockAt.
	coldBlocks ColdBlockReader

	// hotWindowBaseState/hotWindowBaseBlock cache the state and the anchor
	// block exactly as of height hotWindowStart-1 -- i.e. immediately
	// before the current hot window begins. TryReorg needs this as its
	// replay starting point once genesis itself has been trimmed out of
	// memory (see hotWindowBaseStateLocked/hotWindowBaseBlockLocked, which
	// fall back to genesisState/Blocks[0] whenever hotWindowStart is still
	// 0, so these two fields are only ever meaningfully populated after the
	// first real trim -- see trimHotWindowLocked). RestoreHotWindow
	// reconstructs them from a storage.Store snapshot after a restart.
	hotWindowBaseState *State
	hotWindowBaseBlock *Block
}

// ColdBlockReader supplies a finalized block that has aged out of Chain's
// bounded in-memory retention window (see SetHotWindow), read back from
// wherever it was durably archived. Implemented by *storage.Store, which
// reads blocks/<height>.json on demand -- callers never need to hold full
// chain history in RAM just to serve an old block to a resyncing peer.
type ColdBlockReader interface {
	// ColdBlockAt returns the finalized block at height, and false if it
	// isn't available (never existed, or the archive is missing/corrupt for
	// some other reason -- callers must treat false as "not found", never
	// panic or fabricate a stand-in block).
	ColdBlockAt(height uint64) (*Block, bool)
}

var (
	ErrNoGenesis = errors.New("genesis not initialized")
	ErrBadBlock  = errors.New("bad block")

	// ErrStateRootMismatch is validateBlockLocked's verdict specifically
	// when every check on the block's own bytes already passed (correct
	// parent/height, correct hash, correct tx-merkle-root, valid tx
	// signatures, valid proposer authorization) but this node's own
	// recomputed post-apply state root doesn't match the block's declared
	// Header.StateRoot. It wraps ErrBadBlock (errors.Is against ErrBadBlock
	// still matches), but callers that decide whether a rejection is real,
	// provable proposer misbehavior -- e.g. node.Node.HandleProposal
	// deciding whether to call SlashingTracker.RecordInvalidBlock, which
	// debits real chain balance -- must check for this specific sentinel
	// first and treat it differently. Every OTHER validateBlockLocked
	// failure is a structural fact about the block itself, true for any
	// validator regardless of their own state, so any honest validator
	// would independently reach the same verdict. This one check is not:
	// it also depends on this node's own State already agreeing with the
	// proposer's, before the proposed block is even applied. A node whose
	// own state has drifted from the network's for any reason (a
	// transient race, a prior soft desync, still catching up on a roster
	// change the proposer already knows about) will trip this exact check
	// against a perfectly honest proposer -- and keep tripping it forever,
	// for every future block too, since nothing about a later block
	// changes an already-wrong starting point. Treating that as proof of
	// misbehavior and slashing the proposer for it turns one node's own
	// benign, transient disagreement into a permanent, self-inflicted
	// state divergence -- see HandleProposal's use of this sentinel for
	// the live incident that motivated it.
	ErrStateRootMismatch = fmt.Errorf("%w: state root mismatch", ErrBadBlock)
)

func NewChain(genesis Genesis) (*Chain, error) {
	st, err := genesis.ToState()
	if err != nil {
		return nil, err
	}
	c := &Chain{
		ChainID:   genesis.ChainID,
		TxChainID: genesis.TransactionChainID(),
		State:     st,
		DEX:       NewDEX(),
		Oracle:    NewOracle(),
		Blocks:    make([]*Block, 0, 1024),
		Mempool:   make(map[string]Tx),
	}

	// stateRoot/hash normally come from a fresh computation, but a genesis
	// that's already live can pin them instead -- see
	// Genesis.PinnedGenesisStateRoot/PinnedGenesisHash for why that matters
	// (the hashing formula those functions would otherwise be recomputed
	// with has changed over time, and a long-running node never recomputes
	// its own genesis).
	stateRoot := st.Root()
	if pinned, ok := genesis.PinnedGenesisStateRoot(); ok {
		stateRoot = pinned
	}

	gb := &Block{
		Header: BlockHeader{
			Height:       0,
			ParentHash:   "0x0",
			Timestamp:    time.Unix(0, 0).UTC(),
			ProposerID:   "genesis",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    stateRoot,
		},
		Tx:        nil,
		Finalized: true,
	}
	if pinnedHash, ok := genesis.PinnedGenesisHash(); ok {
		gb.Hash = pinnedHash
	} else if _, err := gb.ComputeHash(); err != nil {
		return nil, err
	}
	c.Blocks = append(c.Blocks, gb)
	// Retain genesis state so a later TryReorg can safely replay an
	// alternative branch from any fork point without depending on the caller
	// (see SeedGenesisState for chains restored from a storage snapshot).
	c.genesisState = st.Clone()
	return c, nil
}

// SetHotWindow bounds how much finalized block history Chain keeps in
// memory, trimming the oldest entries out of Blocks once it grows past
// maxHotBlocks and falling back to coldReader (may be nil) to serve an
// older height on demand -- see blockAtLocked/BlocksFrom. maxHotBlocks <= 0
// disables trimming entirely (the default), preserving the original
// unbounded-in-memory behavior. Safe to call at any time, including right
// after NewChain; trims immediately if Blocks already exceeds the new
// bound. For a chain being restored from a storage snapshot that already
// had history trimmed out when it was saved, use RestoreHotWindow instead
// so the hot-window checkpoint carries over correctly.
func (c *Chain) SetHotWindow(maxHotBlocks int, coldReader ColdBlockReader) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxHotBlocks = maxHotBlocks
	c.coldBlocks = coldReader
	c.trimHotWindowLocked()
}

// RestoreHotWindow reconstructs a chain's hot-window bookkeeping after
// loading a split-format snapshot (see storage.Store) that already had
// hotWindowStart > 0 when it was last saved -- i.e. some history had
// already been trimmed out of memory by the process that saved it.
// baseState/baseBlock must be the state and block exactly as of height
// hotWindowStart-1 (storage.Store's Load records both alongside the
// snapshot); either may be nil (a legacy, never-trimmed snapshot has
// neither), in which case the existing genesis-derived defaults are left
// in place -- so calling this with hotWindowStart==0 and nil/nil is always
// a safe no-op for the checkpoint fields. Call once, right after
// constructing Chain from a snapshot and after SeedGenesisState, before the
// chain starts accepting new blocks.
func (c *Chain) RestoreHotWindow(hotWindowStart uint64, baseState *State, baseBlock *Block, maxHotBlocks int, coldReader ColdBlockReader) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hotWindowStart = hotWindowStart
	if baseState != nil {
		c.hotWindowBaseState = baseState.Clone()
	}
	if baseBlock != nil {
		c.hotWindowBaseBlock = baseBlock
	}
	c.maxHotBlocks = maxHotBlocks
	c.coldBlocks = coldReader
	// A restored snapshot -- especially a freshly migrated legacy one,
	// which loads with Blocks holding its entire historical run -- may
	// already exceed the newly configured bound; trim it down immediately
	// rather than waiting for the next block, exactly like SetHotWindow.
	c.trimHotWindowLocked()
}

// HotWindowInfo describes how much of a chain's finalized history
// currently lives in memory, for a Store to persist alongside the
// blocks/state SnapshotData already returns -- see RestoreHotWindow for
// reloading it. BaseState/BaseBlock are exactly as of height Start-1: for
// a chain that has never trimmed anything (Start == 0) that's genesis, so
// callers don't need to special-case the untrimmed case.
type HotWindowInfo struct {
	Start     uint64
	BaseState *State
	BaseBlock *Block
}

func (c *Chain) HotWindowInfo() HotWindowInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	info := HotWindowInfo{Start: c.hotWindowStart, BaseBlock: c.hotWindowBaseBlockLocked()}
	if bs := c.hotWindowBaseStateLocked(); bs != nil {
		info.BaseState = bs.Clone()
	}
	return info
}

// hotWindowBaseStateLocked returns the state exactly as of height
// hotWindowStart-1. When nothing has been trimmed yet (hotWindowStart ==
// 0) that's simply genesisState, so no separate bookkeeping is needed for
// the common, untrimmed case -- hotWindowBaseState is only ever populated
// once trimHotWindowLocked has actually evicted something. Must be called
// with c.mu already held.
func (c *Chain) hotWindowBaseStateLocked() *State {
	if c.hotWindowStart == 0 {
		return c.genesisState
	}
	return c.hotWindowBaseState
}

// hotWindowBaseBlockLocked returns the anchor block for hot-window replay:
// the block immediately preceding hotWindowStart. When nothing has been
// trimmed yet, that's simply Blocks[0] (genesis for a normal chain) --
// once trimming has advanced hotWindowStart past 0, the true anchor block
// is no longer in Blocks at all (it was evicted), so trimHotWindowLocked
// caches it explicitly beforehand. Must be called with c.mu already held.
func (c *Chain) hotWindowBaseBlockLocked() *Block {
	if c.hotWindowStart == 0 {
		if len(c.Blocks) == 0 {
			return nil
		}
		return c.Blocks[0]
	}
	return c.hotWindowBaseBlock
}

// trimHotWindowLocked evicts the oldest in-memory blocks once len(Blocks)
// exceeds maxHotBlocks, advancing hotWindowStart (and the hot-window
// checkpoint) to match. No-ops if maxHotBlocks <= 0, if there's nothing to
// trim yet, or if the checkpoint can't be safely advanced -- trimming is
// strictly best-effort and must never be allowed to risk state
// correctness or fail block production; worst case it just leaves a few
// more blocks in memory than configured until it can safely catch up.
// Must be called with c.mu already held for writing.
func (c *Chain) trimHotWindowLocked() {
	if c.maxHotBlocks <= 0 {
		return
	}
	excess := len(c.Blocks) - c.maxHotBlocks
	if excess <= 0 {
		return
	}
	baseState := c.hotWindowBaseStateLocked()
	if baseState == nil {
		return
	}
	next := baseState.Clone()
	for i := 0; i < excess; i++ {
		var err error
		next, err = c.advanceCheckpointStateLocked(next, c.Blocks[i])
		if err != nil {
			return
		}
	}
	c.hotWindowBaseState = next
	c.hotWindowBaseBlock = c.Blocks[excess-1]
	// Copy into a fresh backing array rather than just re-slicing, so the
	// evicted blocks' *Block pointers (and everything they retain -- Tx
	// slices, vote maps) actually become unreachable and collectible; a
	// bare c.Blocks[excess:] would keep the whole old backing array (and
	// every block pointer in it) alive for as long as the new slice header
	// still points into any part of it, defeating the entire point of
	// trimming.
	c.Blocks = append([]*Block(nil), c.Blocks[excess:]...)
	c.hotWindowStart += uint64(excess)
}

// advanceCheckpointStateLocked returns a clone of st with b's transactions
// and block-economics reward applied -- the same state transition
// FinalizeBlock performs, minus its state-root verification, since b was
// already fully validated once (by FinalizeBlock or TryReorg) before it
// ever reached c.Blocks. Used only to advance the hot-window checkpoint
// past a block being evicted from memory; never touches c.State itself.
// Deliberately duplicated rather than factored into FinalizeBlock's own
// path, so this new, best-effort bookkeeping can never change the
// behavior of the one function every validator's real finality depends on.
func (c *Chain) advanceCheckpointStateLocked(st *State, b *Block) (*State, error) {
	next := st.Clone()
	for _, tx := range b.Tx {
		if err := next.ApplyTx(tx); err != nil {
			return nil, fmt.Errorf("replay transaction %s: %w", tx.ID, err)
		}
	}
	if err := applyBlockEconomics(next, b.Tx, c.resolveProposerAddressLocked(b.Header.ProposerID)); err != nil {
		return nil, err
	}
	return next, nil
}

func (c *Chain) Height() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.heightLocked()
}

func (c *Chain) heightLocked() uint64 {
	if len(c.Blocks) == 0 {
		return 0
	}
	return c.Blocks[len(c.Blocks)-1].Header.Height
}

func (c *Chain) Tip() *Block {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tipLocked()
}

func (c *Chain) tipLocked() *Block {
	if len(c.Blocks) == 0 {
		return nil
	}
	return c.Blocks[len(c.Blocks)-1]
}

// BlocksFrom returns all blocks from height from onward -- the mechanism
// peer catch-up (CatchUpOnce/StartPeerSync) and the /blocks HTTP endpoint
// both rely on to let a fresh or resyncing node rebuild its entire history
// from genesis, so it must keep working for any from, including heights
// this chain has long since trimmed out of memory: those are read back on
// demand from coldBlocks (see SetHotWindow), one height at a time, without
// ever holding the chain lock while doing disk I/O.
func (c *Chain) BlocksFrom(from int) []*Block {
	if from < 0 {
		from = 0
	}
	fromHeight := uint64(from)

	c.mu.RLock()
	if len(c.Blocks) == 0 {
		c.mu.RUnlock()
		return nil
	}
	tipHeight := c.Blocks[len(c.Blocks)-1].Header.Height
	hotStart := c.hotWindowStart
	reader := c.coldBlocks
	if fromHeight > tipHeight {
		c.mu.RUnlock()
		return nil
	}
	var hot []*Block
	if fromHeight >= hotStart {
		idx := fromHeight - hotStart
		if idx < uint64(len(c.Blocks)) {
			hot = make([]*Block, len(c.Blocks)-int(idx))
			copy(hot, c.Blocks[idx:])
		}
	} else {
		hot = make([]*Block, len(c.Blocks))
		copy(hot, c.Blocks)
	}
	c.mu.RUnlock()

	if fromHeight >= hotStart || reader == nil {
		return hot
	}
	out := make([]*Block, 0, int(hotStart-fromHeight)+len(hot))
	for h := fromHeight; h < hotStart; h++ {
		blk, ok := reader.ColdBlockAt(h)
		if !ok {
			// A gap in the archive -- stop rather than hand a resyncing
			// peer a discontiguous range it could misapply.
			return out
		}
		out = append(out, blk)
	}
	out = append(out, hot...)
	return out
}

func (c *Chain) SubmitTx(tx Tx) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tx.ID == "" {
		return errors.New("missing transaction ID")
	}
	if err := tx.Verify(); err != nil {
		return err
	}
	if tx.ChainID != c.transactionChainIDLocked() {
		return fmt.Errorf("wrong transaction chain ID: got %d, want %d", tx.ChainID, c.transactionChainIDLocked())
	}
	if _, exists := c.Mempool[tx.ID]; exists {
		return fmt.Errorf("transaction %s already in mempool", tx.ID)
	}
	expectedNonce := c.State.GetNextNonce(tx.From)
	if tx.Nonce != expectedNonce {
		return fmt.Errorf("nonce mismatch: got %d, expected %d for address %s", tx.Nonce, expectedNonce, tx.From)
	}
	c.Mempool[tx.ID] = tx
	return nil
}

// SimulationResult reports what would happen if a transaction were applied,
// without it ever touching live state or the mempool.
type SimulationResult struct {
	TxID               string `json:"tx_id"`
	Applied            bool   `json:"applied"`
	Error              string `json:"error,omitempty"`
	FromBalanceBefore  uint64 `json:"from_balance_before"`
	FromBalanceAfter   uint64 `json:"from_balance_after"`
	ToBalanceBefore    uint64 `json:"to_balance_before"`
	ToBalanceAfter     uint64 `json:"to_balance_after"`
	ResultingStateRoot string `json:"resulting_state_root,omitempty"`
}

// SimulateTx dry-runs a single transaction against a fresh clone of the
// live state. It runs the exact same checks and the exact same state
// transition function real transactions go through (tx.Verify, chain ID,
// nonce, State.ApplyTx) -- not a separate, simplified reimplementation that
// could silently drift and report an outcome the real chain wouldn't
// actually produce. c.State and c.Mempool are never written to: the clone
// is discarded once the result is read off it.
func (c *Chain) SimulateTx(tx Tx) (SimulationResult, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := SimulationResult{TxID: tx.ID}

	if err := tx.Verify(); err != nil {
		result.Error = err.Error()
		return result, err
	}
	if tx.ChainID != c.transactionChainIDLocked() {
		err := fmt.Errorf("wrong transaction chain ID: got %d, want %d", tx.ChainID, c.transactionChainIDLocked())
		result.Error = err.Error()
		return result, err
	}
	expectedNonce := c.State.GetNextNonce(tx.From)
	if tx.Nonce != expectedNonce {
		err := fmt.Errorf("nonce mismatch: got %d, expected %d for address %s", tx.Nonce, expectedNonce, tx.From)
		result.Error = err.Error()
		return result, err
	}

	tmp := c.State.Clone()
	fromBefore := tmp.Get(tx.From)
	toBefore := tmp.Get(tx.To)

	if err := tmp.ApplyTx(tx); err != nil {
		result.Error = err.Error()
		return result, err
	}

	fromAfter := tmp.Get(tx.From)
	toAfter := tmp.Get(tx.To)

	result.Applied = true
	result.FromBalanceBefore = fromBefore.Balance
	result.FromBalanceAfter = fromAfter.Balance
	result.ToBalanceBefore = toBefore.Balance
	result.ToBalanceAfter = toAfter.Balance
	result.ResultingStateRoot = tmp.Root()
	return result, nil
}

// BuildBlock creates a candidate block from mempool against the current state.
func (c *Chain) BuildBlock(proposerID string, proposerPoCRoot string, maxTx int) (*Block, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.Blocks) == 0 {
		return nil, ErrNoGenesis
	}
	if maxTx <= 0 {
		maxTx = 1000
	}

	tmp := c.State.Clone()
	txs := make([]Tx, 0, maxTx)
	candidates := make([]Tx, 0, len(c.Mempool))
	for _, tx := range c.Mempool {
		candidates = append(candidates, tx)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Fee != candidates[j].Fee {
			return candidates[i].Fee > candidates[j].Fee
		}
		if candidates[i].From != candidates[j].From {
			return candidates[i].From < candidates[j].From
		}
		if candidates[i].Nonce != candidates[j].Nonce {
			return candidates[i].Nonce < candidates[j].Nonce
		}
		return candidates[i].ID < candidates[j].ID
	})
	for _, tx := range candidates {
		if len(txs) >= maxTx {
			break
		}
		if err := tmp.ApplyTx(tx); err != nil {
			continue
		}
		txs = append(txs, tx)
	}
	if err := applyBlockEconomics(tmp, txs, c.resolveProposerAddressLocked(proposerID)); err != nil {
		return nil, err
	}
	txRoot, err := TxMerkleRoot(txs)
	if err != nil {
		return nil, err
	}

	parent := c.tipLocked()
	b := &Block{
		Header: BlockHeader{
			Height:          parent.Header.Height + 1,
			ParentHash:      parent.Hash,
			Timestamp:       time.Time{},
			ProposerID:      proposerID,
			TxMerkleRoot:    txRoot,
			StateRoot:       tmp.Root(),
			ProposerPoCRoot: proposerPoCRoot,
		},
		Tx:             txs,
		ValidatorVotes: make(map[string]int),
		Finalized:      false,
	}
	_, err = b.ComputeHash()
	return b, err
}

// ValidateBlock reports whether b is a fully valid, ready-to-commit block:
// correct hashes/state transition, and, once a validator set is configured,
// both a genuine proposer signature AND real quorum-threshold validator
// approvals. Use this to check a block that already carries its full set of
// collected approvals -- e.g. immediately before FinalizeBlock, or when
// deciding whether to accept a peer's already-finalized block during
// catch-up. For a bare candidate a proposer has just built (which by
// definition has zero approvals yet -- nobody has voted on it), use
// ValidateProposal instead.
func (c *Chain) ValidateBlock(b *Block) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.validateBlockLocked(b, true)
}

// ValidateProposal checks a freshly-built candidate block -- everything
// ValidateBlock checks except the quorum-of-approvals count, which cannot
// possibly be satisfied yet on a block nobody has voted on. The proposer's
// own signature IS still verified (when a validator set is configured),
// since that's exactly what a validator needs to confirm before it's
// willing to vote on the candidate at all -- see node.Node.HandleProposal,
// the real multi-party consensus round's use of this method.
func (c *Chain) ValidateProposal(b *Block) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.validateBlockLocked(b, false)
}

func (c *Chain) validateBlockLocked(b *Block, requireQuorum bool) error {
	if b == nil || b.Hash == "" {
		return ErrBadBlock
	}
	tip := c.tipLocked()
	if tip == nil {
		return ErrNoGenesis
	}
	if b.Header.ParentHash != tip.Hash || b.Header.Height != tip.Header.Height+1 {
		return ErrBadBlock
	}
	if b.Header.Height > 0 && !b.Header.Timestamp.IsZero() {
		return ErrBadBlock
	}
	if b.Header.Height > 0 && len(c.validatorKeys) > 0 && b.Header.Height >= c.authEnforceFromHeight {
		if err := c.verifyBlockAuthorizationLocked(b, requireQuorum); err != nil {
			return err
		}
	}

	expectedHash, err := b.CalculateHash()
	if err != nil || expectedHash != b.Hash {
		return ErrBadBlock
	}
	expectedTxRoot, err := TxMerkleRoot(b.Tx)
	if err != nil || expectedTxRoot != b.Header.TxMerkleRoot {
		return ErrBadBlock
	}
	enforceStateRoot := b.Header.Height >= c.stateRootEnforceFromHeight

	for _, tx := range b.Tx {
		if tx.ChainID != c.transactionChainIDLocked() {
			return fmt.Errorf("wrong transaction chain ID in block: got %d, want %d", tx.ChainID, c.transactionChainIDLocked())
		}
		if err := tx.Verify(); err != nil {
			return fmt.Errorf("invalid transaction signature in block: %w", err)
		}
	}

	tmp := c.State.Clone()
	for _, tx := range b.Tx {
		if err := tmp.ApplyTx(tx); err != nil {
			return err
		}
	}
	if err := applyBlockEconomics(tmp, b.Tx, c.resolveProposerAddressLocked(b.Header.ProposerID)); err != nil {
		return err
	}
	c.applyIrregularStateCorrectionsLocked(tmp, b.Header.Height)
	if enforceStateRoot && tmp.Root() != b.Header.StateRoot {
		return ErrStateRootMismatch
	}
	return nil
}

// FinalizeBlock commits exactly the state transition already validated.
// Unlike ValidateProposal, this always requires real quorum-threshold
// approvals to already be present on b -- this is the one place that
// actually matters, since it's the point at which the block becomes
// permanent chain history.
func (c *Chain) FinalizeBlock(b *Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateBlockLocked(b, true); err != nil {
		return err
	}

	nextState := c.State.Clone()
	for _, tx := range b.Tx {
		if err := nextState.ApplyTx(tx); err != nil {
			return fmt.Errorf("apply finalized transaction %s: %w", tx.ID, err)
		}
	}
	if err := applyBlockEconomics(nextState, b.Tx, c.resolveProposerAddressLocked(b.Header.ProposerID)); err != nil {
		return err
	}
	c.applyIrregularStateCorrectionsLocked(nextState, b.Header.Height)
	if b.Header.Height >= c.stateRootEnforceFromHeight && nextState.Root() != b.Header.StateRoot {
		return ErrBadBlock
	}

	c.State = nextState
	for _, tx := range b.Tx {
		delete(c.Mempool, tx.ID)
	}
	b.Finalized = true
	c.Blocks = append(c.Blocks, b)
	c.trimHotWindowLocked()
	return nil
}

// resolveProposerAddressLocked turns a proposer's node/agent ID (e.g.
// "synthos-validator-12") into the real account address that should
// receive its block-proposal fee reward, by looking up the proposer's
// registered public key (see SetValidatorSet) and deriving the address the
// same way every other address in this chain is derived
// (AddressFromPublicKey). Previously applyBlockEconomics built the address
// as Address("0x"+proposerID) directly -- for a proposerID like
// "synthos-validator-12" that's not valid hex and not 20 bytes, so every
// proposer reward was credited to an address nobody could ever control or
// spend from; effectively burned, but silently and by accident rather than
// by the deliberate BURN_PERCENT split. Returns "" when the proposer isn't
// a registered validator with a known key -- callers must treat that as "no
// address to credit" (the fee is still fully burned, just not credited to a
// guessed/fabricated address) rather than fall back to a fabricated one.
// Caller must already hold c.mu (read or write).
func (c *Chain) resolveProposerAddressLocked(proposerID string) Address {
	pub, ok := c.validatorKeys[proposerID]
	if !ok || len(pub) != ed25519.PublicKeySize {
		return ""
	}
	return AddressFromPublicKey(pub)
}

func applyBlockEconomics(st *State, txs []Tx, proposerAddr Address) error {
	var totalFees uint64
	for _, tx := range txs {
		var err error
		totalFees, err = safeAdd(totalFees, tx.Fee)
		if err != nil {
			return fmt.Errorf("block fee overflow: %w", err)
		}
	}
	if totalFees == 0 || proposerAddr == "" {
		return nil
	}

	burnAmount := (totalFees * BURN_PERCENT) / 100
	rewardAmount := totalFees - burnAmount
	proposer := st.Get(proposerAddr)
	nextBalance, err := safeAdd(proposer.Balance, rewardAmount)
	if err != nil {
		return fmt.Errorf("proposer reward overflow: %w", err)
	}
	proposer.Balance = nextBalance
	st.Set(proposerAddr, proposer)
	return nil
}

func (c *Chain) transactionChainIDLocked() uint64 {
	if c.TxChainID == 0 {
		return 1
	}
	return c.TxChainID
}

func (c *Chain) TransactionChainID() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.transactionChainIDLocked()
}

func (c *Chain) MempoolSnapshot() map[string]Tx {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]Tx, len(c.Mempool))
	for id, tx := range c.Mempool {
		out[id] = tx
	}
	return out
}

func (c *Chain) SnapshotData() (string, uint64, []*Block, *State) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	blocks := make([]*Block, len(c.Blocks))
	for i, block := range c.Blocks {
		if block == nil {
			continue
		}
		copyBlock := *block
		// append([]Tx(nil), block.Tx...) would silently collapse a non-nil
		// empty Tx slice back to nil (append with zero elements to append
		// returns the destination unchanged), changing the block's canonical
		// JSON encoding ("tx":[] vs "tx":null) across every save/reload
		// cycle. Use make+copy instead so nil stays nil and non-nil (even
		// empty) stays non-nil.
		if block.Tx != nil {
			copyBlock.Tx = make([]Tx, len(block.Tx))
			copy(copyBlock.Tx, block.Tx)
		}
		if block.ValidatorVotes != nil {
			copyBlock.ValidatorVotes = make(map[string]int, len(block.ValidatorVotes))
			for validator, vote := range block.ValidatorVotes {
				copyBlock.ValidatorVotes[validator] = vote
			}
		}
		blocks[i] = &copyBlock
	}
	return c.ChainID, c.transactionChainIDLocked(), blocks, c.State.Clone()
}

// SetValidatorSet configures the registered validators' public keys and the
// minimum number of distinct validator approvals a block must carry to be
// accepted. Call this once, at startup, with the same validator roster
// passed to consensus.Engine.SetValidators / node.Node.SetValidators, so all
// three layers agree on who is allowed to propose and finalize blocks.
//
// Until this is called, Chain performs no proposer/quorum signature checks
// at all (matching its previous behavior).
func (c *Chain) SetValidatorSet(keys map[string]ed25519.PublicKey, requiredQuorum int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make(map[string]ed25519.PublicKey, len(keys))
	for id, pub := range keys {
		cp[id] = pub
	}
	c.validatorKeys = cp
	if requiredQuorum < 1 {
		requiredQuorum = 1
	}
	c.requiredQuorum = requiredQuorum
}

// SetAuthEnforceFromHeight grandfathers in any existing chain history below
// height by exempting it from the ProposerSignature/QuorumSignatures check
// that SetValidatorSet's key set would otherwise apply to every non-genesis
// block. Call it once, alongside SetValidatorSet, with the height of the
// first block that was actually produced under real block-signing (i.e. the
// height at which this feature was deployed chain-wide) -- never with an
// arbitrary or per-node value, since every node must agree on exactly which
// blocks are exempt or they will disagree about which chain is valid. Blocks
// at or above height are held to full verification exactly as before; this
// only relaxes the check for the genuinely pre-existing, never-signed
// prefix. Not calling this at all preserves the original behavior (enforce
// from height 1 onward).
func (c *Chain) SetAuthEnforceFromHeight(height uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authEnforceFromHeight = height
}

// SetStateRootEnforceFromHeight grandfathers in existing chain history below
// height by skipping the replayed-state-root-must-match-Header.StateRoot
// check for it (see stateRootEnforceFromHeight's doc comment). Must be the
// same value on every node sharing this chain, chosen at or above the
// height live when this fix is deployed -- never per-node, or nodes will
// disagree about which history is valid.
func (c *Chain) SetStateRootEnforceFromHeight(height uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stateRootEnforceFromHeight = height
}

// StateCorrection is a single, one-time, explicit adjustment to a specific
// account's balance at a specific block height, applied deterministically
// during that block's validation and finalization (see
// applyIrregularStateCorrectionsLocked), before the resulting state's
// Root() is compared against the block's declared StateRoot or committed
// as the chain's live State.
//
// This exists for exactly one situation: a REAL, already-happened,
// already-quorum-agreed state change that was never recorded as an
// ordinary transaction, so no amount of replaying this chain's real
// transaction history can ever reproduce it on its own -- see
// SetIrregularStateCorrections's doc comment for the specific incident
// this shipped for. It is deliberately narrow, not a general-purpose
// "edit the ledger" escape hatch: every entry here must correspond to
// something that genuinely, verifiably already happened and was already
// agreed to by quorum on the live chain (the same way a legitimate
// hard-fork state patch documents a specific, already-occurred
// irregularity), never a means to alter history that didn't actually
// happen this way.
type StateCorrection struct {
	// Height is the block height this correction is applied at, as part
	// of validating/finalizing exactly that block -- matching the height
	// at which the real, already-agreed state change actually took
	// effect on the live chain.
	Height uint64 `json:"height"`
	// Address is the account whose balance is adjusted.
	Address Address `json:"address"`
	// Debit is subtracted from Address's balance, clamped at zero (never
	// negative, never underflows) -- the same safe-clamp behavior
	// consensus.SlashingTracker's real penalty execution already uses
	// (see internal/node.NewNode's ExecuteSlash wiring), since the
	// correction this shipped for exists specifically to reproduce that
	// mechanism's already-executed effect.
	Debit uint64 `json:"debit"`
}

// SetIrregularStateCorrections configures one-time, explicit, height-gated
// state adjustments applied during block validation/finalization (see
// StateCorrection's doc comment for the safety contract). Must be set
// identically on every node sharing this chain, exactly like
// SetAuthEnforceFromHeight/SetStateRootEnforceFromHeight -- a node missing
// an entry here will permanently disagree with its peers about this
// chain's state from that block onward.
//
// Shipped for one specific, real incident: internal/node.NewNode's
// SlashingTracker.SetExecuteSlash callback mutates chain.State directly,
// synchronously, outside the deterministic block-apply path every other
// validator also runs (see chain.ErrStateRootMismatch's doc comment and
// the "Live incident" note on validateBlockLocked's state-root-mismatch
// handling, a few lines above this call site). Before the downtime
// instance of that bug class was fixed (6062e21), synthos-rpc (agent ID
// synthos-render-validator-1) was genuinely falling behind in real time
// (the same OOM-driven catch-up problem patch #1 fixed), and each of
// validator-12 and validator-13 independently observed it missing its
// slot, crossed RecordMissedBlock's threshold, and executed a real local
// slash against synthos-rpc's OWN account -- self-inflicted in the sense
// that the *target* was the lagging node, not the two nodes doing the
// recording. Both real-slashed the same address by the tracker's
// DowntimePenalty (50 at the time), clamped to zero since that account's
// balance was already zero, and created that account's entry in
// State.Accounts for the first time in the process -- a real,
// net-zero-supply, already-quorum-agreed change (both validator-12 and
// validator-13 finalized every subsequent block on top of it) that no
// amount of replaying this chain's real transaction history can ever
// reproduce, since it was never a transaction.
//
// The first attempt at this fix (patch #5) misidentified the target as
// synthos-validator-12's own address -- a plausible-looking but wrong
// guess (validator-12 is this chain's sole block producer, so it was the
// natural first suspect for "the account that got slashed"), reasoned
// from real forensic evidence (account_count off by exactly 1 between a
// clean resync and the live chain, circulating supply identical, zero
// real transactions anywhere in the surrounding range) that was all
// true but didn't by itself pin down WHICH address. It deployed cleanly
// and even produced a bit-identical Account{Balance:0, Nonce:0} entry
// (a clamp-to-zero debit from an already-zero balance is indistinguishable
// from ANY penalty amount, which is also why the exact debit here can't
// be confirmed as 50 vs. 250 vs. 1000 from hash-matching alone -- 50 is
// used because downtime is the only one of the three slashing paths that
// can ever target a pure follower like synthos-rpc, which never proposes
// a block and so can never be the target of an invalid-block or
// equivocation slash) -- but it still produced the wrong overall state
// root, because it was the wrong account. The real target was confirmed
// by brute-forcing every known validator/treasury address (plus
// transfers between them) against the block's actual declared root and
// finding the one exact match: see the state-root-mismatch diagnostic
// commits this shipped alongside, which were reverted once this
// confirmed fix replaced them. A node doing a genuine from-genesis
// resync (rather than loading an existing snapshot that already has this
// baked in) needs this to converge at all -- see
// TestChain_FinalizeBlock_NeedsIrregularCorrectionToReplayRealSelfSlash.
func (c *Chain) SetIrregularStateCorrections(corrections []StateCorrection) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stateCorrections = append([]StateCorrection(nil), corrections...)
}

// applyIrregularStateCorrectionsLocked applies every configured
// StateCorrection whose Height matches height to st, using the same safe
// clamp-to-zero debit consensus.SlashingTracker's real penalty execution
// uses. Caller must already hold c.mu (read or write) -- reads
// c.stateCorrections only, never mutates Chain itself.
func (c *Chain) applyIrregularStateCorrectionsLocked(st *State, height uint64) {
	for _, corr := range c.stateCorrections {
		if corr.Height != height {
			continue
		}
		acc := st.Get(corr.Address)
		if acc.Balance > corr.Debit {
			acc.Balance -= corr.Debit
		} else {
			acc.Balance = 0
		}
		st.Set(corr.Address, acc)
	}
}

// SeedGenesisState records the chain's state at height 0, needed to safely
// replay an alternative branch during a fork-choice reorg (see TryReorg). A
// chain built via NewChain sets this automatically; a chain restored from a
// storage snapshot (which starts directly from its saved State, not from
// genesis) must call this explicitly with a freshly-computed genesis state
// before TryReorg can be used. Until it is called, TryReorg safely refuses
// to reorg rather than risk building on unknown state.
func (c *Chain) SeedGenesisState(genesisState *State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if genesisState != nil {
		c.genesisState = genesisState.Clone()
	}
}

// BlockAt returns the finalized block at the given height, or nil if the
// chain hasn't reached that height or the block simply isn't available.
// Blocks are stored contiguously by height starting at hotWindowStart
// (index == height - hotWindowStart) while trimming is disabled (the
// default) hotWindowStart is always 0, so this is a direct lookup exactly
// as before. Once history has been trimmed out of memory, a height below
// hotWindowStart falls through to coldBlocks (see SetHotWindow) -- read
// without holding the chain lock, so a peer catching up on old history
// never blocks live block production.
func (c *Chain) BlockAt(height uint64) *Block {
	c.mu.RLock()
	blk, maybeCold := c.blockAtLocked(height)
	reader := c.coldBlocks
	c.mu.RUnlock()
	if blk != nil || !maybeCold || reader == nil {
		return blk
	}
	if cb, ok := reader.ColdBlockAt(height); ok {
		return cb
	}
	return nil
}

// blockAtLocked returns the in-memory block at height if it's within the
// current hot window (maybeCold=false either way), or (nil, true) if
// height is a plausible-but-trimmed height the caller should retry via
// coldBlocks, or (nil, false) if height is simply out of range (beyond the
// current tip, or the chain isn't initialized yet) and no cold lookup
// could possibly help. Must be called with c.mu already held.
func (c *Chain) blockAtLocked(height uint64) (blk *Block, maybeCold bool) {
	if len(c.Blocks) == 0 {
		return nil, false
	}
	tipHeight := c.Blocks[len(c.Blocks)-1].Header.Height
	if height > tipHeight {
		return nil, false
	}
	if height < c.hotWindowStart {
		return nil, true
	}
	idx := height - c.hotWindowStart
	if idx >= uint64(len(c.Blocks)) {
		return nil, true
	}
	return c.Blocks[idx], false
}

// verifyBlockAuthorizationLocked checks that b was actually produced by a
// real, registered validator: a valid proposer signature from
// Header.ProposerID's registered key. When requireQuorum is true, it
// additionally requires valid approval signatures from at least
// requiredQuorum distinct registered validators -- callers finalizing a
// block (or checking one that should already be fully approved) always
// pass true; callers validating a bare candidate that hasn't been voted on
// yet (see ValidateProposal) pass false, since requiring approvals a
// candidate cannot possibly have yet would make it impossible for any
// validator to ever validate-then-vote on a fresh proposal. Must be called
// with c.mu already held.
func (c *Chain) verifyBlockAuthorizationLocked(b *Block, requireQuorum bool) error {
	proposerKey, ok := c.validatorKeys[b.Header.ProposerID]
	if !ok || len(proposerKey) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: proposer %q is not a registered validator", ErrBadBlock, b.Header.ProposerID)
	}
	propSig, err := decodeHexSig(b.ProposerSignature)
	if err != nil || !ed25519.Verify(proposerKey, []byte(b.Hash), propSig) {
		return fmt.Errorf("%w: invalid or missing proposer signature", ErrBadBlock)
	}
	if !requireQuorum {
		return nil
	}

	approvalMsg := b.QuorumApprovalMessage()
	seen := make(map[string]struct{}, len(b.QuorumSignatures))
	for validatorID, sigHex := range b.QuorumSignatures {
		key, ok := c.validatorKeys[validatorID]
		if !ok || len(key) != ed25519.PublicKeySize {
			continue // ignore signatures from unregistered or malformed keys
		}
		sig, err := decodeHexSig(sigHex)
		if err != nil || !ed25519.Verify(key, approvalMsg, sig) {
			continue // ignore invalid signatures rather than failing the whole block on one bad entry
		}
		seen[validatorID] = struct{}{}
	}
	if len(seen) < c.requiredQuorum {
		return fmt.Errorf("%w: only %d of %d required validator approvals verified", ErrBadBlock, len(seen), c.requiredQuorum)
	}
	return nil
}

func decodeHexSig(s string) ([]byte, error) {
	if len(s) >= 2 && s[:2] == "0x" {
		s = s[2:]
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(b) != ed25519.SignatureSize {
		return nil, fmt.Errorf("signature must be %d bytes, got %d", ed25519.SignatureSize, len(b))
	}
	return b, nil
}

// TryReorg considers switching the canonical chain to an alternative branch
// a peer has offered, starting at forkHeight (the first contested height;
// must be > 0 and no greater than the chain's current height -- genesis is
// never contested) and running through candidate. It only ever switches when
// the alternative branch is strictly heavier by the chain's fork-choice rule
// -- more distinct, cryptographically verified validator approvals over the
// contested range, tie-broken by length -- AND every block in it
// independently re-validates by replaying it through the exact same checks
// FinalizeBlock always runs (state transition, hashes, and, once a
// validator set is configured, proposer/quorum signatures). An adversarial
// or merely out-of-date peer offering a fabricated, under-signed, or invalid
// branch can never trigger a reorg this way; only a genuinely more-approved,
// fully valid branch can. Returns whether a reorg happened.
func (c *Chain) TryReorg(forkHeight uint64, candidate []*Block) (bool, error) {
	c.mu.Lock()
	if c.genesisState == nil {
		c.mu.Unlock()
		return false, errors.New("reorg unavailable: genesis state not seeded (call SeedGenesisState)")
	}
	if forkHeight == 0 || forkHeight > c.heightLocked() {
		c.mu.Unlock()
		return false, fmt.Errorf("%w: invalid fork height %d", ErrBadBlock, forkHeight)
	}
	if forkHeight < c.hotWindowStart {
		hotWindowStart := c.hotWindowStart
		c.mu.Unlock()
		// Everything below the hot window has already been superseded by
		// at least maxHotBlocks-worth of later, quorum-finalized history --
		// under real BFT finality a genuinely honest competing branch this
		// deep essentially can't exist, and TryReorg's only production
		// caller (applyPeerBlock) only ever contests a height at or very
		// near the current tip. Refusing here trades an
		// unreachable-in-practice capability for never having to hold full
		// chain history in memory just in case -- the same fail-safe
		// posture as the "genesis state not seeded" case above.
		return false, fmt.Errorf("reorg unavailable: fork height %d predates the retained hot window (starts at %d)", forkHeight, hotWindowStart)
	}
	if len(candidate) == 0 {
		c.mu.Unlock()
		return false, errors.New("empty candidate branch")
	}
	if candidate[0] == nil || candidate[0].Header.Height != forkHeight {
		c.mu.Unlock()
		return false, fmt.Errorf("%w: candidate branch must start at height %d", ErrBadBlock, forkHeight)
	}

	hotStart := c.hotWindowStart
	// c.Blocks[i] always has height hotStart+i. The replay anchor
	// (baseBlock, resolved below) is conceptually the block at height
	// hotStart-1 -- except when hotStart == 0, where there is no height
	// -1: genesis itself (c.Blocks[0]) serves as the anchor instead, since
	// it's never replayed through FinalizeBlock (see NewChain), so it must
	// be excluded from prefix (the blocks that DO need replaying) even
	// though it's the first entry in c.Blocks.
	prefixStartIdx := uint64(0)
	if hotStart == 0 {
		prefixStartIdx = 1
	}
	prefixEndIdx := forkHeight - hotStart
	prefix := make([]*Block, prefixEndIdx-prefixStartIdx)
	copy(prefix, c.Blocks[prefixStartIdx:prefixEndIdx])
	currentTail := make([]*Block, uint64(len(c.Blocks))-prefixEndIdx)
	copy(currentTail, c.Blocks[prefixEndIdx:])
	validatorKeys := c.validatorKeys
	requiredQuorum := c.requiredQuorum
	// baseState/baseBlock anchor the replay at the hot window's own
	// boundary instead of always genesis. For a chain that has never
	// trimmed anything (hotStart == 0) these are exactly
	// genesisState/Blocks[0], so behavior is identical to before this
	// existed; see hotWindowBaseStateLocked/hotWindowBaseBlockLocked.
	baseState := c.hotWindowBaseStateLocked()
	baseBlock := c.hotWindowBaseBlockLocked()
	if baseState == nil || baseBlock == nil {
		c.mu.Unlock()
		return false, errors.New("reorg unavailable: hot-window checkpoint not established")
	}
	baseState = baseState.Clone()
	chainID, txChainID := c.ChainID, c.transactionChainIDLocked()
	startLen := len(c.Blocks)
	c.mu.Unlock() // release the real chain's lock while we do the (possibly slow) replay work off to the side

	// Fork choice: only switch to a branch that is strictly better than what
	// we already have over the contested range. An attacker offering a
	// longer but under-signed branch cannot out-weigh a shorter, fully
	// quorum-certified one.
	scoreOf := func(blocks []*Block) int {
		total := 0
		for _, blk := range blocks {
			if blk != nil {
				total += blk.verifiedApprovalCount(validatorKeys)
			}
		}
		return total
	}
	candidateScore, currentScore := scoreOf(candidate), scoreOf(currentTail)
	if candidateScore < currentScore || (candidateScore == currentScore && len(candidate) <= len(currentTail)) {
		return false, nil
	}

	// Replay the agreed prefix plus the candidate branch through a disposable
	// scratch chain, reusing FinalizeBlock's own validation unchanged -- so a
	// reorg can only ever install a branch that is independently just as
	// valid as if every block in it had been finalized block-by-block from
	// scratch.
	// scratch is seeded with baseBlock as its sole anchor block (needed so
	// the first replayed prefix block's ParentHash has something to check
	// against -- see validateBlockLocked) and baseState as the state
	// immediately before it; scratch.FinalizeBlock's own trimming is
	// inert here since scratch.maxHotBlocks is left at its zero value.
	scratch := &Chain{
		ChainID:        chainID,
		TxChainID:      txChainID,
		State:          baseState,
		DEX:            NewDEX(),
		Oracle:         NewOracle(),
		Blocks:         []*Block{baseBlock},
		Mempool:        make(map[string]Tx),
		validatorKeys:  validatorKeys,
		requiredQuorum: requiredQuorum,
	}
	for _, blk := range prefix {
		cp := *blk
		if err := scratch.FinalizeBlock(&cp); err != nil {
			return false, fmt.Errorf("internal error replaying agreed history at height %d: %w", blk.Header.Height, err)
		}
	}
	for _, blk := range candidate {
		if blk == nil {
			return false, ErrBadBlock
		}
		cp := *blk
		if err := scratch.FinalizeBlock(&cp); err != nil {
			return false, fmt.Errorf("candidate branch invalid at height %d: %w", blk.Header.Height, err)
		}
	}

	// The candidate branch is fully valid and strictly heavier. Swap it in.
	// Transactions unique to the discarded branch are not automatically
	// re-queued into the mempool -- the same as any other finalized block,
	// their senders can simply resubmit if still relevant.
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.Blocks) != startLen || c.hotWindowStart != hotStart {
		return false, errors.New("chain advanced during reorg replay, aborting")
	}
	// scratch.Blocks is [baseBlock, replayed-prefix..., replayed-candidate...];
	// skip the seeded anchor and the (already-known-valid, unchanged) prefix,
	// keeping only the freshly replayed candidate tail.
	newBlocks := make([]*Block, 0, len(prefix)+len(candidate))
	newBlocks = append(newBlocks, prefix...)
	newBlocks = append(newBlocks, scratch.Blocks[1+len(prefix):]...)
	c.Blocks = newBlocks
	c.State = scratch.State
	c.trimHotWindowLocked()
	return true, nil
}
