// Package app runs the SYNTHOS chain rules as a CometBFT application.
//
// CometBFT owns everything about agreeing on blocks: peer networking,
// proposer rotation, voting, finality, and catching validators that sign
// conflicting votes. This package owns what blocks mean: it applies each
// transaction through internal/chain (the same code the legacy chain
// uses), pays fees, turns CometBFT's misbehavior reports into slashing,
// and tells CometBFT which validators hold stake.
package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"

	"synthos-collective/internal/chain"
)

// AppVersion is reported to CometBFT in Info.
const AppVersion uint64 = 1

// GenesisValidator seeds the first validator set of a new chain. Its
// self-bond is moved out of the operator's genesis balance.
type GenesisValidator struct {
	Operator        chain.Address `json:"operator"`
	ConsensusPubKey string        `json:"consensus_pub_key"`
	SelfBond        uint64        `json:"self_bond"`
	Moniker         string        `json:"moniker,omitempty"`
	Endpoint        string        `json:"endpoint,omitempty"`
}

// Genesis is the app_state CometBFT's genesis.json carries for SYNTHOS.
type Genesis struct {
	// Chain is the existing SYNTHOS genesis format: balances and metadata
	// (founder/treasury addresses, citizen reward rate, ...).
	Chain chain.Genesis `json:"chain"`
	// Staking are the chain-wide validator rules. Living in genesis, they
	// are identical on every node by construction.
	Staking chain.ValidatorStakingParams `json:"staking"`
	// Validators are the first validators; more join by bonding.
	Validators []GenesisValidator `json:"validators"`
}

// Validate checks rules that only make sense under CometBFT: staking must
// be on from the first block (it's the only way to become a validator)
// and the legacy stake-consensus switch must stay off (CometBFT decides
// block authorization).
func (g Genesis) Validate() error {
	if err := g.Chain.Validate(); err != nil {
		return err
	}
	if g.Staking.EnabledFromHeight != 1 {
		return errors.New("staking.enabled_from_height must be 1: under CometBFT, bonding is how validators join")
	}
	if g.Staking.ConsensusFromHeight != 0 {
		return errors.New("staking.consensus_from_height must be 0: CometBFT authorizes blocks itself")
	}
	if g.Staking.EpochBlocks == 0 {
		return errors.New("staking.epoch_blocks must be greater than zero")
	}
	if g.Staking.SlashFractionBps == 0 || g.Staking.SlashFractionBps > 10_000 {
		return errors.New("staking.slash_fraction_bps must be between 1 and 10000")
	}
	if len(g.Validators) == 0 {
		return errors.New("genesis needs at least one validator")
	}
	return g.Staking.Validate()
}

// persisted is what Commit writes to disk.
type persisted struct {
	ChainID   string                       `json:"chain_id"`
	TxChainID uint64                       `json:"tx_chain_id"`
	Staking   chain.ValidatorStakingParams `json:"staking"`
	Height    int64                        `json:"height"`
	AppHash   string                       `json:"app_hash"`
	State     *chain.State                 `json:"state"`
}

const stateFileName = "synthos-app-state.json"

// App implements abcitypes.Application.
type App struct {
	abcitypes.BaseApplication

	mu      sync.Mutex
	dataDir string // "" keeps everything in memory (tests)

	chainID   string
	txChainID uint64
	staking   chain.ValidatorStakingParams

	// committed is the state after the last committed block.
	committed *chain.State
	height    int64
	appHash   []byte

	// pending is the result of FinalizeBlock, made durable by Commit.
	pending        *chain.State
	pendingHeight  int64
	pendingAppHash []byte

	// check is committed state plus transactions accepted into the
	// mempool since, so a sender can queue several transactions with
	// consecutive nonces.
	check *chain.State
}

var _ abcitypes.Application = (*App)(nil)

// New returns an App that persists its state in dataDir (created if
// missing), resuming from whatever was last committed there. An empty
// dataDir keeps state in memory only.
func New(dataDir string) (*App, error) {
	a := &App{dataDir: dataDir}
	if dataDir == "" {
		return a, nil
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, stateFileName))
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	var p persisted
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("reading %s: %w", stateFileName, err)
	}
	hash, err := decodeRoot(p.AppHash)
	if err != nil {
		return nil, err
	}
	if got := p.State.Root(); got != p.AppHash {
		return nil, fmt.Errorf("%s is corrupt: stored app hash %s but state hashes to %s", stateFileName, p.AppHash, got)
	}
	a.chainID, a.txChainID, a.staking = p.ChainID, p.TxChainID, p.Staking
	a.committed, a.height, a.appHash = p.State, p.Height, hash
	a.check = p.State.Clone()
	return a, nil
}

func decodeRoot(root string) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(root, "0x"))
	if err != nil {
		return nil, fmt.Errorf("bad state root %q: %w", root, err)
	}
	return b, nil
}

// txContext is what transactions in the block at height see. blockTime is
// the block's consensus time: CometBFT's BFT time for a decided block
// (every validator computes the same one), or the node's clock for a
// mempool check. It replaces each transaction's own, unsigned timestamp.
func (a *App) txContext(height int64, blockTime time.Time) chain.TxContext {
	ctx := chain.TxContext{Height: uint64(height), ChainID: a.chainID, Staking: a.staking}
	if !blockTime.IsZero() {
		ctx.BlockTime = blockTime.Unix()
	}
	return ctx
}

// Info tells CometBFT how far this app has committed, so after a restart
// CometBFT replays only the blocks the app hasn't seen.
func (a *App) Info(context.Context, *abcitypes.RequestInfo) (*abcitypes.ResponseInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return &abcitypes.ResponseInfo{
		Data:             "synthos",
		AppVersion:       AppVersion,
		LastBlockHeight:  a.height,
		LastBlockAppHash: a.appHash,
	}, nil
}

// InitChain builds the genesis state and the first validator set.
func (a *App) InitChain(_ context.Context, req *abcitypes.RequestInitChain) (*abcitypes.ResponseInitChain, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var g Genesis
	if err := json.Unmarshal(req.AppStateBytes, &g); err != nil {
		return nil, fmt.Errorf("parsing app_state: %w", err)
	}
	if g.Chain.ChainID != req.ChainId {
		return nil, fmt.Errorf("app_state chain.chain_id %q does not match CometBFT chain_id %q", g.Chain.ChainID, req.ChainId)
	}
	if err := g.Validate(); err != nil {
		return nil, err
	}
	st, err := g.Chain.ToState()
	if err != nil {
		return nil, err
	}
	for _, v := range g.Validators {
		if err := st.BondGenesisValidator(v.Operator, v.ConsensusPubKey, v.SelfBond, v.Moniker, v.Endpoint); err != nil {
			return nil, fmt.Errorf("genesis validator %s: %w", v.Operator, err)
		}
	}
	set := st.ActiveValidatorSet(g.Staking)
	if len(set) == 0 {
		return nil, errors.New("no genesis validator meets staking.min_self_bond")
	}
	st.ReplaceValidatorSetSnapshot(set)
	updates, err := validatorUpdates(nil, set)
	if err != nil {
		return nil, err
	}
	hash, err := decodeRoot(st.Root())
	if err != nil {
		return nil, err
	}

	a.chainID = req.ChainId
	a.txChainID = g.Chain.TxChainID
	if a.txChainID == 0 {
		a.txChainID = 1 // same default the legacy chain uses
	}
	a.staking = g.Staking
	a.committed, a.height, a.appHash = st, 0, hash
	a.check = st.Clone()
	return &abcitypes.ResponseInitChain{Validators: updates, AppHash: hash}, nil
}

func (a *App) decodeTx(raw []byte) (chain.Tx, error) {
	var tx chain.Tx
	if err := json.Unmarshal(raw, &tx); err != nil {
		return tx, fmt.Errorf("not a SYNTHOS transaction: %w", err)
	}
	if tx.ChainID != a.txChainID {
		return tx, fmt.Errorf("wrong transaction chain ID: got %d, want %d", tx.ChainID, a.txChainID)
	}
	return tx, nil
}

// CheckTx admits a transaction to the mempool only if it would apply on
// top of everything already admitted.
func (a *App) CheckTx(_ context.Context, req *abcitypes.RequestCheckTx) (*abcitypes.ResponseCheckTx, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.check == nil {
		return &abcitypes.ResponseCheckTx{Code: 1, Log: "chain not initialized"}, nil
	}
	tx, err := a.decodeTx(req.Tx)
	if err != nil {
		return &abcitypes.ResponseCheckTx{Code: 1, Log: err.Error()}, nil
	}
	next := a.check.Clone()
	if err := chain.ApplyTransaction(next, tx, a.txContext(a.height+1, time.Now())); err != nil {
		return &abcitypes.ResponseCheckTx{Code: 1, Log: err.Error()}, nil
	}
	a.check = next
	return &abcitypes.ResponseCheckTx{Code: 0}, nil
}

// FinalizeBlock executes a decided block. A transaction that fails is
// reported as failed and changes nothing (not even its fee); the rest of
// the block still applies, as CometBFT expects.
func (a *App) FinalizeBlock(_ context.Context, req *abcitypes.RequestFinalizeBlock) (*abcitypes.ResponseFinalizeBlock, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.committed == nil {
		return nil, errors.New("FinalizeBlock before InitChain")
	}
	st := a.committed.Clone()
	ctx := a.txContext(req.Height, req.Time)

	// Resolve the proposer against the state before this block's
	// transactions, so a bond or slash inside the block can't change who
	// gets paid for it.
	proposer := operatorForConsensusAddress(st, req.ProposerAddress)

	results := make([]*abcitypes.ExecTxResult, len(req.Txs))
	applied := make([]chain.Tx, 0, len(req.Txs))
	for i, raw := range req.Txs {
		tx, err := a.decodeTx(raw)
		if err == nil {
			next := st.Clone()
			if err = chain.ApplyTransaction(next, tx, ctx); err == nil {
				st = next
				applied = append(applied, tx)
			}
		}
		if err != nil {
			results[i] = &abcitypes.ExecTxResult{Code: 1, Log: err.Error()}
			continue
		}
		results[i] = &abcitypes.ExecTxResult{Code: 0}
	}
	if err := chain.ApplyBlockFees(st, applied, proposer); err != nil {
		return nil, err
	}

	slashedAny := false
	for _, m := range req.Misbehavior {
		key, ok := consensusKeyForAddress(st, m.Validator.Address)
		if !ok {
			continue
		}
		if _, ok := st.SlashForDoubleSign(key, a.staking.SlashFractionBps); ok {
			slashedAny = true
		}
	}

	var updates []abcitypes.ValidatorUpdate
	if slashedAny || uint64(req.Height)%a.staking.EpochBlocks == 0 {
		current := st.AuthorizingValidatorSet()
		next := st.ActiveValidatorSet(a.staking)
		// CometBFT can't run with an empty validator set; if stake would
		// empty it (everyone exited or was slashed), keep the current set.
		if len(next) > 0 {
			var err error
			if updates, err = validatorUpdates(current, next); err != nil {
				return nil, err
			}
			st.ReplaceValidatorSetSnapshot(next)
		}
	}

	hash, err := decodeRoot(st.Root())
	if err != nil {
		return nil, err
	}
	a.pending, a.pendingHeight, a.pendingAppHash = st, req.Height, hash
	return &abcitypes.ResponseFinalizeBlock{TxResults: results, ValidatorUpdates: updates, AppHash: hash}, nil
}

// Commit makes the last finalized block durable and resets the mempool
// view to it.
func (a *App) Commit(context.Context, *abcitypes.RequestCommit) (*abcitypes.ResponseCommit, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending == nil {
		return nil, errors.New("Commit without FinalizeBlock")
	}
	a.committed, a.height, a.appHash = a.pending, a.pendingHeight, a.pendingAppHash
	a.pending = nil
	a.check = a.committed.Clone()
	if err := a.persistLocked(); err != nil {
		return nil, err
	}
	return &abcitypes.ResponseCommit{}, nil
}

func (a *App) persistLocked() error {
	if a.dataDir == "" {
		return nil
	}
	raw, err := json.Marshal(persisted{
		ChainID: a.chainID, TxChainID: a.txChainID, Staking: a.staking,
		Height: a.height, AppHash: "0x" + hex.EncodeToString(a.appHash), State: a.committed,
	})
	if err != nil {
		return err
	}
	// Write-then-rename, so a crash mid-write leaves the previous
	// committed state intact rather than a truncated file.
	final := filepath.Join(a.dataDir, stateFileName)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// Query serves read-only lookups against committed state:
//
//	path "account",    data = address  -> the account (balance, nonce)
//	path "validators"                  -> registered, active and current sets
//	path "status"                      -> height and app hash
func (a *App) Query(_ context.Context, req *abcitypes.RequestQuery) (*abcitypes.ResponseQuery, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.committed == nil {
		return &abcitypes.ResponseQuery{Code: 1, Log: "chain not initialized"}, nil
	}
	var body any
	switch strings.Trim(req.Path, "/") {
	case "account":
		body = a.committed.Get(chain.Address(string(req.Data)))
	case "validators":
		body = map[string]any{
			"registered": a.committed.ValidatorRecords(),
			"active":     a.committed.ActiveValidatorSet(a.staking),
			"current":    a.committed.AuthorizingValidatorSet(),
		}
	case "status":
		body = map[string]any{"height": a.height, "app_hash": "0x" + hex.EncodeToString(a.appHash), "chain_id": a.chainID}
	default:
		return &abcitypes.ResponseQuery{Code: 1, Log: "unknown query path " + req.Path}, nil
	}
	value, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &abcitypes.ResponseQuery{Code: 0, Value: value, Height: a.height}, nil
}

// ConsensusAddress returns the 20-byte address CometBFT uses for an
// ed25519 consensus key given as 0x hex.
func ConsensusAddress(consensusKeyHex string) ([]byte, error) {
	pub, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(consensusKeyHex), "0x"))
	if err != nil || len(pub) != cmted25519.PubKeySize {
		return nil, fmt.Errorf("bad consensus key %q", consensusKeyHex)
	}
	return cmted25519.PubKey(pub).Address(), nil
}

func consensusKeyForAddress(st *chain.State, addr []byte) (string, bool) {
	for _, rec := range st.ValidatorRecords() {
		a, err := ConsensusAddress(rec.ConsensusPubKey)
		if err == nil && string(a) == string(addr) {
			return rec.ConsensusPubKey, true
		}
	}
	return "", false
}

func operatorForConsensusAddress(st *chain.State, addr []byte) chain.Address {
	key, ok := consensusKeyForAddress(st, addr)
	if !ok {
		return ""
	}
	rec, _ := st.ValidatorByConsensusKey(key)
	return rec.Operator
}

// validatorUpdates returns the CometBFT updates that turn set from into
// set to: new or changed powers, and power 0 for anyone dropped. Order
// follows to, then removals in from's order, so it's deterministic.
func validatorUpdates(from, to []chain.ActiveValidator) ([]abcitypes.ValidatorUpdate, error) {
	old := make(map[string]uint64, len(from))
	for _, v := range from {
		old[v.ConsensusPubKey] = v.Power
	}
	keep := make(map[string]bool, len(to))
	var out []abcitypes.ValidatorUpdate
	for _, v := range to {
		keep[v.ConsensusPubKey] = true
		if p, ok := old[v.ConsensusPubKey]; ok && p == v.Power {
			continue
		}
		u, err := update(v.ConsensusPubKey, v.Power)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	for _, v := range from {
		if keep[v.ConsensusPubKey] {
			continue
		}
		u, err := update(v.ConsensusPubKey, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func update(keyHex string, power uint64) (abcitypes.ValidatorUpdate, error) {
	pub, err := hex.DecodeString(strings.TrimPrefix(keyHex, "0x"))
	if err != nil || len(pub) != cmted25519.PubKeySize {
		return abcitypes.ValidatorUpdate{}, fmt.Errorf("bad consensus key %q", keyHex)
	}
	if power > 1<<60 {
		return abcitypes.ValidatorUpdate{}, fmt.Errorf("voting power %d too large", power)
	}
	return abcitypes.Ed25519ValidatorUpdate(pub, int64(power)), nil
}
