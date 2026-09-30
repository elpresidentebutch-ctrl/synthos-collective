// Package export turns a legacy synthosd node's saved chain state into
// the starting state of a CometBFT chain, keeping every account, balance,
// nonce, asset, citizen stake, governance proposal and bridge record.
package export

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	abcitypes "github.com/cometbft/cometbft/abci/types"

	"synthos-collective/cometapp/app"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/storage"
)

// Legacy is a legacy node's state as read from disk.
type Legacy struct {
	ChainID   string
	TxChainID uint64
	State     *chain.State
	// Height and TipStateRoot come from the newest saved block, when the
	// block files are present next to the state.
	Height       uint64
	TipStateRoot string
	// Verified is true when State hashes to the newest block's state root:
	// the state is exactly what the network agreed on at Height.
	Verified bool
}

// Load reads a legacy node's saved state. path is either the node's data
// directory (holding state.json and blocks/, or the older chain.json) or
// a state.json file on its own.
func Load(path string) (*Legacy, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	var l Legacy
	if info.IsDir() {
		store, err := storage.New(path)
		if err != nil {
			return nil, err
		}
		snap, err := store.Load()
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		l = Legacy{ChainID: snap.ChainID, TxChainID: snap.TxChainID, State: snap.State}
		if n := len(snap.Blocks); n > 0 {
			tip := snap.Blocks[n-1]
			l.Height, l.TipStateRoot = tip.Header.Height, tip.Header.StateRoot
		}
	} else {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var f struct {
			ChainID   string       `json:"chain_id"`
			TxChainID uint64       `json:"tx_chain_id"`
			State     *chain.State `json:"state"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("%s is not a synthosd state file: %w", path, err)
		}
		l = Legacy{ChainID: f.ChainID, TxChainID: f.TxChainID, State: f.State}
		// A state.json normally sits next to its blocks/ directory.
		if tip, ok := newestBlock(filepath.Join(filepath.Dir(path), "blocks")); ok {
			l.Height, l.TipStateRoot = tip.Header.Height, tip.Header.StateRoot
		}
	}
	if l.State == nil {
		return nil, errors.New("no chain state found")
	}
	l.Verified = l.TipStateRoot != "" && l.State.Root() == l.TipStateRoot
	return &l, nil
}

// newestBlock finds the highest-numbered blocks/<height>.json.
func newestBlock(dir string) (*chain.Block, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	var best *chain.Block
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var b chain.Block
		if json.Unmarshal(raw, &b) != nil {
			continue
		}
		if best == nil || b.Header.Height > best.Header.Height {
			bb := b
			best = &bb
		}
	}
	return best, best != nil
}

// Totals summarizes where every SYN in a state is.
type Totals struct {
	Accounts        int    `json:"accounts"`
	Balances        uint64 `json:"balances"`
	CitizenStaked   uint64 `json:"citizen_staked"`
	ValidatorLocked uint64 `json:"validator_locked"`
	// Held is Balances + CitizenStaked + ValidatorLocked.
	Held      uint64 `json:"held"`
	MaxSupply uint64 `json:"max_supply"`
	// Burned is MaxSupply - Held: fees burned and supply never issued.
	Burned uint64 `json:"burned_or_unissued"`

	Treasury        chain.Address `json:"treasury_address"`
	TreasuryBalance uint64        `json:"treasury_balance"`
	Founder         chain.Address `json:"founder_address"`
	CitizenRateBps  uint64        `json:"citizen_reward_rate_bps_per_year"`
	StateRoot       string        `json:"state_root"`
}

// Summarize totals st. It fails if the amounts overflow or exceed the
// maximum supply, which would mean the state is corrupt.
func Summarize(st *chain.State) (Totals, error) {
	t := Totals{MaxSupply: chain.MAX_SUPPLY, Treasury: st.TreasuryAddress, Founder: st.GovernanceFounder,
		CitizenRateBps: st.CitizenRewardRateBpsPerYear, StateRoot: st.Root()}
	add := func(sum *uint64, v uint64) error {
		if *sum+v < *sum {
			return errors.New("amounts overflow: the state is corrupt")
		}
		*sum += v
		return nil
	}
	for _, acc := range st.Accounts {
		t.Accounts++
		if err := add(&t.Balances, acc.Balance); err != nil {
			return t, err
		}
	}
	for _, s := range st.CitizenStakes {
		if err := add(&t.CitizenStaked, s.Amount); err != nil {
			return t, err
		}
	}
	t.ValidatorLocked = st.TotalValidatorLocked()
	t.Held = t.Balances
	for _, v := range []uint64{t.CitizenStaked, t.ValidatorLocked} {
		if err := add(&t.Held, v); err != nil {
			return t, err
		}
	}
	if t.Held > t.MaxSupply {
		return t, fmt.Errorf("the state holds %d SYN, more than the maximum supply %d", t.Held, t.MaxSupply)
	}
	t.Burned = t.MaxSupply - t.Held
	if st.TreasuryAddress != "" {
		t.TreasuryBalance = st.Get(st.TreasuryAddress).Balance
	}
	return t, nil
}

// Options describe the new chain.
type Options struct {
	ChainID   string
	TxChainID uint64
	// AllowSameTxChainID must be set to reuse the legacy chain's
	// transaction chain ID. Only the final mainnet switch should: on a
	// test network, sharing it would let anyone copy a transaction signed
	// for the test network onto the real one (same accounts, same nonces).
	AllowSameTxChainID bool
	Staking            chain.ValidatorStakingParams
	Validators         []app.GenesisValidator
	// LegacyGenesis, when given, fills in the treasury address, founder
	// address and citizen reward rate if the saved state lost them (the
	// legacy node re-applies them from its genesis file at every start).
	LegacyGenesis *chain.Genesis
}

// Genesis builds the new chain's genesis from a legacy state.
func Genesis(l *Legacy, o Options) (app.Genesis, error) {
	if o.ChainID == "" {
		return app.Genesis{}, errors.New("a chain ID for the new chain is required")
	}
	if o.TxChainID == 0 {
		return app.Genesis{}, errors.New("a transaction chain ID for the new chain is required")
	}
	legacyTx := l.TxChainID
	if legacyTx == 0 {
		legacyTx = 1
	}
	if o.TxChainID == legacyTx && !o.AllowSameTxChainID {
		return app.Genesis{}, fmt.Errorf("transaction chain ID %d is the legacy chain's own: transactions signed for the new chain could be replayed on the old one. Use a different one for a test network (reusing it is only for the final mainnet switch)", legacyTx)
	}
	st := l.State.Clone()
	// Validators come from the genesis validator list; anything a legacy
	// node recorded about its own consensus doesn't carry over.
	st.Validators = map[chain.Address]chain.ValidatorRecord{}
	st.ValidatorSetSnapshot = nil
	if g := o.LegacyGenesis; g != nil {
		ref, err := g.ToState()
		if err != nil {
			return app.Genesis{}, fmt.Errorf("legacy genesis: %w", err)
		}
		if st.TreasuryAddress == "" {
			st.TreasuryAddress = ref.TreasuryAddress
		}
		if st.CitizenRewardRateBpsPerYear == 0 {
			st.CitizenRewardRateBpsPerYear = ref.CitizenRewardRateBpsPerYear
		}
		if st.GovernanceFounder == "" {
			st.GovernanceFounder = ref.GovernanceFounder
		}
	}
	vals := append([]app.GenesisValidator(nil), o.Validators...)
	sort.Slice(vals, func(i, j int) bool { return vals[i].Operator < vals[j].Operator })
	g := app.Genesis{
		Chain:            chain.Genesis{ChainID: o.ChainID, TxChainID: o.TxChainID},
		Staking:          o.Staking,
		Validators:       vals,
		InitialState:     st,
		InitialStateRoot: st.Root(),
	}
	if err := g.Validate(); err != nil {
		return app.Genesis{}, err
	}
	return g, nil
}

// Check boots g the way every node will (CometBFT's InitChain on a fresh
// app) and returns the resulting app hash, so a genesis file that can't
// start a chain is caught before it is handed out.
func Check(g app.Genesis) (string, error) {
	raw, err := json.Marshal(g)
	if err != nil {
		return "", err
	}
	a, err := app.New("")
	if err != nil {
		return "", err
	}
	res, err := a.InitChain(context.Background(), &abcitypes.RequestInitChain{
		ChainId: g.Chain.ChainID, AppStateBytes: raw, InitialHeight: 1,
	})
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(res.AppHash), nil
}
