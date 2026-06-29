package chain

import (
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
}

var (
	ErrNoGenesis = errors.New("genesis not initialized")
	ErrBadBlock  = errors.New("bad block")
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

	// Create genesis block (height 0).
	gb := &Block{
		Header: BlockHeader{
			Height:     0,
			ParentHash: "0x0",
			// Genesis must be deterministic across all nodes. Do NOT use wall-clock time.
			Timestamp:  time.Unix(0, 0).UTC(),
			ProposerID: "genesis",
			StateRoot:  st.Root(),
		},
		Tx:        nil,
		Finalized: true,
	}
	_, _ = gb.ComputeHash()
	c.Blocks = append(c.Blocks, gb)
	return c, nil
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

// BlocksFrom returns all blocks from height `from` onward.
// Compatible with the JS validator GET /blocks?from=N protocol.
func (c *Chain) BlocksFrom(from int) []*Block {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if from < 0 {
		from = 0
	}
	if from >= len(c.Blocks) {
		return nil
	}
	out := make([]*Block, len(c.Blocks[from:]))
	copy(out, c.Blocks[from:])
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

	// SECURITY: Reject duplicate transaction IDs to prevent replay via mempool.
	if _, exists := c.Mempool[tx.ID]; exists {
		return fmt.Errorf("transaction %s already in mempool", tx.ID)
	}

	// SECURITY: Validate nonce against current state to prevent replay
	expectedNonce := c.State.GetNextNonce(tx.From)
	if tx.Nonce != expectedNonce {
		return fmt.Errorf("nonce mismatch: got %d, expected %d for address %s", tx.Nonce, expectedNonce, tx.From)
	}

	c.Mempool[tx.ID] = tx
	return nil
}

// BuildBlock creates a candidate block from mempool against the current state.
// This does NOT finalize; agents will vote and then call FinalizeBlock.
func (c *Chain) BuildBlock(proposerID string, proposerPoCRoot string, maxTx int) (*Block, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.Blocks) == 0 {
		return nil, ErrNoGenesis
	}
	if maxTx <= 0 {
		maxTx = 1000
	}

	// Apply txs to a temporary state clone in a deterministic order.
	// This ensures that all validators can reproduce the same candidate block
	// given the same mempool and tip state (timeless runtime).
	tmp := c.State.Clone()
	txs := make([]Tx, 0, maxTx)
	candidates := make([]Tx, 0, len(c.Mempool))
	for _, tx := range c.Mempool {
		candidates = append(candidates, tx)
	}
	sort.Slice(candidates, func(i, j int) bool {
		// Higher fee first.
		if candidates[i].Fee != candidates[j].Fee {
			return candidates[i].Fee > candidates[j].Fee
		}
		// Stable sender ordering.
		if candidates[i].From != candidates[j].From {
			return candidates[i].From < candidates[j].From
		}
		// Lower nonce first within a sender.
		if candidates[i].Nonce != candidates[j].Nonce {
			return candidates[i].Nonce < candidates[j].Nonce
		}
		// Stable fallback by tx ID.
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

	parent := c.tipLocked()
	b := &Block{
		Header: BlockHeader{
			Height:     parent.Header.Height + 1,
			ParentHash: parent.Hash,
			// Timeless runtime: no wall-clock timestamps in non-genesis blocks.
			Timestamp:       time.Time{},
			ProposerID:      proposerID,
			StateRoot:       tmp.Root(),
			ProposerPoCRoot: proposerPoCRoot,
		},
		Tx:             txs,
		ValidatorVotes: make(map[string]int),
		Finalized:      false,
	}
	_, err := b.ComputeHash()
	return b, err
}

// ValidateBlock checks basic structure and replays txs to confirm StateRoot.
// SECURITY: Enforces signature verification on all transactions before block acceptance.
func (c *Chain) ValidateBlock(b *Block) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.validateBlockLocked(b)
}

// validateBlockLocked is pure: validation must never mutate canonical state.
func (c *Chain) validateBlockLocked(b *Block) error {
	if b == nil || b.Hash == "" {
		return ErrBadBlock
	}
	tip := c.tipLocked()
	if tip == nil {
		return ErrNoGenesis
	}
	if b.Header.ParentHash != tip.Hash {
		return ErrBadBlock
	}
	if b.Header.Height != tip.Header.Height+1 {
		return ErrBadBlock
	}
	// Timeless runtime: only genesis may have a timestamp.
	if b.Header.Height > 0 && !b.Header.Timestamp.IsZero() {
		return ErrBadBlock
	}

	// SECURITY: Verify all transaction signatures before applying
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
	if tmp.Root() != b.Header.StateRoot {
		return ErrBadBlock
	}
	return nil
}

// FinalizeBlock commits a validated block to canonical chain state.
// Distributes collected fees to the block proposer.
func (c *Chain) FinalizeBlock(b *Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateBlockLocked(b); err != nil {
		return err
	}

	// Calculate total fees collected in this block
	var totalFees uint64
	for _, tx := range b.Tx {
		// Apply to canonical state (should not fail if ValidateBlock passed).
		if err := c.State.ApplyTx(tx); err != nil {
			return fmt.Errorf("apply finalized transaction %s: %w", tx.ID, err)
		}
		var err error
		totalFees, err = safeAdd(totalFees, tx.Fee)
		if err != nil {
			return fmt.Errorf("block fee overflow: %w", err)
		}
		delete(c.Mempool, tx.ID)
	}

	// Distribute collected fees: Proposer gets a cut, the rest is recycled
	if totalFees > 0 && b.Header.ProposerID != "" {
		burnAmount := (totalFees * BURN_PERCENT) / 100
		rewardAmount := totalFees - burnAmount

		proposerAddr := Address("0x" + b.Header.ProposerID)
		proposer := c.State.Get(proposerAddr)
		nextBalance, err := safeAdd(proposer.Balance, rewardAmount)
		if err != nil {
			return fmt.Errorf("proposer reward overflow: %w", err)
		}
		proposer.Balance = nextBalance
		c.State.Set(proposerAddr, proposer)

		// Cryptographically burn tokens (permanently destroying them)
		// We do not add the burnAmount to any account, removing it from circulation.
	}

	b.Finalized = true
	c.Blocks = append(c.Blocks, b)
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

// SnapshotData returns a consistent persistence view.
func (c *Chain) SnapshotData() (string, uint64, []*Block, *State) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	blocks := make([]*Block, len(c.Blocks))
	for i, block := range c.Blocks {
		if block == nil {
			continue
		}
		copyBlock := *block
		copyBlock.Tx = append([]Tx(nil), block.Tx...)
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
