package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"testing"
)

var testStakingParams = ValidatorStakingParams{
	EnabledFromHeight:   1,
	MinSelfBond:         1_000,
	UnbondingBlocks:     5,
	MaxActiveValidators: 0,
}

type testAccount struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	addr Address
}

func newTestAccount(t *testing.T) testAccount {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return testAccount{pub: pub, priv: priv, addr: AddressFromPublicKey(pub)}
}

func newConsensusKeyHex(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return "0x" + bytesToHex(pub)
}

func signedValidatorTx(t *testing.T, chainID uint64, a testAccount, nonce uint64, txType string, amount, fee uint64, meta map[string]string) Tx {
	t.Helper()
	md := []KeyValuePair{{Key: "type", Value: txType}}
	for k, v := range meta {
		md = append(md, KeyValuePair{Key: k, Value: v})
	}
	tx := Tx{
		ChainID:   chainID,
		From:      a.addr,
		To:        a.addr,
		Amount:    amount,
		Fee:       fee,
		Nonce:     nonce,
		PublicKey: "0x" + bytesToHex(a.pub),
		Metadata:  md,
	}
	if err := tx.Sign(a.priv); err != nil {
		t.Fatal(err)
	}
	return tx
}

func stakingTestState(t *testing.T, funded ...testAccount) *State {
	t.Helper()
	s := NewState()
	for _, a := range funded {
		s.Set(a.addr, Account{Balance: 100_000})
	}
	return s
}

// --- State-level transitions ------------------------------------------------

func TestValidatorBondRegistersAndLocksStake(t *testing.T) {
	op := newTestAccount(t)
	s := stakingTestState(t, op)
	key := newConsensusKeyHex(t)

	tx := signedValidatorTx(t, 1, op, 0, "validator_bond", 5_000, 10, map[string]string{
		"consensus_pub_key": key, "moniker": "home-node-1", "endpoint": "https://node.example",
	})
	if err := s.applyValidatorTx(tx, "validator_bond", 7, testStakingParams); err != nil {
		t.Fatalf("bond: %v", err)
	}

	acc := s.Get(op.addr)
	if acc.Balance != 100_000-5_000-10 {
		t.Fatalf("balance = %d, want %d", acc.Balance, 100_000-5_000-10)
	}
	if acc.Nonce != 1 {
		t.Fatalf("nonce = %d, want 1", acc.Nonce)
	}
	rec, ok := s.GetValidator(op.addr)
	if !ok {
		t.Fatal("validator record not created")
	}
	if rec.SelfBond != 5_000 || rec.ConsensusPubKey != key || rec.FirstBondHeight != 7 ||
		rec.Moniker != "home-node-1" || rec.Endpoint != "https://node.example" {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if got := s.TotalValidatorLocked(); got != 5_000 {
		t.Fatalf("TotalValidatorLocked = %d, want 5000", got)
	}
}

func TestValidatorBondRejections(t *testing.T) {
	op := newTestAccount(t)
	other := newTestAccount(t)
	key := newConsensusKeyHex(t)

	cases := []struct {
		name  string
		setup func(s *State)
		tx    func() Tx
		want  error
	}{
		{
			name: "below minimum self-bond",
			tx: func() Tx {
				return signedValidatorTx(t, 1, op, 0, "validator_bond", 999, 10, map[string]string{"consensus_pub_key": key})
			},
			want: ErrValidatorBondTooSmall,
		},
		{
			name: "more than balance",
			tx: func() Tx {
				return signedValidatorTx(t, 1, op, 0, "validator_bond", 100_000, 10, map[string]string{"consensus_pub_key": key})
			},
			want: ErrInsufficientFunds,
		},
		{
			name: "consensus key already used by another validator",
			setup: func(s *State) {
				tx := signedValidatorTx(t, 1, other, 0, "validator_bond", 2_000, 10, map[string]string{"consensus_pub_key": key})
				if err := s.applyValidatorTx(tx, "validator_bond", 1, testStakingParams); err != nil {
					t.Fatalf("setup bond: %v", err)
				}
			},
			tx: func() Tx {
				// Same key spelled differently (no 0x, upper case) must still collide.
				return signedValidatorTx(t, 1, op, 0, "validator_bond", 2_000, 10, map[string]string{"consensus_pub_key": upper(key[2:])})
			},
			want: ErrValidatorKeyInUse,
		},
		{
			name: "wrong asset",
			tx: func() Tx {
				tx := signedValidatorTx(t, 1, op, 0, "validator_bond", 2_000, 10, map[string]string{"consensus_pub_key": key})
				tx.AssetID = "NGOT"
				return tx
			},
			want: ErrValidatorAssetNotSYN,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := stakingTestState(t, op, other)
			if tc.setup != nil {
				tc.setup(s)
			}
			before := s.Get(op.addr)
			err := s.applyValidatorTx(tc.tx(), "validator_bond", 1, testStakingParams)
			if err != tc.want {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			after := s.Get(op.addr)
			if after.Balance != before.Balance || after.Nonce != before.Nonce {
				t.Fatalf("rejected bond changed the account: before=%+v after=%+v", before, after)
			}
			if _, ok := s.GetValidator(op.addr); ok {
				t.Fatal("rejected bond created a validator record")
			}
		})
	}

	t.Run("missing or malformed key", func(t *testing.T) {
		for _, bad := range []string{"", "0xnothex", "0x" + bytesToHex([]byte("too short"))} {
			s := stakingTestState(t, op)
			tx := signedValidatorTx(t, 1, op, 0, "validator_bond", 2_000, 10, map[string]string{"consensus_pub_key": bad})
			if err := s.applyValidatorTx(tx, "validator_bond", 1, testStakingParams); err == nil {
				t.Fatalf("key %q: expected an error", bad)
			}
		}
	})
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'f' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

func TestValidatorTopUpAndKeyMismatch(t *testing.T) {
	op := newTestAccount(t)
	s := stakingTestState(t, op)
	key := newConsensusKeyHex(t)
	bond := signedValidatorTx(t, 1, op, 0, "validator_bond", 2_000, 10, map[string]string{"consensus_pub_key": key})
	if err := s.applyValidatorTx(bond, "validator_bond", 1, testStakingParams); err != nil {
		t.Fatal(err)
	}
	// Top-up without repeating the key is fine; small top-ups are fine too
	// because the total, not the increment, must meet the minimum.
	topUp := signedValidatorTx(t, 1, op, 1, "validator_bond", 1, 10, nil)
	if err := s.applyValidatorTx(topUp, "validator_bond", 2, testStakingParams); err != nil {
		t.Fatalf("top-up: %v", err)
	}
	if rec, _ := s.GetValidator(op.addr); rec.SelfBond != 2_001 || rec.FirstBondHeight != 1 {
		t.Fatalf("after top-up: %+v", rec)
	}
	// A different key can't be swapped in.
	swap := signedValidatorTx(t, 1, op, 2, "validator_bond", 1, 10, map[string]string{"consensus_pub_key": newConsensusKeyHex(t)})
	if err := s.applyValidatorTx(swap, "validator_bond", 3, testStakingParams); err != ErrValidatorKeyMismatch {
		t.Fatalf("got %v, want ErrValidatorKeyMismatch", err)
	}
}

func TestValidatorUnbondAndWithdrawLifecycle(t *testing.T) {
	op := newTestAccount(t)
	s := stakingTestState(t, op)
	key := newConsensusKeyHex(t)
	nonce := uint64(0)
	apply := func(txType string, amount uint64, height uint64) error {
		tx := signedValidatorTx(t, 1, op, nonce, txType, amount, 10, map[string]string{"consensus_pub_key": key})
		err := s.applyValidatorTx(tx, txType, height, testStakingParams)
		if err == nil {
			nonce++
		}
		return err
	}

	if err := apply("validator_bond", 3_000, 10); err != nil {
		t.Fatal(err)
	}
	// Partial unbond that would leave 500 (< min 1000) is refused.
	if err := apply("validator_unbond", 2_500, 11); err != ErrValidatorBondTooSmall {
		t.Fatalf("got %v, want ErrValidatorBondTooSmall", err)
	}
	// Unbonding more than bonded is refused.
	if err := apply("validator_unbond", 3_001, 11); err != ErrValidatorInsufficientBond {
		t.Fatalf("got %v, want ErrValidatorInsufficientBond", err)
	}
	// Partial unbond leaving exactly the minimum is fine.
	if err := apply("validator_unbond", 2_000, 11); err != nil {
		t.Fatalf("partial unbond: %v", err)
	}
	rec, _ := s.GetValidator(op.addr)
	if rec.SelfBond != 1_000 || len(rec.Unbonding) != 1 || rec.Unbonding[0].CompleteAtHeight != 16 {
		t.Fatalf("after partial unbond: %+v", rec)
	}
	// Unbonding SYN is still locked: counted, not spendable.
	if s.TotalValidatorLocked() != 3_000 {
		t.Fatalf("TotalValidatorLocked = %d, want 3000", s.TotalValidatorLocked())
	}

	// Full exit.
	if err := apply("validator_unbond", 1_000, 12); err != nil {
		t.Fatalf("full unbond: %v", err)
	}
	// Nothing has matured at height 15.
	if err := apply("validator_withdraw", 1, 15); err != ErrValidatorNothingMatured {
		t.Fatalf("got %v, want ErrValidatorNothingMatured", err)
	}
	balBefore := s.Get(op.addr).Balance
	// At 16 only the first entry (2000) has matured.
	if err := apply("validator_withdraw", 1, 16); err != nil {
		t.Fatalf("withdraw at 16: %v", err)
	}
	if got := s.Get(op.addr).Balance; got != balBefore+2_000-10 {
		t.Fatalf("balance after first withdraw = %d, want %d", got, balBefore+2_000-10)
	}
	if _, ok := s.GetValidator(op.addr); !ok {
		t.Fatal("record deleted while an unbonding entry is still pending")
	}
	// At 17 the second matures; the record then has nothing left and is removed.
	if err := apply("validator_withdraw", 1, 17); err != nil {
		t.Fatalf("withdraw at 17: %v", err)
	}
	if _, ok := s.GetValidator(op.addr); ok {
		t.Fatal("record should be removed once fully unbonded and withdrawn")
	}
	if s.TotalValidatorLocked() != 0 {
		t.Fatalf("TotalValidatorLocked = %d after exit, want 0", s.TotalValidatorLocked())
	}
	// Every SYN came back except fees: 100_000 - 5 successful txs * 10
	// (bond, two unbonds, two withdrawals; rejected txs cost nothing).
	if got := s.Get(op.addr).Balance; got != 100_000-5*10 {
		t.Fatalf("final balance = %d, want %d", got, 100_000-5*10)
	}
}

func TestValidatorUnbondingEntriesAreCapped(t *testing.T) {
	op := newTestAccount(t)
	s := stakingTestState(t, op)
	params := testStakingParams
	params.MinSelfBond = 1
	bond := signedValidatorTx(t, 1, op, 0, "validator_bond", 100, 1, map[string]string{"consensus_pub_key": newConsensusKeyHex(t)})
	if err := s.applyValidatorTx(bond, "validator_bond", 1, params); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxUnbondingEntries; i++ {
		tx := signedValidatorTx(t, 1, op, uint64(i+1), "validator_unbond", 1, 1, nil)
		if err := s.applyValidatorTx(tx, "validator_unbond", 2, params); err != nil {
			t.Fatalf("unbond %d: %v", i, err)
		}
	}
	tx := signedValidatorTx(t, 1, op, uint64(MaxUnbondingEntries+1), "validator_unbond", 1, 1, nil)
	if err := s.applyValidatorTx(tx, "validator_unbond", 2, params); err != ErrValidatorTooManyUnbonding {
		t.Fatalf("got %v, want ErrValidatorTooManyUnbonding", err)
	}
}

// --- Active set -------------------------------------------------------------

func TestActiveValidatorSetIsDeterministicAndOrdered(t *testing.T) {
	s := NewState()
	s.Validators = map[Address]ValidatorRecord{
		"0xcc": {Operator: "0xcc", ConsensusPubKey: "0x03", SelfBond: 5_000},
		"0xaa": {Operator: "0xaa", ConsensusPubKey: "0x01", SelfBond: 5_000},
		"0xbb": {Operator: "0xbb", ConsensusPubKey: "0x02", SelfBond: 9_000},
		"0xdd": {Operator: "0xdd", ConsensusPubKey: "0x04", SelfBond: 999},                                                                   // below minimum
		"0xee": {Operator: "0xee", ConsensusPubKey: "0x05", SelfBond: 0, Unbonding: []UnbondingEntry{{Amount: 5_000, CompleteAtHeight: 99}}}, // exiting
	}

	got := s.ActiveValidatorSet(testStakingParams)
	want := []Address{"0xbb", "0xaa", "0xcc"} // highest bond first, ties by address
	if len(got) != len(want) {
		t.Fatalf("active set = %+v, want operators %v", got, want)
	}
	for i := range want {
		if got[i].Operator != want[i] {
			t.Fatalf("position %d: got %s, want %s (full: %+v)", i, got[i].Operator, want[i], got)
		}
	}
	if got[0].Power != 9_000 {
		t.Fatalf("power = %d, want 9000", got[0].Power)
	}

	capped := testStakingParams
	capped.MaxActiveValidators = 2
	if c := s.ActiveValidatorSet(capped); len(c) != 2 || c[0].Operator != "0xbb" || c[1].Operator != "0xaa" {
		t.Fatalf("capped set = %+v", c)
	}

	// Same state, computed many times (map iteration order varies): same answer.
	for i := 0; i < 50; i++ {
		again := s.ActiveValidatorSet(testStakingParams)
		for j := range want {
			if again[j].Operator != want[j] {
				t.Fatalf("run %d produced a different order: %+v", i, again)
			}
		}
	}
}

// --- State root and clone ---------------------------------------------------

// TestStateRootUnchangedForRealGenesis pins the state root of the real
// config/genesis.json, as computed by the code before validator staking
// existed. An empty validator registry must not change any root, or every
// existing block would stop validating.
func TestStateRootUnchangedForRealGenesis(t *testing.T) {
	const rootBeforeValidatorStaking = "0xa40c015e2800877ec9db67c55a5316b5e8b43dcf967fcbf3ade78aa7bc44a2ff"
	raw, err := os.ReadFile("../../config/genesis.json")
	if err != nil {
		t.Skipf("real genesis not available: %v", err)
	}
	var g Genesis
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	s, err := g.ToState()
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Root(); got != rootBeforeValidatorStaking {
		t.Fatalf("genesis state root changed: got %s, want %s", got, rootBeforeValidatorStaking)
	}
	s.Validators = nil // an old snapshot loads with no map at all
	if got := s.Root(); got != rootBeforeValidatorStaking {
		t.Fatalf("nil validator map changed the root: got %s", got)
	}
}

func TestStateRootCommitsToValidatorRegistry(t *testing.T) {
	op := newTestAccount(t)
	s := stakingTestState(t, op)
	emptyRoot := s.Root()

	tx := signedValidatorTx(t, 1, op, 0, "validator_bond", 2_000, 10, map[string]string{"consensus_pub_key": newConsensusKeyHex(t)})
	if err := s.applyValidatorTx(tx, "validator_bond", 1, testStakingParams); err != nil {
		t.Fatal(err)
	}
	bonded := s.Root()
	if bonded == emptyRoot {
		t.Fatal("root did not change after bonding")
	}

	// Two states with identical balances but a different validator record
	// must have different roots, or the registry isn't really committed.
	alt := s.Clone()
	rec := alt.Validators[op.addr]
	rec.Moniker = "different"
	alt.Validators[op.addr] = rec
	if alt.Root() == bonded {
		t.Fatal("root does not commit to validator record contents")
	}
}

func TestStateCloneDeepCopiesValidators(t *testing.T) {
	s := NewState()
	s.Validators["0xaa"] = ValidatorRecord{Operator: "0xaa", SelfBond: 1, Unbonding: []UnbondingEntry{{Amount: 5, CompleteAtHeight: 9}}}
	c := s.Clone()
	rec := c.Validators["0xaa"]
	rec.Unbonding[0].Amount = 777
	c.Validators["0xaa"] = rec
	if s.Validators["0xaa"].Unbonding[0].Amount != 5 {
		t.Fatal("Clone aliased a validator's unbonding slice")
	}
}

// --- Through the real chain pipeline ---------------------------------------

func finalizeNextBlock(t *testing.T, c *Chain, proposerID string, proposerPriv ed25519.PrivateKey) *Block {
	t.Helper()
	b, err := c.BuildBlock(proposerID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	b.ProposerSignature = "0x" + bytesToHex(ed25519.Sign(proposerPriv, []byte(b.Hash)))
	b.QuorumSignatures = map[string]string{
		proposerID: "0x" + bytesToHex(ed25519.Sign(proposerPriv, b.QuorumApprovalMessage())),
	}
	if err := c.FinalizeBlock(b); err != nil {
		t.Fatalf("FinalizeBlock: %v", err)
	}
	return b
}

func newStakingTestChain(t *testing.T, op testAccount, proposerPub ed25519.PublicKey, params ValidatorStakingParams) *Chain {
	t.Helper()
	c, err := NewChain(Genesis{ChainID: "staking-test", Alloc: map[Address]uint64{op.addr: 100_000}})
	if err != nil {
		t.Fatal(err)
	}
	c.SetValidatorSet(map[string]ed25519.PublicKey{"proposer": proposerPub}, 1)
	c.SetValidatorStakingParams(params)
	return c
}

// TestValidatorBondThroughBlockPipeline submits a real signed bond through
// SubmitTx -> BuildBlock -> FinalizeBlock, then replays the same block on a
// second node with the same params (it must agree, state root included) and
// on a node with staking disabled (it must reject the block -- which is why
// the params have to be identical on every node).
func TestValidatorBondThroughBlockPipeline(t *testing.T) {
	op := newTestAccount(t)
	proposerPub, proposerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	nodeA := newStakingTestChain(t, op, proposerPub, testStakingParams)
	bond := signedValidatorTx(t, nodeA.TransactionChainID(), op, 0, "validator_bond", 5_000, 10,
		map[string]string{"consensus_pub_key": newConsensusKeyHex(t), "moniker": "anyone"})
	if err := nodeA.SubmitTx(bond); err != nil {
		t.Fatalf("SubmitTx: %v", err)
	}
	// SimulateTx must use the same staking path the block will: on the
	// staking path the bond locks 5000 instead of sending it to tx.To.
	sim, err := nodeA.SimulateTx(bond)
	if err != nil {
		t.Fatalf("SimulateTx: %v %+v", err, sim)
	}
	if sim.FromBalanceAfter != 100_000-5_000-10 {
		t.Fatalf("SimulateTx balance after = %d, want %d (staking path not used?)", sim.FromBalanceAfter, 100_000-5_000-10)
	}
	block := finalizeNextBlock(t, nodeA, "proposer", proposerPriv)
	if len(block.Tx) != 1 {
		t.Fatalf("expected the bond in the block, got %d txs", len(block.Tx))
	}

	active := nodeA.ActiveValidatorSet()
	if len(active) != 1 || active[0].Operator != op.addr || active[0].Power != 5_000 {
		t.Fatalf("active set after bond = %+v", active)
	}

	nodeB := newStakingTestChain(t, op, proposerPub, testStakingParams)
	replay := *block
	if err := nodeB.FinalizeBlock(&replay); err != nil {
		t.Fatalf("a node with identical params rejected the block: %v", err)
	}
	if nodeB.State.Root() != nodeA.State.Root() {
		t.Fatal("two nodes with identical params disagree on the state root")
	}

	nodeC := newStakingTestChain(t, op, proposerPub, ValidatorStakingParams{})
	replay2 := *block
	if err := nodeC.FinalizeBlock(&replay2); err == nil {
		t.Fatal("a node with staking disabled accepted a block whose bond it applies differently")
	}
}

// TestValidatorTxTypesUnchangedBeforeActivation proves the code is inert
// until EnabledFromHeight: before it, a validator_bond-typed transaction is
// applied exactly as State.ApplyTx always applied it.
func TestValidatorTxTypesUnchangedBeforeActivation(t *testing.T) {
	op := newTestAccount(t)
	proposerPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	params := testStakingParams
	params.EnabledFromHeight = 100
	c := newStakingTestChain(t, op, proposerPub, params)
	tx := signedValidatorTx(t, c.TransactionChainID(), op, 0, "validator_bond", 5_000, 10,
		map[string]string{"consensus_pub_key": newConsensusKeyHex(t)})

	viaChain := c.State.Clone()
	c.mu.RLock()
	errChain := c.applyTxLocked(viaChain, tx, 99)
	c.mu.RUnlock()
	legacy := c.State.Clone()
	errLegacy := legacy.ApplyTx(tx)

	if (errChain == nil) != (errLegacy == nil) {
		t.Fatalf("error mismatch before activation: chain=%v legacy=%v", errChain, errLegacy)
	}
	if viaChain.Root() != legacy.Root() {
		t.Fatal("before activation, a validator_bond tx applied differently from the legacy path")
	}
	if len(viaChain.Validators) != 0 {
		t.Fatal("a validator was registered before activation")
	}
}

func TestValidatorStakingParamsValidate(t *testing.T) {
	if err := (ValidatorStakingParams{}).Validate(); err != nil {
		t.Fatalf("disabled params should be valid: %v", err)
	}
	if err := (ValidatorStakingParams{EnabledFromHeight: 1, UnbondingBlocks: 5}).Validate(); err == nil {
		t.Fatal("enabled with zero minimum bond should be rejected")
	}
	if err := (ValidatorStakingParams{EnabledFromHeight: 1, MinSelfBond: 5}).Validate(); err == nil {
		t.Fatal("enabled with zero unbonding period should be rejected")
	}
	if err := testStakingParams.Validate(); err != nil {
		t.Fatalf("test params should be valid: %v", err)
	}
}
