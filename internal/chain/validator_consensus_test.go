package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strconv"
	"testing"
)

// Step 2 tests: stake-weighted block authorization, epoch snapshots and
// double-sign slashing, exercised through the real block pipeline.

var consensusTestParams = ValidatorStakingParams{
	EnabledFromHeight:   1,
	MinSelfBond:         1_000,
	UnbondingBlocks:     5,
	ConsensusFromHeight: 4,
	EpochBlocks:         3,
	SlashFractionBps:    500,   // 5%
	ReporterRewardBps:   1_000, // 10% of the slash
}

type testValidator struct {
	op      testAccount
	keyHex  string
	keyPriv ed25519.PrivateKey
	bond    uint64
}

func newTestValidator(t *testing.T, bond uint64) testValidator {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return testValidator{op: newTestAccount(t), keyHex: "0x" + bytesToHex(pub), keyPriv: priv, bond: bond}
}

type consensusHarness struct {
	t          *testing.T
	chain      *Chain
	replica    *Chain // a second, independent node fed the same blocks
	legacyPriv ed25519.PrivateKey
	vals       []testValidator
	reporter   testAccount
}

// newConsensusHarness builds two nodes with identical config. Blocks 1-3
// are authorized by the legacy roster; block 1 carries every validator's
// bond; the snapshot taken at the end of block 3 authorizes blocks 4-6.
func newConsensusHarness(t *testing.T, bonds ...uint64) *consensusHarness {
	t.Helper()
	legacyPub, legacyPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := &consensusHarness{t: t, legacyPriv: legacyPriv, reporter: newTestAccount(t)}
	alloc := map[Address]uint64{h.reporter.addr: 100_000}
	for _, b := range bonds {
		v := newTestValidator(t, b)
		h.vals = append(h.vals, v)
		alloc[v.op.addr] = 100_000
	}
	mk := func() *Chain {
		c, err := NewChain(Genesis{ChainID: "stake-consensus-test", Alloc: alloc})
		if err != nil {
			t.Fatal(err)
		}
		c.SetValidatorSet(map[string]ed25519.PublicKey{"proposer": legacyPub}, 1)
		c.SetValidatorStakingParams(consensusTestParams)
		return c
	}
	h.chain, h.replica = mk(), mk()
	return h
}

func (h *consensusHarness) submit(tx Tx) {
	h.t.Helper()
	if err := h.chain.SubmitTx(tx); err != nil {
		h.t.Fatalf("SubmitTx: %v", err)
	}
}

func (h *consensusHarness) bondAll() {
	h.t.Helper()
	for _, v := range h.vals {
		h.submit(signedValidatorTx(h.t, h.chain.TransactionChainID(), v.op, 0, "validator_bond", v.bond, 10,
			map[string]string{"consensus_pub_key": v.keyHex}))
	}
}

// replicate feeds a finalized block to the second node and checks it
// reaches the identical state.
func (h *consensusHarness) replicate(b *Block) {
	h.t.Helper()
	cp := *b
	if err := h.replica.FinalizeBlock(&cp); err != nil {
		h.t.Fatalf("replica rejected block %d: %v", b.Header.Height, err)
	}
	if h.replica.State.Root() != h.chain.State.Root() {
		h.t.Fatalf("replica state root differs after block %d", b.Header.Height)
	}
}

func (h *consensusHarness) legacyBlock() *Block {
	h.t.Helper()
	b := finalizeNextBlock(h.t, h.chain, "proposer", h.legacyPriv)
	h.replicate(b)
	return b
}

// buildStake builds the next block proposed by proposer and approved by
// signers, without finalizing it.
func (h *consensusHarness) buildStake(proposer testValidator, signers ...testValidator) *Block {
	h.t.Helper()
	b, err := h.chain.BuildBlock(proposer.keyHex, "", 50)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := b.ComputeHash(); err != nil {
		h.t.Fatal(err)
	}
	id := h.chain.ChainID
	b.ProposerSignature = "0x" + bytesToHex(ed25519.Sign(proposer.keyPriv, StakeProposalMessage(id, b.Header.Height, b.Hash)))
	b.QuorumSignatures = map[string]string{}
	for _, s := range signers {
		b.QuorumSignatures[s.keyHex] = "0x" + bytesToHex(ed25519.Sign(s.keyPriv, StakeApprovalMessage(id, b.Header.Height, b.Hash)))
	}
	return b
}

func (h *consensusHarness) stakeBlock(proposer testValidator, signers ...testValidator) *Block {
	h.t.Helper()
	b := h.buildStake(proposer, signers...)
	if err := h.chain.FinalizeBlock(b); err != nil {
		h.t.Fatalf("FinalizeBlock at %d: %v", b.Header.Height, err)
	}
	h.replicate(b)
	return b
}

// runToStakeConsensus bonds in block 1 and produces legacy blocks 2-3.
func (h *consensusHarness) runToStakeConsensus() {
	h.t.Helper()
	h.bondAll()
	h.legacyBlock()
	h.legacyBlock()
	h.legacyBlock()
}

func TestStakeSnapshotTakenAtEpochBoundary(t *testing.T) {
	h := newConsensusHarness(t, 5_000, 3_000, 2_000)
	h.bondAll()
	h.legacyBlock()
	h.legacyBlock()
	if got := h.chain.State.AuthorizingValidatorSet(); len(got) != 0 {
		t.Fatalf("snapshot exists before the epoch boundary: %+v", got)
	}
	if h.chain.StakeConsensusActiveAt(3) {
		t.Fatal("stake consensus active before consensus_from_height")
	}
	h.legacyBlock() // end of block 3 = snapshot for blocks 4-6
	set := h.chain.State.AuthorizingValidatorSet()
	if len(set) != 3 || set[0].Power != 5_000 || set[1].Power != 3_000 || set[2].Power != 2_000 {
		t.Fatalf("snapshot = %+v", set)
	}
	if !h.chain.StakeConsensusActiveAt(4) {
		t.Fatal("stake consensus should be active at block 4")
	}
}

func TestStakeAuthorizationNeedsMoreThanTwoThirdsOfStake(t *testing.T) {
	h := newConsensusHarness(t, 5_000, 3_000, 2_000) // total 10,000
	a, b, c := h.vals[0], h.vals[1], h.vals[2]
	h.runToStakeConsensus()

	for _, tc := range []struct {
		name    string
		signers []testValidator
		ok      bool
	}{
		{"A alone (50%)", []testValidator{a}, false},
		{"B+C (50%)", []testValidator{b, c}, false},
		{"A+C (70%)", []testValidator{a, c}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blk := h.buildStake(a, tc.signers...)
			err := h.chain.ValidateBlock(blk)
			if tc.ok && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected rejection for insufficient stake")
			}
		})
	}

	// A's signature listed twice under two spellings of its key must count
	// once: A holds 50%, which would wrongly become 100% if counted twice.
	blk := h.buildStake(a, a)
	blk.QuorumSignatures["0X"+upper(a.keyHex[2:])] = blk.QuorumSignatures[a.keyHex]
	if err := h.chain.ValidateBlock(blk); err == nil {
		t.Fatal("one validator's signature was counted twice")
	}

	h.stakeBlock(a, a, c) // actually finalize block 4 on both nodes
	if h.chain.Height() != 4 {
		t.Fatalf("height = %d, want 4", h.chain.Height())
	}
}

func TestStakeAuthorizationExactlyTwoThirdsIsNotEnough(t *testing.T) {
	h := newConsensusHarness(t, 3_000, 3_000, 3_000) // total 9,000
	h.runToStakeConsensus()
	blk := h.buildStake(h.vals[0], h.vals[0], h.vals[1]) // 6,000 = exactly 2/3
	if err := h.chain.ValidateBlock(blk); err == nil {
		t.Fatal("exactly 2/3 of stake must not be enough")
	}
	blk = h.buildStake(h.vals[0], h.vals[0], h.vals[1], h.vals[2])
	if err := h.chain.ValidateBlock(blk); err != nil {
		t.Fatalf("all three should be enough: %v", err)
	}
}

func TestStakeModeRejectsLegacyAndUnknownProposers(t *testing.T) {
	h := newConsensusHarness(t, 5_000, 3_000, 2_000)
	a := h.vals[0]
	h.runToStakeConsensus()

	// The old roster key can no longer authorize a block.
	legacy, err := h.chain.BuildBlock("proposer", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	legacy.ProposerSignature = "0x" + bytesToHex(ed25519.Sign(h.legacyPriv, []byte(legacy.Hash)))
	legacy.QuorumSignatures = map[string]string{"proposer": "0x" + bytesToHex(ed25519.Sign(h.legacyPriv, legacy.QuorumApprovalMessage()))}
	if err := h.chain.ValidateBlock(legacy); err == nil {
		t.Fatal("legacy roster authorized a block in stake mode")
	}

	// A key that never bonded can't propose, even with enough approvals.
	outsider := newTestValidator(t, 0)
	blk := h.buildStake(outsider, h.vals...)
	if err := h.chain.ValidateBlock(blk); err == nil {
		t.Fatal("an unbonded key proposed a block")
	}

	// A proposer signature over the legacy message (not the stake one) fails.
	blk = h.buildStake(a, h.vals...)
	blk.ProposerSignature = "0x" + bytesToHex(ed25519.Sign(a.keyPriv, []byte(blk.Hash)))
	if err := h.chain.ValidateBlock(blk); err == nil {
		t.Fatal("legacy-format proposer signature accepted in stake mode")
	}

	// Approvals signed for a different chain don't count.
	blk = h.buildStake(a)
	for _, v := range h.vals {
		blk.QuorumSignatures[v.keyHex] = "0x" + bytesToHex(ed25519.Sign(v.keyPriv, StakeApprovalMessage("some-other-chain", blk.Header.Height, blk.Hash)))
	}
	if err := h.chain.ValidateBlock(blk); err == nil {
		t.Fatal("approvals from another chain ID were accepted")
	}
}

func TestStakeModeProposerRewardGoesToOperator(t *testing.T) {
	h := newConsensusHarness(t, 5_000, 3_000, 2_000)
	a := h.vals[0]
	h.runToStakeConsensus()

	// A fee-paying transfer inside a stake-mode block.
	transfer := Tx{ChainID: h.chain.TransactionChainID(), From: h.reporter.addr, To: h.reporter.addr,
		Amount: 1, Fee: 100, Nonce: 0, PublicKey: "0x" + bytesToHex(h.reporter.pub)}
	if err := transfer.Sign(h.reporter.priv); err != nil {
		t.Fatal(err)
	}
	h.submit(transfer)
	before := h.chain.State.Get(a.op.addr).Balance
	h.stakeBlock(a, h.vals...)
	gained := h.chain.State.Get(a.op.addr).Balance - before
	want := uint64(100) - uint64(100)*BURN_PERCENT/100
	if gained != want {
		t.Fatalf("proposer operator gained %d, want %d (fee minus burn)", gained, want)
	}
}

func TestStakeSetChangesOnlyAtEpochBoundaries(t *testing.T) {
	h := newConsensusHarness(t, 5_000, 3_000, 2_000)
	a, c := h.vals[0], h.vals[2]
	h.runToStakeConsensus()

	// C bonds a lot more during the epoch (block 4).
	h.submit(signedValidatorTx(t, h.chain.TransactionChainID(), c.op, 1, "validator_bond", 20_000, 10, nil))
	h.stakeBlock(a, h.vals...) // block 4
	if got := h.chain.State.AuthorizingValidatorSet(); got[0].Operator != a.op.addr {
		t.Fatalf("authorizing set changed mid-epoch: %+v", got)
	}
	if got := h.chain.ActiveValidatorSet(); got[0].Operator != c.op.addr {
		t.Fatalf("active (implied) set should already show C first: %+v", got)
	}
	h.stakeBlock(a, h.vals...) // block 5
	h.stakeBlock(a, h.vals...) // block 6: end of epoch, new snapshot
	set := h.chain.State.AuthorizingValidatorSet()
	if set[0].Operator != c.op.addr || set[0].Power != 22_000 {
		t.Fatalf("snapshot after epoch boundary = %+v", set)
	}
	// From block 7, C alone holds 22,000 of 30,000 (73%) -- enough by itself.
	h.stakeBlock(c, c)
}

// doubleSignEvidence has v sign two different hashes at height and returns
// an evidence transaction from reporter.
func doubleSignEvidence(t *testing.T, chainID string, txChainID uint64, reporter testAccount, nonce uint64, v testValidator, height uint64, kind string) Tx {
	t.Helper()
	hashA := "0x" + bytesToHex(randomBytes(t, 32))
	hashB := "0x" + bytesToHex(randomBytes(t, 32))
	msg := StakeApprovalMessage
	if kind == "propose" {
		msg = StakeProposalMessage
	}
	return signedValidatorTx(t, txChainID, reporter, nonce, "validator_evidence", 1, 10, map[string]string{
		"consensus_pub_key": v.keyHex,
		"kind":              kind,
		"evidence_height":   strconv.FormatUint(height, 10),
		"hash_a":            hashA,
		"sig_a":             "0x" + bytesToHex(ed25519.Sign(v.keyPriv, msg(chainID, height, hashA))),
		"hash_b":            hashB,
		"sig_b":             "0x" + bytesToHex(ed25519.Sign(v.keyPriv, msg(chainID, height, hashB))),
	})
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDoubleSignEvidenceSlashesTombstonesAndRewardsReporter(t *testing.T) {
	h := newConsensusHarness(t, 5_000, 3_000, 2_000)
	a, c := h.vals[0], h.vals[2]
	h.runToStakeConsensus()
	h.stakeBlock(a, h.vals...) // block 4: C approves normally...

	// ...and C also signed a conflicting block at height 4.
	reporterBefore := h.chain.State.Get(h.reporter.addr).Balance
	h.submit(doubleSignEvidence(t, h.chain.ChainID, h.chain.TransactionChainID(), h.reporter, 0, c, 4, "approve"))
	h.stakeBlock(a, h.vals...) // block 5 carries the evidence; replica applies it identically

	rec, ok := h.chain.State.GetValidator(c.op.addr)
	if !ok || !rec.Tombstoned {
		t.Fatalf("offender not tombstoned: %+v", rec)
	}
	if rec.SelfBond != 2_000-100 {
		t.Fatalf("self-bond after 5%% slash = %d, want 1900", rec.SelfBond)
	}
	// Reporter pays the fee (10) and receives 10% of the 100 slashed.
	if got := h.chain.State.Get(h.reporter.addr).Balance; got != reporterBefore-10+10 {
		t.Fatalf("reporter balance = %d, want %d", got, reporterBefore)
	}
	for _, v := range h.chain.State.AuthorizingValidatorSet() {
		if v.Operator == c.op.addr {
			t.Fatal("offender still in the authorizing set within the same epoch")
		}
	}
	for _, v := range h.chain.ActiveValidatorSet() {
		if v.Operator == c.op.addr {
			t.Fatal("tombstoned validator still in the active set")
		}
	}

	// It can't bond back in, can't be slashed twice, but can leave with
	// what's left.
	s := h.chain.State.Clone()
	bond := signedValidatorTx(t, 1, c.op, 1, "validator_bond", 5_000, 10, nil)
	if err := s.applyValidatorTx(bond, "validator_bond", 6, consensusTestParams); err != ErrValidatorTombstoned {
		t.Fatalf("tombstoned bond: got %v", err)
	}
	again := doubleSignEvidence(t, h.chain.ChainID, 1, h.reporter, 1, c, 4, "propose")
	if err := s.applyValidatorTxCtx(again, "validator_evidence", validatorTxContext{height: 6, params: consensusTestParams, chainID: h.chain.ChainID}); err != ErrValidatorTombstoned {
		t.Fatalf("second slash: got %v", err)
	}
	unbond := signedValidatorTx(t, 1, c.op, 1, "validator_unbond", 1_900, 10, nil)
	if err := s.applyValidatorTx(unbond, "validator_unbond", 6, consensusTestParams); err != nil {
		t.Fatalf("tombstoned validator should be able to unbond: %v", err)
	}
	if _, ok := s.GetValidator(c.op.addr); !ok {
		t.Fatal("tombstoned record must be kept even at zero stake")
	}
}

func TestDoubleSignEvidenceSlashesUnbondingStakeToo(t *testing.T) {
	v := newTestValidator(t, 4_000)
	reporter := newTestAccount(t)
	s := stakingTestState(t, v.op, reporter)
	ctx := validatorTxContext{height: 10, params: consensusTestParams, chainID: "c"}
	bond := signedValidatorTx(t, 1, v.op, 0, "validator_bond", 4_000, 10, map[string]string{"consensus_pub_key": v.keyHex})
	if err := s.applyValidatorTxCtx(bond, "validator_bond", ctx); err != nil {
		t.Fatal(err)
	}
	// Trying to escape by unbonding right after misbehaving doesn't help.
	unbond := signedValidatorTx(t, 1, v.op, 1, "validator_unbond", 2_000, 10, nil)
	if err := s.applyValidatorTxCtx(unbond, "validator_unbond", ctx); err != nil {
		t.Fatal(err)
	}
	ev := doubleSignEvidence(t, "c", 1, reporter, 0, v, 9, "propose")
	if err := s.applyValidatorTxCtx(ev, "validator_evidence", ctx); err != nil {
		t.Fatalf("evidence: %v", err)
	}
	rec, _ := s.GetValidator(v.op.addr)
	if rec.SelfBond != 1_900 || rec.Unbonding[0].Amount != 1_900 {
		t.Fatalf("both bonded and unbonding stake should lose 5%%: %+v", rec)
	}
}

func TestInvalidDoubleSignEvidenceIsRejected(t *testing.T) {
	v := newTestValidator(t, 4_000)
	reporter := newTestAccount(t)
	fresh := func() *State {
		s := stakingTestState(t, v.op, reporter)
		bond := signedValidatorTx(t, 1, v.op, 0, "validator_bond", 4_000, 10, map[string]string{"consensus_pub_key": v.keyHex})
		if err := s.applyValidatorTx(bond, "validator_bond", 1, consensusTestParams); err != nil {
			t.Fatal(err)
		}
		return s
	}
	ctx := validatorTxContext{height: 10, params: consensusTestParams, chainID: "mainnet"}
	sign := func(chainID string, height uint64, hash string) string {
		return "0x" + bytesToHex(ed25519.Sign(v.keyPriv, StakeApprovalMessage(chainID, height, hash)))
	}
	hashA := "0x" + bytesToHex(randomBytes(t, 32))
	hashB := "0x" + bytesToHex(randomBytes(t, 32))
	base := map[string]string{
		"consensus_pub_key": v.keyHex, "kind": "approve", "evidence_height": "9",
		"hash_a": hashA, "sig_a": sign("mainnet", 9, hashA),
		"hash_b": hashB, "sig_b": sign("mainnet", 9, hashB),
	}
	with := func(kv ...string) map[string]string {
		m := map[string]string{}
		for kk, vv := range base {
			m[kk] = vv
		}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	upperA := "0x" + upper(hashA[2:])

	// Each case breaks exactly one rule; its signatures are otherwise
	// genuine, so only the rule under test can reject it.
	cases := map[string]map[string]string{
		"same block twice":             with("hash_b", hashA, "sig_b", sign("mainnet", 9, hashA)),
		"same block, different case":   with("hash_b", upperA, "sig_b", sign("mainnet", 9, upperA)),
		"signed on a different chain":  with("sig_b", sign("testnet", 9, hashB)),
		"signatures for other heights": with("sig_b", sign("mainnet", 8, hashB)),
		"before stake consensus":       with("evidence_height", "2", "sig_a", sign("mainnet", 2, hashA), "sig_b", sign("mainnet", 2, hashB)),
		"future height":                with("evidence_height", "11", "sig_a", sign("mainnet", 11, hashA), "sig_b", sign("mainnet", 11, hashB)),
		"unknown kind":                 with("kind", "vote"),
	}
	for name, meta := range cases {
		t.Run(name, func(t *testing.T) {
			s := fresh()
			ev := signedValidatorTx(t, 1, reporter, 0, "validator_evidence", 1, 10, meta)
			err := s.applyValidatorTxCtx(ev, "validator_evidence", ctx)
			if err == nil {
				t.Fatal("invalid evidence accepted")
			}
			if rec, _ := s.GetValidator(v.op.addr); rec.Tombstoned || rec.SelfBond != 4_000 {
				t.Fatalf("rejected evidence changed the validator: %+v", rec)
			}
		})
	}

	t.Run("not active before consensus_from_height", func(t *testing.T) {
		s := fresh()
		ev := signedValidatorTx(t, 1, reporter, 0, "validator_evidence", 1, 10, base)
		early := validatorTxContext{height: 3, params: consensusTestParams, chainID: "mainnet"}
		if err := s.applyValidatorTxCtx(ev, "validator_evidence", early); !errors.Is(err, ErrStakeConsensusInactive) {
			t.Fatalf("got %v, want ErrStakeConsensusInactive", err)
		}
	})
	t.Run("valid evidence is accepted", func(t *testing.T) {
		s := fresh()
		ev := signedValidatorTx(t, 1, reporter, 0, "validator_evidence", 1, 10, base)
		if err := s.applyValidatorTxCtx(ev, "validator_evidence", ctx); err != nil {
			t.Fatalf("valid evidence rejected: %v", err)
		}
	})
}

// TestEmptyStakeSetKeepsLegacyRoster: stake consensus configured but
// nobody bonded. The chain must keep running on the configured roster
// rather than halt.
func TestEmptyStakeSetKeepsLegacyRoster(t *testing.T) {
	h := newConsensusHarness(t) // no validators
	for i := 0; i < 6; i++ {
		h.legacyBlock()
	}
	if h.chain.StakeConsensusActiveAt(7) {
		t.Fatal("stake consensus reported active with an empty set")
	}
}

func TestStakeConsensusParamsValidate(t *testing.T) {
	ok := consensusTestParams
	if err := ok.Validate(); err != nil {
		t.Fatalf("test params should be valid: %v", err)
	}
	bad := map[string]func(p *ValidatorStakingParams){
		"consensus not after enabled": func(p *ValidatorStakingParams) { p.ConsensusFromHeight = p.EnabledFromHeight },
		"zero epoch":                  func(p *ValidatorStakingParams) { p.EpochBlocks = 0 },
		"free double-signing":         func(p *ValidatorStakingParams) { p.SlashFractionBps = 0 },
		"slash over 100%":             func(p *ValidatorStakingParams) { p.SlashFractionBps = 10_001 },
		"reporter over 100%":          func(p *ValidatorStakingParams) { p.ReporterRewardBps = 10_001 },
	}
	for name, mutate := range bad {
		p := consensusTestParams
		mutate(&p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}
