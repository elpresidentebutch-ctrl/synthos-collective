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
	// Retain genesis state so a later TryReorg can safely replay an
	// alternative branch from any fork point without depending on the caller
	// (see SeedGenesisState for chains restored from a storage snapshot).
	c.genesisState = st.Clone()
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
	if b.Header.Height > 0 && len(c.validatorKeys) > 0 && b.Header.Height >= c.authEnforceFromHeight {
		if err := c.verifyBlockAuthorizationLocked(b); err != nil {
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
// chain hasn't reached that height. Blocks are stored contiguously by
// height (index == height), so this is a direct lookup.
func (c *Chain) BlockAt(height uint64) *Block {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if height >= uint64(len(c.Blocks)) {
		return nil
	}
	return c.Blocks[height]
}

// verifyBlockAuthorizationLocked checks that b was actually produced and
// approved by real, registered validators: a valid proposer signature from
// Header.ProposerID's registered key, plus valid approval signatures from at
// least requiredQuorum distinct registered validators. Must be called with
// c.mu already held.
func (c *Chain) verifyBlockAuthorizationLocked(b *Block) error {
	proposerKey, ok := c.validatorKeys[b.Header.ProposerID]
	if !ok || len(proposerKey) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: proposer %q is not a registered validator", ErrBadBlock, b.Header.ProposerID)
	}
	propSig, err := decodeHexSig(b.ProposerSignature)
	if err != nil || !ed25519.Verify(proposerKey, []byte(b.Hash), propSig) {
		return fmt.Errorf("%w: invalid or missing proposer signature", ErrBadBlock)
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
	if len(candidate) == 0 {
		c.mu.Unlock()
		return false, errors.New("empty candidate branch")
	}
	if candidate[0] == nil || candidate[0].Header.Height != forkHeight {
		c.mu.Unlock()
		return false, fmt.Errorf("%w: candidate branch must start at height %d", ErrBadBlock, forkHeight)
	}

	prefix := make([]*Block, forkHeight)
	copy(prefix, c.Blocks[:forkHeight])
	currentTail := make([]*Block, uint64(len(c.Blocks))-forkHeight)
	copy(currentTail, c.Blocks[forkHeight:])
	validatorKeys := c.validatorKeys
	requiredQuorum := c.requiredQuorum
	genesisState := c.genesisState.Clone()
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
	scratch := &Chain{
		ChainID:        chainID,
		TxChainID:      txChainID,
		State:          genesisState,
		DEX:            NewDEX(),
		Oracle:         NewOracle(),
		Blocks:         []*Block{prefix[0]},
		Mempool:        make(map[string]Tx),
		validatorKeys:  validatorKeys,
		requiredQuorum: requiredQuorum,
	}
	for _, blk := range prefix[1:] {
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
	if len(c.Blocks) != startLen {
		return false, errors.New("chain advanced during reorg replay, aborting")
	}
	newBlocks := make([]*Block, 0, len(prefix)+len(scratch.Blocks)-1)
	newBlocks = append(newBlocks, prefix...)
	newBlocks = append(newBlocks, scratch.Blocks[len(prefix):]...)
	c.Blocks = newBlocks
	c.State = scratch.State
	return true, nil
}
