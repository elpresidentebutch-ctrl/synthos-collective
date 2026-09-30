package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	abcitypes "github.com/cometbft/cometbft/abci/types"

	"synthos-collective/internal/chain"
)

// These tests drive the App the way CometBFT does: InitChain, then
// FinalizeBlock + Commit per block, with CheckTx for the mempool.

const testChainID = "synthos-comet-test"
const testTxChainID = 77

var testStaking = chain.ValidatorStakingParams{
	EnabledFromHeight:   1,
	MinSelfBond:         1_000,
	UnbondingBlocks:     5,
	MaxActiveValidators: 0,
	EpochBlocks:         3,
	SlashFractionBps:    500, // 5%
	ReporterRewardBps:   1_000,
}

type account struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	addr chain.Address
}

func newAccount(t *testing.T) account {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return account{pub: pub, priv: priv, addr: chain.AddressFromPublicKey(pub)}
}

type validator struct {
	op     account
	keyHex string
	bond   uint64
}

func newValidator(t *testing.T, bond uint64) validator {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return validator{op: newAccount(t), keyHex: "0x" + hex.EncodeToString(pub), bond: bond}
}

func (v validator) consensusAddr(t *testing.T) []byte {
	t.Helper()
	a, err := ConsensusAddress(v.keyHex)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

type fixture struct {
	t    *testing.T
	vals []validator
	user account
	gen  Genesis
}

func newFixture(t *testing.T, bonds ...uint64) *fixture {
	t.Helper()
	f := &fixture{t: t, user: newAccount(t)}
	alloc := map[chain.Address]uint64{f.user.addr: 100_000}
	var gvals []GenesisValidator
	for _, b := range bonds {
		v := newValidator(t, b)
		f.vals = append(f.vals, v)
		alloc[v.op.addr] = 100_000
		gvals = append(gvals, GenesisValidator{Operator: v.op.addr, ConsensusPubKey: v.keyHex, SelfBond: b, Moniker: "val"})
	}
	f.gen = Genesis{
		Chain:      chain.Genesis{ChainID: testChainID, TxChainID: testTxChainID, Alloc: alloc},
		Staking:    testStaking,
		Validators: gvals,
	}
	return f
}

func (f *fixture) newApp(dataDir string) (*App, *abcitypes.ResponseInitChain) {
	f.t.Helper()
	a, err := New(dataDir)
	if err != nil {
		f.t.Fatal(err)
	}
	raw, err := json.Marshal(f.gen)
	if err != nil {
		f.t.Fatal(err)
	}
	res, err := a.InitChain(context.Background(), &abcitypes.RequestInitChain{ChainId: testChainID, AppStateBytes: raw, InitialHeight: 1})
	if err != nil {
		f.t.Fatalf("InitChain: %v", err)
	}
	return a, res
}

func (f *fixture) tx(from account, nonce uint64, amount, fee uint64, meta map[string]string) []byte {
	f.t.Helper()
	tx := chain.Tx{
		ChainID: testTxChainID, From: from.addr, To: from.addr, Amount: amount, Fee: fee, Nonce: nonce,
		PublicKey: "0x" + hex.EncodeToString(from.pub),
	}
	for k, v := range meta {
		tx.Metadata = append(tx.Metadata, chain.KeyValuePair{Key: k, Value: v})
	}
	if err := tx.Sign(from.priv); err != nil {
		f.t.Fatal(err)
	}
	raw, err := json.Marshal(tx)
	if err != nil {
		f.t.Fatal(err)
	}
	return raw
}

func (f *fixture) transfer(from account, to chain.Address, nonce, amount, fee uint64) []byte {
	f.t.Helper()
	tx := chain.Tx{
		ChainID: testTxChainID, From: from.addr, To: to, Amount: amount, Fee: fee, Nonce: nonce,
		PublicKey: "0x" + hex.EncodeToString(from.pub),
	}
	if err := tx.Sign(from.priv); err != nil {
		f.t.Fatal(err)
	}
	raw, _ := json.Marshal(tx)
	return raw
}

func block(t *testing.T, a *App, height int64, proposer []byte, txs [][]byte, misbehavior ...abcitypes.Misbehavior) *abcitypes.ResponseFinalizeBlock {
	t.Helper()
	res, err := a.FinalizeBlock(context.Background(), &abcitypes.RequestFinalizeBlock{
		Height: height, Txs: txs, ProposerAddress: proposer, Misbehavior: misbehavior,
	})
	if err != nil {
		t.Fatalf("FinalizeBlock %d: %v", height, err)
	}
	if _, err := a.Commit(context.Background(), &abcitypes.RequestCommit{}); err != nil {
		t.Fatalf("Commit %d: %v", height, err)
	}
	return res
}

func balance(a *App, addr chain.Address) uint64 { return a.committed.Get(addr).Balance }

func updatePowers(t *testing.T, ups []abcitypes.ValidatorUpdate) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, u := range ups {
		out["0x"+hex.EncodeToString(u.PubKey.GetEd25519())] = u.Power
	}
	return out
}

func TestInitChainSeedsValidatorsFromStake(t *testing.T) {
	f := newFixture(t, 5_000, 3_000)
	a, res := f.newApp("")
	got := updatePowers(t, res.Validators)
	if len(got) != 2 || got[f.vals[0].keyHex] != 5_000 || got[f.vals[1].keyHex] != 3_000 {
		t.Fatalf("initial validators = %v", got)
	}
	if len(res.AppHash) != 32 {
		t.Fatalf("app hash should be 32 bytes, got %d", len(res.AppHash))
	}
	// Self-bond came out of the operator's balance: supply unchanged.
	if b := balance(a, f.vals[0].op.addr); b != 100_000-5_000 {
		t.Fatalf("operator balance after genesis bond = %d", b)
	}
}

func TestGenesisRulesForCometBFT(t *testing.T) {
	cases := map[string]func(g *Genesis){
		"legacy stake consensus switch": func(g *Genesis) { g.Staking.ConsensusFromHeight = 10 },
		"staking not from block 1":      func(g *Genesis) { g.Staking.EnabledFromHeight = 5 },
		"no validators":                 func(g *Genesis) { g.Validators = nil },
		"zero epoch":                    func(g *Genesis) { g.Staking.EpochBlocks = 0 },
		"free double-signing":           func(g *Genesis) { g.Staking.SlashFractionBps = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, 5_000)
			mutate(&f.gen)
			a, _ := New("")
			raw, _ := json.Marshal(f.gen)
			if _, err := a.InitChain(context.Background(), &abcitypes.RequestInitChain{ChainId: testChainID, AppStateBytes: raw}); err == nil {
				t.Fatal("expected InitChain to reject this genesis")
			}
		})
	}
	t.Run("chain id mismatch", func(t *testing.T) {
		f := newFixture(t, 5_000)
		a, _ := New("")
		raw, _ := json.Marshal(f.gen)
		if _, err := a.InitChain(context.Background(), &abcitypes.RequestInitChain{ChainId: "other", AppStateBytes: raw}); err == nil {
			t.Fatal("expected chain id mismatch to be rejected")
		}
	})
}

func TestTransferPaysFeeToProposerOperator(t *testing.T) {
	f := newFixture(t, 5_000)
	a, _ := f.newApp("")
	v := f.vals[0]
	dest := newAccount(t).addr
	opBefore := balance(a, v.op.addr)

	res := block(t, a, 1, v.consensusAddr(t), [][]byte{f.transfer(f.user, dest, 0, 250, 100)})
	if res.TxResults[0].Code != 0 {
		t.Fatalf("transfer failed: %s", res.TxResults[0].Log)
	}
	if balance(a, dest) != 250 || balance(a, f.user.addr) != 100_000-250-100 {
		t.Fatalf("balances after transfer: dest=%d user=%d", balance(a, dest), balance(a, f.user.addr))
	}
	want := uint64(100) - uint64(100)*chain.BURN_PERCENT/100
	if got := balance(a, v.op.addr) - opBefore; got != want {
		t.Fatalf("proposer operator earned %d, want %d", got, want)
	}
}

func TestFailedTxChangesNothingAndBlockContinues(t *testing.T) {
	f := newFixture(t, 5_000)
	a, _ := f.newApp("")
	ref, _ := f.newApp("")
	dest := newAccount(t).addr
	bad := f.transfer(f.user, dest, 7, 1, 100) // wrong nonce
	good := f.transfer(f.user, dest, 0, 1, 100)
	garbage := []byte("not json")

	res := block(t, a, 1, nil, [][]byte{bad, good, garbage})
	if res.TxResults[0].Code == 0 || res.TxResults[1].Code != 0 || res.TxResults[2].Code == 0 {
		t.Fatalf("unexpected results: %+v", res.TxResults)
	}
	refRes := block(t, ref, 1, nil, [][]byte{good})
	if string(res.AppHash) != string(refRes.AppHash) {
		t.Fatal("failed transactions changed state: app hash differs from a block with only the good tx")
	}
}

func TestCheckTxQueuesConsecutiveNoncesAndResetsOnCommit(t *testing.T) {
	f := newFixture(t, 5_000)
	a, _ := f.newApp("")
	dest := newAccount(t).addr
	ctx := context.Background()
	check := func(raw []byte) uint32 {
		res, err := a.CheckTx(ctx, &abcitypes.RequestCheckTx{Tx: raw})
		if err != nil {
			t.Fatal(err)
		}
		return res.Code
	}
	tx0 := f.transfer(f.user, dest, 0, 1, 10)
	tx1 := f.transfer(f.user, dest, 1, 1, 10)
	if check(tx0) != 0 || check(tx1) != 0 {
		t.Fatal("consecutive nonces should both be admitted")
	}
	if check(tx1) == 0 {
		t.Fatal("the same nonce twice should be rejected")
	}
	wrongChain := chain.Tx{ChainID: 999, From: f.user.addr, To: dest, Amount: 1, Fee: 10, Nonce: 2, PublicKey: "0x" + hex.EncodeToString(f.user.pub)}
	_ = wrongChain.Sign(f.user.priv)
	raw, _ := json.Marshal(wrongChain)
	if check(raw) == 0 {
		t.Fatal("a transaction for another chain ID should be rejected")
	}
	// After a block with only tx0 commits, the mempool view resets to
	// committed state: tx1 is next, tx0 is stale.
	block(t, a, 1, nil, [][]byte{tx0})
	if check(tx0) == 0 {
		t.Fatal("an already-committed nonce should be rejected after Commit")
	}
	if check(tx1) != 0 {
		t.Fatal("the next nonce should be admitted after Commit")
	}
}

func TestBondJoinsValidatorSetAtEpochBoundary(t *testing.T) {
	f := newFixture(t, 5_000)
	a, _ := f.newApp("")
	joiner := newValidator(t, 4_000)
	// Anyone with SYN can join: fund the newcomer, then it bonds.
	block(t, a, 1, nil, [][]byte{f.transfer(f.user, joiner.op.addr, 0, 10_000, 10)})
	res := block(t, a, 2, nil, [][]byte{f.tx(joiner.op, 0, 4_000, 10, map[string]string{
		"type": "validator_bond", "consensus_pub_key": joiner.keyHex, "moniker": "newcomer",
	})})
	if res.TxResults[0].Code != 0 {
		t.Fatalf("bond failed: %s", res.TxResults[0].Log)
	}
	if len(res.ValidatorUpdates) != 0 {
		t.Fatalf("set changed mid-epoch: %v", updatePowers(t, res.ValidatorUpdates))
	}
	res = block(t, a, 3, nil, nil) // epoch boundary (3 % 3 == 0)
	got := updatePowers(t, res.ValidatorUpdates)
	if len(got) != 1 || got[joiner.keyHex] != 4_000 {
		t.Fatalf("epoch update = %v, want only the newcomer at 4000", got)
	}
}

func TestFullUnbondRemovesValidatorAtEpoch(t *testing.T) {
	f := newFixture(t, 5_000, 3_000)
	a, _ := f.newApp("")
	leaver := f.vals[1]
	block(t, a, 1, nil, [][]byte{f.tx(leaver.op, 0, 3_000, 10, map[string]string{"type": "validator_unbond"})})
	block(t, a, 2, nil, nil)
	res := block(t, a, 3, nil, nil)
	got := updatePowers(t, res.ValidatorUpdates)
	if p, ok := got[leaver.keyHex]; !ok || p != 0 || len(got) != 1 {
		t.Fatalf("epoch update = %v, want the leaver removed (power 0)", got)
	}
}

func TestCometMisbehaviorSlashesAndRemovesImmediately(t *testing.T) {
	f := newFixture(t, 5_000, 3_000, 2_000)
	a, _ := f.newApp("")
	cheat := f.vals[2]
	res := block(t, a, 1, nil, nil, abcitypes.Misbehavior{
		Type:      abcitypes.MisbehaviorType_DUPLICATE_VOTE,
		Validator: abcitypes.Validator{Address: cheat.consensusAddr(t), Power: 2_000},
		Height:    1,
	})
	got := updatePowers(t, res.ValidatorUpdates)
	if p, ok := got[cheat.keyHex]; !ok || p != 0 || len(got) != 1 {
		t.Fatalf("update after misbehavior = %v, want the cheater removed now (not at the epoch)", got)
	}
	rec, _ := a.committed.GetValidator(cheat.op.addr)
	if !rec.Tombstoned || rec.SelfBond != 2_000-100 {
		t.Fatalf("cheater record = %+v, want tombstoned with 5%% slashed", rec)
	}
	// Reported again later: already punished, nothing more happens.
	res = block(t, a, 2, nil, nil, abcitypes.Misbehavior{
		Type: abcitypes.MisbehaviorType_DUPLICATE_VOTE, Validator: abcitypes.Validator{Address: cheat.consensusAddr(t)}, Height: 1,
	})
	if len(res.ValidatorUpdates) != 0 {
		t.Fatalf("second report produced updates: %v", updatePowers(t, res.ValidatorUpdates))
	}
	if rec2, _ := a.committed.GetValidator(cheat.op.addr); rec2.SelfBond != rec.SelfBond {
		t.Fatal("slashed twice for one offense")
	}
}

func TestEmptyStakeSetIsNeverSentToCometBFT(t *testing.T) {
	f := newFixture(t, 5_000)
	a, _ := f.newApp("")
	v := f.vals[0]
	block(t, a, 1, nil, [][]byte{f.tx(v.op, 0, 5_000, 10, map[string]string{"type": "validator_unbond"})})
	block(t, a, 2, nil, nil)
	res := block(t, a, 3, nil, nil)
	if len(res.ValidatorUpdates) != 0 {
		t.Fatalf("would have emptied CometBFT's validator set: %v", updatePowers(t, res.ValidatorUpdates))
	}
}

// runScript applies the same mixed sequence of blocks to an app.
func runScript(t *testing.T, f *fixture, a *App, from int64) [][]byte {
	t.Helper()
	joiner := newValidatorFromSeed(t, 42)
	var hashes [][]byte
	blocks := [][][]byte{
		{f.transfer(f.user, joiner.op.addr, 0, 10_000, 10)},
		{f.tx(joiner.op, 0, 4_000, 10, map[string]string{"type": "validator_bond", "consensus_pub_key": joiner.keyHex})},
		nil,
		{f.transfer(f.user, joiner.op.addr, 1, 5, 10)},
	}
	for i, txs := range blocks {
		h := int64(i + 1)
		if h < from {
			continue
		}
		res := block(t, a, h, f.vals[0].consensusAddr(t), txs)
		hashes = append(hashes, res.AppHash)
	}
	return hashes
}

// newValidatorFromSeed makes the same validator in every call, so two
// independent apps can be fed byte-identical transactions.
func newValidatorFromSeed(t *testing.T, seed byte) validator {
	t.Helper()
	s := make([]byte, ed25519.SeedSize)
	s[0] = seed
	priv := ed25519.NewKeyFromSeed(s)
	pub := priv.Public().(ed25519.PublicKey)
	s[1] = 1
	cpriv := ed25519.NewKeyFromSeed(s)
	return validator{
		op:     account{pub: pub, priv: priv, addr: chain.AddressFromPublicKey(pub)},
		keyHex: "0x" + hex.EncodeToString(cpriv.Public().(ed25519.PublicKey)),
	}
}

func TestTwoNodesReachIdenticalAppHashes(t *testing.T) {
	f := newFixture(t, 5_000, 3_000)
	a, _ := f.newApp("")
	b, _ := f.newApp("")
	ha, hb := runScript(t, f, a, 1), runScript(t, f, b, 1)
	for i := range ha {
		if string(ha[i]) != string(hb[i]) {
			t.Fatalf("app hash differs at block %d", i+1)
		}
	}
}

func TestRestartResumesFromDisk(t *testing.T) {
	f := newFixture(t, 5_000, 3_000)
	dir := t.TempDir()
	a, _ := f.newApp(dir)
	ref, _ := f.newApp("")
	refHashes := runScript(t, f, ref, 1)

	// Run the first two blocks, "crash", reopen from disk, finish.
	joiner := newValidatorFromSeed(t, 42)
	block(t, a, 1, f.vals[0].consensusAddr(t), [][]byte{f.transfer(f.user, joiner.op.addr, 0, 10_000, 10)})
	block(t, a, 2, f.vals[0].consensusAddr(t), [][]byte{f.tx(joiner.op, 0, 4_000, 10, map[string]string{"type": "validator_bond", "consensus_pub_key": joiner.keyHex})})

	reopened, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	info, _ := reopened.Info(context.Background(), &abcitypes.RequestInfo{})
	if info.LastBlockHeight != 2 || string(info.LastBlockAppHash) != string(refHashes[1]) {
		t.Fatalf("after restart Info = height %d hash %x, want 2 / %x", info.LastBlockHeight, info.LastBlockAppHash, refHashes[1])
	}
	rest := runScript(t, f, reopened, 3)
	for i, h := range rest {
		if string(h) != string(refHashes[i+2]) {
			t.Fatalf("after restart, block %d hash differs from an app that never stopped", i+3)
		}
	}
}

func TestQueryAccountAndValidators(t *testing.T) {
	f := newFixture(t, 5_000)
	a, _ := f.newApp("")
	block(t, a, 1, nil, nil)
	res, _ := a.Query(context.Background(), &abcitypes.RequestQuery{Path: "account", Data: []byte(f.user.addr)})
	var acc chain.Account
	if res.Code != 0 || json.Unmarshal(res.Value, &acc) != nil || acc.Balance != 100_000 {
		t.Fatalf("account query: code %d value %s", res.Code, res.Value)
	}
	res, _ = a.Query(context.Background(), &abcitypes.RequestQuery{Path: "validators"})
	if res.Code != 0 || !strings.Contains(string(res.Value), f.vals[0].keyHex) {
		t.Fatalf("validators query: %s", res.Value)
	}
}
