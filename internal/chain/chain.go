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

	gb := &Block{
		Header: BlockHeader{
			Height:       0,
			ParentHash:   "0x0",
			Timestamp:    time.Unix(0, 0).UTC(),
			ProposerID:   "genesis",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    st.Root(),
		},
		Tx:        nil,
		Finalized: true,
	}
	if _, err := gb.ComputeHash(); err != nil {
		return nil, err
	}
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

// BlocksFrom returns all blocks from height from onward.
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
	if err := applyBlockEconomics(tmp, txs, proposerID); err != nil {
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

func (c *Chain) ValidateBlock(b *Block) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.validateBlockLocked(b)
}

func (c *Chain) validateBlockLocked(b *Block) error {
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

	expectedHash, err := b.CalculateHash()
	if err != nil || expectedHash != b.Hash {
		return ErrBadBlock
	}
	expectedTxRoot, err := TxMerkleRoot(b.Tx)
	if err != nil || expectedTxRoot != b.Header.TxMerkleRoot {
		return ErrBadBlock
	}

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
	if err := applyBlockEconomics(tmp, b.Tx, b.Header.ProposerID); err != nil {
		return err
	}
	if tmp.Root() != b.Header.StateRoot {
		return ErrBadBlock
	}
	return nil
}

// FinalizeBlock commits exactly the state transition already validated.
func (c *Chain) FinalizeBlock(b *Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateBlockLocked(b); err != nil {
		return err
	}

	nextState := c.State.Clone()
	for _, tx := range b.Tx {
		if err := nextState.ApplyTx(tx); err != nil {
			return fmt.Errorf("apply finalized transaction %s: %w", tx.ID, err)
		}
	}
	if err := applyBlockEconomics(nextState, b.Tx, b.Header.ProposerID); err != nil {
		return err
	}
	if nextState.Root() != b.Header.StateRoot {
		return ErrBadBlock
	}

	c.State = nextState
	for _, tx := range b.Tx {
		delete(c.Mempool, tx.ID)
	}
	b.Finalized = true
	c.Blocks = append(c.Blocks, b)
	return nil
}

func applyBlockEconomics(st *State, txs []Tx, proposerID string) error {
	var totalFees uint64
	for _, tx := range txs {
		var err error
		totalFees, err = safeAdd(totalFees, tx.Fee)
		if err != nil {
			return fmt.Errorf("block fee overflow: %w", err)
		}
	}
	if totalFees == 0 || proposerID == "" {
		return nil
	}

	burnAmount := (totalFees * BURN_PERCENT) / 100
	rewardAmount := totalFees - burnAmount
	proposerAddr := Address("0x" + proposerID)
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
