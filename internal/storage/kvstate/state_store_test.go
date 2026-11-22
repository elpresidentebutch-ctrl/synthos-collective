package kvstate_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/storage/kvstate"
)

func TestKVStateStore_EmptyStateRootMatches(t *testing.T) {
	st := chain.NewState()
	kvStore := kvstate.NewMemKVStore()
	store := kvstate.NewKVStateStore(kvStore)

	if err := store.SyncFromState(st); err != nil {
		t.Fatalf("sync from empty state failed: %v", err)
	}

	stRoot := st.Root()
	kvRoot := store.Root()

	if kvRoot != stRoot {
		t.Fatalf("empty state root mismatch:\n  got:  %s\n  want: %s", kvRoot, stRoot)
	}
}

func TestKVStateStore_MultiAccountRootMatches(t *testing.T) {
	st := chain.NewState()

	// Seed multiple accounts with varying balances, nonces, and assets
	accounts := []struct {
		addr    chain.Address
		balance uint64
		nonce   uint64
		assets  map[string]uint64
	}{
		{"0x1111111111111111111111111111111111111111", 5000, 1, nil},
		{"0x2222222222222222222222222222222222222222", 1000000, 0, map[string]uint64{"usdt": 500}},
		{"0x3333333333333333333333333333333333333333", 42, 10, map[string]uint64{"syn_dex": 1234}},
		{"0x4444444444444444444444444444444444444444", 0, 5, nil},
		{"0x5555555555555555555555555555555555555555", 999999999, 99, map[string]uint64{"token_a": 1, "token_b": 2}},
	}

	for _, a := range accounts {
		st.Set(a.addr, chain.Account{
			Balance: a.balance,
			Nonce:   a.nonce,
			Assets:  a.assets,
		})
	}

	store := kvstate.NewKVStateStore(kvstate.NewMemKVStore())
	if err := store.SyncFromState(st); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if got, want := store.AccountCount(), len(accounts); got != want {
		t.Fatalf("account count mismatch: got %d, want %d", got, want)
	}

	stRoot := st.Root()
	kvRoot := store.Root()

	if kvRoot != stRoot {
		t.Fatalf("multi-account root mismatch:\n  got:  %s\n  want: %s", kvRoot, stRoot)
	}
}

func TestKVStateStore_ConsensusObjectsRootMatches(t *testing.T) {
	st := chain.NewState()

	// 1. Accounts
	st.Set("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", chain.Account{Balance: 10000, Nonce: 0})
	st.Set("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", chain.Account{Balance: 20000, Nonce: 1})

	// 2. Bridge events
	st.BridgeEvents["evt-1"] = chain.BridgeRecord{
		ID:                 "evt-1",
		Type:               "bridge_lock_native",
		Address:            "0xsender1",
		Recipient:          "0xrecip1",
		Amount:             500,
		DestinationChainID: "base",
		TxID:               "0xtxid1",
		Timestamp:          1700000000,
	}
	st.ProcessedBridgeEvents["evt-1"] = true
	st.BridgeValidators["0xval1"] = "active"
	st.BridgeQuorum = 3
	st.LastBridgeEventID = "evt-1"

	// 3. Validators
	st.Validators["0xop1"] = chain.ValidatorRecord{
		Operator:        "0xop1",
		ConsensusPubKey: "0xkey1",
		Moniker:         "validator-one",
		SelfBond:        10000,
	}

	// 4. Validator Set Snapshot
	st.ValidatorSetSnapshot = []chain.ActiveValidator{
		{Operator: "0xop1", ConsensusPubKey: "0xkey1", Moniker: "validator-one", Power: 10000},
	}

	// 5. Fee Epoch State
	st.FeeEpochTreasuryIncome = 500
	st.FeeEpochTotalPower = 10000
	st.FeeEpochSignedPower["0xop1"] = 10000

	// 6. Fee Model V3 Base Fee
	st.FeeModelV3BaseFee = 15

	store := kvstate.NewKVStateStore(kvstate.NewMemKVStore())
	if err := store.SyncFromState(st); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	stRoot := st.Root()
	kvRoot := store.Root()

	if kvRoot != stRoot {
		t.Fatalf("consensus objects state root mismatch:\n  got:  %s\n  want: %s", kvRoot, stRoot)
	}
}

func TestKVStateStore_MainnetSnapshotExactRootMatch(t *testing.T) {
	// Locate isolated benchmark state copy
	benchCopyPath := filepath.Join(os.TempDir(), "synthos-evm-benchmark", "state-copy.json")
	raw, err := os.ReadFile(benchCopyPath)
	if err != nil {
		t.Skipf("benchmark state copy not found at %s (skipping mainnet snapshot test): %v", benchCopyPath, err)
	}

	var p struct {
		ChainID   string                       `json:"chain_id"`
		TxChainID uint64                       `json:"tx_chain_id"`
		Staking   chain.ValidatorStakingParams `json:"staking"`
		Height    int64                        `json:"height"`
		AppHash   string                       `json:"app_hash"`
		State     *chain.State                 `json:"state"`
	}

	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal mainnet state copy failed: %v", err)
	}

	if p.State == nil {
		t.Fatalf("state in snapshot is nil")
	}

	stRoot := p.State.Root()
	t.Logf("Mainnet State Root at height %d: %s", p.Height, stRoot)

	// Sync into prototype KVStateStore
	store := kvstate.NewKVStateStore(kvstate.NewMemKVStore())
	if err := store.SyncFromState(p.State); err != nil {
		t.Fatalf("sync mainnet state failed: %v", err)
	}

	kvRoot := store.Root()
	t.Logf("KVStateStore Root at height %d:     %s", p.Height, kvRoot)

	// PROVE BYTE-FOR-BYTE ROOT EQUIVALENCE
	if kvRoot != stRoot {
		t.Fatalf("BYTE-FOR-BYTE ROOT MISMATCH ON MAINNET STATE!\n  stRoot: %s\n  kvRoot: %s", stRoot, kvRoot)
	}

	// Verify Account count and stake
	if store.AccountCount() != len(p.State.Accounts) {
		t.Errorf("account count mismatch: kv=%d, st=%d", store.AccountCount(), len(p.State.Accounts))
	}
	if store.TotalStake() != p.State.TotalStake() {
		t.Errorf("total stake mismatch: kv=%d, st=%d", store.TotalStake(), p.State.TotalStake())
	}

	// Verify point lookups for every account
	for addr, wantAcc := range p.State.Accounts {
		gotAcc := store.Get(addr)
		if gotAcc.Balance != wantAcc.Balance {
			t.Errorf("account %s balance mismatch: got %d, want %d", addr, gotAcc.Balance, wantAcc.Balance)
		}
		if gotAcc.Nonce != wantAcc.Nonce {
			t.Errorf("account %s nonce mismatch: got %d, want %d", addr, gotAcc.Nonce, wantAcc.Nonce)
		}
	}

	// Verify export roundtrip
	exported := store.ExportToState()
	expRoot := exported.Root()
	if expRoot != stRoot {
		t.Fatalf("exported state root mismatch:\n  got:  %s\n  want: %s", expRoot, stRoot)
	}
}

func TestKVStateStore_PointMutationEquivalence(t *testing.T) {
	st := chain.NewState()
	store := kvstate.NewKVStateStore(kvstate.NewMemKVStore())

	// Add 50 accounts iteratively to both and verify root at each step
	for i := 0; i < 50; i++ {
		addr := chain.Address(fmt.Sprintf("0x%040x", i+1))
		acc := chain.Account{
			Balance: uint64((i + 1) * 1000),
			Nonce:   uint64(i % 5),
		}
		st.Set(addr, acc)
		store.Set(addr, acc)

		if i%10 == 0 {
			if store.Root() != st.Root() {
				t.Fatalf("root diverged at account iteration %d", i)
			}
		}
	}

	if store.Root() != st.Root() {
		t.Fatalf("final root diverged after 50 mutations")
	}
}

func BenchmarkKVStateStore_PointLookup(b *testing.B) {
	benchCopyPath := filepath.Join(os.TempDir(), "synthos-evm-benchmark", "state-copy.json")
	raw, err := os.ReadFile(benchCopyPath)
	if err != nil {
		b.Skip("state copy not found")
	}

	var p struct {
		State *chain.State `json:"state"`
	}
	_ = json.Unmarshal(raw, &p)

	store := kvstate.NewKVStateStore(kvstate.NewMemKVStore())
	_ = store.SyncFromState(p.State)

	var targetAddr chain.Address
	for addr := range p.State.Accounts {
		targetAddr = addr
		break
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Get(targetAddr)
	}
}

func BenchmarkKVStateStore_PointUpdate(b *testing.B) {
	benchCopyPath := filepath.Join(os.TempDir(), "synthos-evm-benchmark", "state-copy.json")
	raw, err := os.ReadFile(benchCopyPath)
	if err != nil {
		b.Skip("state copy not found")
	}

	var p struct {
		State *chain.State `json:"state"`
	}
	_ = json.Unmarshal(raw, &p)

	store := kvstate.NewKVStateStore(kvstate.NewMemKVStore())
	_ = store.SyncFromState(p.State)

	var targetAddr chain.Address
	for addr := range p.State.Accounts {
		targetAddr = addr
		break
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		acc := store.Get(targetAddr)
		acc.Balance += 1
		store.Set(targetAddr, acc)
	}
}

func BenchmarkKVStateStore_RootRebuild(b *testing.B) {
	benchCopyPath := filepath.Join(os.TempDir(), "synthos-evm-benchmark", "state-copy.json")
	raw, err := os.ReadFile(benchCopyPath)
	if err != nil {
		b.Skip("state copy not found")
	}

	var p struct {
		State *chain.State `json:"state"`
	}
	_ = json.Unmarshal(raw, &p)

	store := kvstate.NewKVStateStore(kvstate.NewMemKVStore())
	_ = store.SyncFromState(p.State)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Root()
	}
}
