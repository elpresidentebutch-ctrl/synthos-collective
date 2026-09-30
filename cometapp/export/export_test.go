package export

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synthos-collective/cometapp/app"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/storage"
)

const legacyTxChainID = 20260702

var staking = chain.ValidatorStakingParams{EnabledFromHeight: 1, MinSelfBond: 1_000, UnbondingBlocks: 20,
	EpochBlocks: 10, SlashFractionBps: 500, ReporterRewardBps: 1_000}

type fixture struct {
	dir      string
	treasury chain.Address
	founder  chain.Address
	operator chain.Address
	holder   chain.Address
	key      string
}

// savedLegacyNode writes a legacy node's data directory the way synthosd
// does (storage.Store.Save), with state beyond plain balances: a nonce,
// an asset balance and a citizen stake.
func savedLegacyNode(t *testing.T) fixture {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	f := fixture{
		dir:      t.TempDir(),
		treasury: "0xa4aabbc7e5841259b61afb4b4fbfcd7dae7b0501",
		founder:  "0xf0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0",
		operator: "0x0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e",
		holder:   "0x1111111111111111111111111111111111111111",
		key:      "0x" + hex.EncodeToString(pub),
	}
	c, err := chain.NewChain(chain.Genesis{
		ChainID: "synthos-collective", TxChainID: legacyTxChainID,
		Alloc: map[chain.Address]uint64{f.treasury: 13_000_000_000, f.founder: 1_000_000, f.operator: 500_000, f.holder: 70_000},
		Metadata: map[string]any{
			"treasury_address": string(f.treasury), "founder_address": string(f.founder),
			"citizen_reward_rate_bps_per_year": float64(650),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Give the holder a nonce, an asset and a citizen stake, as a node
	// that had processed transactions would have saved.
	acc := c.State.Get(f.holder)
	acc.Nonce = 3
	acc.Assets = map[string]uint64{"gold": 9}
	c.State.Set(f.holder, acc)
	if err := c.State.StakeCitizen(f.holder, 20_000, 1_790_000_000); err != nil {
		t.Fatal(err)
	}
	store, err := storage.New(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fixture) options() Options {
	return Options{
		ChainID: "synthos-testnet-1", TxChainID: 20261001, Staking: staking,
		Validators: []app.GenesisValidator{{Operator: f.operator, ConsensusPubKey: f.key, SelfBond: 100_000, Moniker: "val-1"}},
	}
}

func TestExportKeepsEveryAccountAndStake(t *testing.T) {
	f := savedLegacyNode(t)
	l, err := Load(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.ChainID != "synthos-collective" || l.TxChainID != legacyTxChainID {
		t.Fatalf("legacy ids = %q / %d", l.ChainID, l.TxChainID)
	}
	before, err := Summarize(l.State)
	if err != nil {
		t.Fatal(err)
	}
	if before.Held != 13_000_000_000+1_000_000+500_000+70_000 || before.CitizenStaked != 20_000 {
		t.Fatalf("legacy totals = %+v", before)
	}

	g, err := Genesis(l, f.options())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Check(g); err != nil {
		t.Fatalf("genesis does not boot: %v", err)
	}

	// Round-trip through JSON (what nodes actually read) and start a chain.
	raw, _ := json.Marshal(g)
	var back app.Genesis
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(back); err != nil {
		t.Fatalf("genesis after JSON round trip: %v", err)
	}

	st := back.InitialState
	h := st.Get(f.holder)
	if h.Balance != 50_000 || h.Nonce != 3 || h.Assets["gold"] != 9 {
		t.Fatalf("holder = %+v", h)
	}
	if st.GetCitizenStake(f.holder).Amount != 20_000 {
		t.Fatal("citizen stake lost")
	}
	if st.TreasuryAddress != f.treasury || st.GovernanceFounder != f.founder || st.CitizenRewardRateBpsPerYear != 650 {
		t.Fatalf("settings lost: treasury %s founder %s rate %d", st.TreasuryAddress, st.GovernanceFounder, st.CitizenRewardRateBpsPerYear)
	}
	after, err := Summarize(st)
	if err != nil {
		t.Fatal(err)
	}
	if after.Held != before.Held || after.Accounts != before.Accounts {
		t.Fatalf("totals changed: before %+v after %+v", before, after)
	}
}

func TestVerifiedAgainstNewestBlock(t *testing.T) {
	f := savedLegacyNode(t)
	// Saved straight from genesis, the state differs from the genesis
	// block's root (the test edited it), so it is loaded but not verified.
	l, err := Load(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.TipStateRoot == "" {
		t.Fatal("tip block not found")
	}
	if l.Verified {
		t.Fatal("an edited state was reported as matching the chain")
	}

	// An untouched chain verifies, from the directory or from state.json.
	clean := t.TempDir()
	c, err := chain.NewChain(chain.Genesis{ChainID: "synthos-collective", Alloc: map[chain.Address]uint64{f.holder: 5}})
	if err != nil {
		t.Fatal(err)
	}
	store, _ := storage.New(clean)
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{clean, filepath.Join(clean, "state.json")} {
		l, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if !l.Verified {
			t.Fatalf("%s: untouched state not verified (root %s, tip %s)", p, l.State.Root(), l.TipStateRoot)
		}
	}
}

func TestRefusesUnsafeOrBrokenGenesis(t *testing.T) {
	f := savedLegacyNode(t)
	l, err := Load(f.dir)
	if err != nil {
		t.Fatal(err)
	}

	o := f.options()
	o.TxChainID = legacyTxChainID
	if _, err := Genesis(l, o); err == nil || !strings.Contains(err.Error(), "replayed") {
		t.Fatalf("same tx chain ID accepted: %v", err)
	}
	o.AllowSameTxChainID = true
	if _, err := Genesis(l, o); err != nil {
		t.Fatalf("mainnet switch with same tx chain ID refused: %v", err)
	}

	// A validator whose operator can't cover the bond: the genesis can't boot.
	o = f.options()
	o.Validators[0].SelfBond = 10_000_000
	if g, err := Genesis(l, o); err == nil {
		if _, err := Check(g); err == nil {
			t.Fatal("genesis with an unfunded validator bond booted")
		}
	}

	// A hand-edited balance no longer matches initial_state_root.
	g, err := Genesis(l, f.options())
	if err != nil {
		t.Fatal(err)
	}
	acc := g.InitialState.Get(f.holder)
	acc.Balance += 1_000_000
	g.InitialState.Set(f.holder, acc)
	if _, err := Check(g); err == nil || !strings.Contains(err.Error(), "edited") {
		t.Fatalf("edited genesis accepted: %v", err)
	}
}

func TestLegacyGenesisFillsLostSettings(t *testing.T) {
	f := savedLegacyNode(t)
	l, err := Load(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	// What the live nodes' saved state looked like before the fix in
	// ReapplyGenesisCitizenRewardConfig: settings zeroed.
	l.State.TreasuryAddress, l.State.GovernanceFounder, l.State.CitizenRewardRateBpsPerYear = "", "", 0
	o := f.options()
	o.LegacyGenesis = &chain.Genesis{ChainID: "synthos-collective", Alloc: map[chain.Address]uint64{f.treasury: 1},
		Metadata: map[string]any{"treasury_address": string(f.treasury), "founder_address": string(f.founder),
			"citizen_reward_rate_bps_per_year": float64(650)}}
	g, err := Genesis(l, o)
	if err != nil {
		t.Fatal(err)
	}
	st := g.InitialState
	if st.TreasuryAddress != f.treasury || st.GovernanceFounder != f.founder || st.CitizenRewardRateBpsPerYear != 650 {
		t.Fatal("settings not restored from the legacy genesis")
	}
	if l.State.TreasuryAddress != "" {
		t.Fatal("export modified the loaded legacy state")
	}
}

func TestLoadRejectsNonState(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")
	if err := os.WriteFile(p, []byte(`{"hello":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("file without a state accepted")
	}
}
