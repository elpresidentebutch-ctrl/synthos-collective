package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

// 1. Below H: fees go to the proposer exactly as before, even with a treasury
// set; an empty proposer burns everything.
func TestFeeModelV2_BelowActivationHeight(t *testing.T) {
	st := NewState()
	treasury := Address("0xtreasury0000000000000000000000000000000")
	proposer := Address("0xproposer000000000000000000000000000000")
	st.TreasuryAddress = treasury

	txs := []Tx{
		{Fee: 60},
		{Fee: 40},
	}

	heightBelowH := FeeModelV2ActivationHeight - 1
	split, err := ApplyBlockFees(st, txs, proposer, heightBelowH)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if split.TotalFees != 100 {
		t.Fatalf("total fees = %d, want 100", split.TotalFees)
	}
	if split.BurnAmount != 50 {
		t.Fatalf("burn amount = %d, want 50", split.BurnAmount)
	}
	if split.ToProposer != 50 {
		t.Fatalf("to proposer = %d, want 50", split.ToProposer)
	}
	if split.ToTreasury != 0 {
		t.Fatalf("to treasury = %d, want 0", split.ToTreasury)
	}

	if bal := st.Get(proposer).Balance; bal != 50 {
		t.Fatalf("proposer balance = %d, want 50", bal)
	}
	if bal := st.Get(treasury).Balance; bal != 0 {
		t.Fatalf("treasury balance = %d, want 0", bal)
	}
	if st.FeeEpochTreasuryIncome != 0 {
		t.Fatalf("epoch treasury income = %d, want 0", st.FeeEpochTreasuryIncome)
	}

	// Empty proposer burns everything
	stEmpty := NewState()
	stEmpty.TreasuryAddress = treasury
	splitEmpty, err := ApplyBlockFees(stEmpty, txs, "", heightBelowH)
	if err != nil {
		t.Fatalf("unexpected error on empty proposer: %v", err)
	}
	if splitEmpty.BurnAmount != 100 {
		t.Fatalf("burn amount on empty proposer = %d, want 100", splitEmpty.BurnAmount)
	}
	if bal := stEmpty.Get(treasury).Balance; bal != 0 {
		t.Fatalf("treasury balance = %d, want 0", bal)
	}
}

// 2. At and after H: half burned, half to the treasury; proposer gets nothing.
func TestFeeModelV2_AtAndAfterActivationHeight(t *testing.T) {
	st := NewState()
	treasury := Address("0xtreasury0000000000000000000000000000000")
	proposer := Address("0xproposer000000000000000000000000000000")
	st.TreasuryAddress = treasury

	txs := []Tx{
		{Fee: 60},
		{Fee: 40},
	}

	heightAtH := FeeModelV2ActivationHeight
	split, err := ApplyBlockFees(st, txs, proposer, heightAtH)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if split.TotalFees != 100 {
		t.Fatalf("total fees = %d, want 100", split.TotalFees)
	}
	if split.BurnAmount != 50 {
		t.Fatalf("burn amount = %d, want 50", split.BurnAmount)
	}
	if split.ToTreasury != 50 {
		t.Fatalf("to treasury = %d, want 50", split.ToTreasury)
	}
	if split.ToProposer != 0 {
		t.Fatalf("to proposer = %d, want 0", split.ToProposer)
	}

	if bal := st.Get(proposer).Balance; bal != 0 {
		t.Fatalf("proposer balance = %d, want 0", bal)
	}
	if bal := st.Get(treasury).Balance; bal != 50 {
		t.Fatalf("treasury balance = %d, want 50", bal)
	}
	if st.FeeEpochTreasuryIncome != 50 {
		t.Fatalf("epoch treasury income = %d, want 50", st.FeeEpochTreasuryIncome)
	}

	// At height H + 1
	splitAfter, err := ApplyBlockFees(st, txs, proposer, heightAtH+1)
	if err != nil {
		t.Fatalf("unexpected error at H+1: %v", err)
	}
	if splitAfter.ToTreasury != 50 {
		t.Fatalf("to treasury = %d, want 50", splitAfter.ToTreasury)
	}
	if bal := st.Get(treasury).Balance; bal != 100 {
		t.Fatalf("treasury balance = %d, want 100", bal)
	}
	if st.FeeEpochTreasuryIncome != 100 {
		t.Fatalf("epoch treasury income = %d, want 100", st.FeeEpochTreasuryIncome)
	}
	if bal := st.Get(proposer).Balance; bal != 0 {
		t.Fatalf("proposer balance = %d, want 0", bal)
	}

	// Missing TreasuryAddress at H or later must fail
	stNoTreasury := NewState()
	_, err = ApplyBlockFees(stNoTreasury, txs, proposer, heightAtH)
	if err == nil {
		t.Fatal("expected error applying fees at H when TreasuryAddress is empty, got nil")
	}
}

// 3. RequiredFee table; a tx with too low a fee fails at H and succeeds below H.
func TestFeeModelV2_RequiredFee(t *testing.T) {
	testCases := []struct {
		amount  uint64
		wantFee uint64
	}{
		{1, 10},
		{10_000, 10},
		{10_001, 11},
		{100_000, 100},
		{1_000_000, 1_000},
		{20_000_000, 1_000},
	}

	for _, tc := range testCases {
		tx := Tx{Amount: tc.amount} // default empty metadata & AssetID -> SYN send
		got := RequiredFee(tx)
		if got != tc.wantFee {
			t.Errorf("RequiredFee for amount %d = %d, want %d", tc.amount, got, tc.wantFee)
		}

		// Also check with explicit type: transfer
		txTransfer := Tx{
			Amount:   tc.amount,
			Metadata: []KeyValuePair{{Key: "type", Value: "transfer"}},
		}
		gotTransfer := RequiredFee(txTransfer)
		if gotTransfer != tc.wantFee {
			t.Errorf("RequiredFee (transfer) for amount %d = %d, want %d", tc.amount, gotTransfer, tc.wantFee)
		}
	}

	// Non-SYN send: AssetID != "" or other tx types -> required fee 10
	assetTx := Tx{
		Amount:  500_000,
		AssetID: "NGOT",
	}
	if got := RequiredFee(assetTx); got != 10 {
		t.Errorf("RequiredFee for non-SYN asset send = %d, want 10", got)
	}

	otherTx := Tx{
		Amount:   500_000,
		Metadata: []KeyValuePair{{Key: "type", Value: "validator_bond"}},
	}
	if got := RequiredFee(otherTx); got != 10 {
		t.Errorf("RequiredFee for validator_bond = %d, want 10", got)
	}

	// Tx with too low a fee fails at H and succeeds below H
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sender := AddressFromPublicKey(pub)
	recipient := Address("0xrecip00000000000000000000000000000000")

	st := NewState()
	st.Accounts[sender] = Account{Balance: 10_000_000}

	// 100,000 SYN send requires 100 fee. Provide fee 50.
	lowFeeTx := Tx{
		ChainID:   1,
		From:      sender,
		To:        recipient,
		Amount:    100_000,
		Fee:       50,
		PublicKey: "0x" + hex.EncodeToString(pub),
		Timestamp: 1000,
	}
	if err := lowFeeTx.Sign(priv); err != nil {
		t.Fatal(err)
	}

	// Below H: succeeds
	ctxBelow := TxContext{Height: uint64(FeeModelV2ActivationHeight - 1)}
	cloneBelow := st.Clone()
	if err := ApplyTransaction(cloneBelow, lowFeeTx, ctxBelow); err != nil {
		t.Fatalf("expected low-fee tx to succeed below H, got error: %v", err)
	}

	// At H: fails with ErrFeeTooLow
	ctxAt := TxContext{Height: uint64(FeeModelV2ActivationHeight)}
	cloneAt := st.Clone()
	if err := ApplyTransaction(cloneAt, lowFeeTx, ctxAt); err != ErrFeeTooLow {
		t.Fatalf("expected ErrFeeTooLow at H, got: %v", err)
	}

	// At H with adequate fee (100): succeeds
	validFeeTx := lowFeeTx
	validFeeTx.Fee = 100
	if err := validFeeTx.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := ApplyTransaction(cloneAt, validFeeTx, ctxAt); err != nil {
		t.Fatalf("expected valid fee tx to succeed at H, got error: %v", err)
	}
}

// 4. Payout with 4 equal validators all signing: each gets 1/4 of the pool.
func TestFeeModelV2_PayoutFourEqualValidators(t *testing.T) {
	st := NewState()
	treasury := Address("0xtreasury0000000000000000000000000000000")
	st.TreasuryAddress = treasury
	st.Accounts[treasury] = Account{Balance: 10_000_000}

	st.FeeEpochTreasuryIncome = 800_000 // pool = 400_000

	v1 := Address("0xval10000000000000000000000000000000000")
	v2 := Address("0xval20000000000000000000000000000000000")
	v3 := Address("0xval30000000000000000000000000000000000")
	v4 := Address("0xval40000000000000000000000000000000000")

	// Each validator has 100 power and signed every block
	st.RecordFeeEpochVote(v1, 100, true)
	st.RecordFeeEpochVote(v2, 100, true)
	st.RecordFeeEpochVote(v3, 100, true)
	st.RecordFeeEpochVote(v4, 100, true)

	payouts, err := st.DistributeFeeEpochPayout(0)
	if err != nil {
		t.Fatalf("payout error: %v", err)
	}

	if len(payouts) != 4 {
		t.Fatalf("expected 4 payouts, got %d", len(payouts))
	}

	for _, p := range payouts {
		if p.Amount != 100_000 { // 1/4 of 400_000
			t.Errorf("operator %s payout = %d, want 100000", p.Operator, p.Amount)
		}
		if bal := st.Get(p.Operator).Balance; bal != 100_000 {
			t.Errorf("operator %s balance = %d, want 100000", p.Operator, bal)
		}
	}

	// Treasury paid out 400_000, 9_600_000 remains
	if bal := st.Get(treasury).Balance; bal != 9_600_000 {
		t.Fatalf("treasury balance = %d, want 9600000", bal)
	}

	// Epoch state reset
	if st.FeeEpochTreasuryIncome != 0 || st.FeeEpochTotalPower != 0 || len(st.FeeEpochSignedPower) != 0 {
		t.Fatal("epoch state was not reset after payout")
	}
}

// 5. One validator signs half the epoch: it gets half its share; the rest
// stays in the treasury.
func TestFeeModelV2_OneValidatorSignsHalf(t *testing.T) {
	st := NewState()
	treasury := Address("0xtreasury0000000000000000000000000000000")
	st.TreasuryAddress = treasury
	st.Accounts[treasury] = Account{Balance: 10_000_000}

	st.FeeEpochTreasuryIncome = 800_000 // pool = 400_000

	v1 := Address("0xval10000000000000000000000000000000000")
	v2 := Address("0xval20000000000000000000000000000000000")
	v3 := Address("0xval30000000000000000000000000000000000")
	v4 := Address("0xval40000000000000000000000000000000000")

	// Total power per round = 400 (100 each).
	// Round 1: all 4 sign
	st.RecordFeeEpochVote(v1, 100, true)
	st.RecordFeeEpochVote(v2, 100, true)
	st.RecordFeeEpochVote(v3, 100, true)
	st.RecordFeeEpochVote(v4, 100, true)

	// Round 2: v1, v2, v3 sign; v4 is offline (did not commit)
	st.RecordFeeEpochVote(v1, 100, true)
	st.RecordFeeEpochVote(v2, 100, true)
	st.RecordFeeEpochVote(v3, 100, true)
	st.RecordFeeEpochVote(v4, 100, false)

	// Total power recorded = 800
	// v1, v2, v3 signed power = 200 each (200/800 = 1/4 of pool = 100_000)
	// v4 signed power = 100 (100/800 = 1/8 of pool = 50_000)

	payouts, err := st.DistributeFeeEpochPayout(0)
	if err != nil {
		t.Fatalf("payout error: %v", err)
	}

	payoutMap := make(map[Address]uint64)
	for _, p := range payouts {
		payoutMap[p.Operator] = p.Amount
	}

	if payoutMap[v1] != 100_000 {
		t.Errorf("v1 payout = %d, want 100000", payoutMap[v1])
	}
	if payoutMap[v2] != 100_000 {
		t.Errorf("v2 payout = %d, want 100000", payoutMap[v2])
	}
	if payoutMap[v3] != 100_000 {
		t.Errorf("v3 payout = %d, want 100000", payoutMap[v3])
	}
	if payoutMap[v4] != 50_000 { // half of 100_000
		t.Errorf("v4 payout = %d, want 50000", payoutMap[v4])
	}

	// Total disbursed: 350_000 (the unearned 50_000 remains in treasury)
	// Starting balance 10_000_000 - 350_000 = 9_650_000
	if bal := st.Get(treasury).Balance; bal != 9_650_000 {
		t.Fatalf("treasury balance = %d, want 9650000", bal)
	}
}

// 6. Treasury balance smaller than the pool: payouts scale down, no negative balance.
func TestFeeModelV2_TreasuryBalanceSmallerThanPool(t *testing.T) {
	st := NewState()
	treasury := Address("0xtreasury0000000000000000000000000000000")
	st.TreasuryAddress = treasury
	st.Accounts[treasury] = Account{Balance: 100} // Treasury only has 100

	st.FeeEpochTreasuryIncome = 800 // pool = 400

	v1 := Address("0xval10000000000000000000000000000000000")
	v2 := Address("0xval20000000000000000000000000000000000")

	// Equal 50/50 split -> unscaled share would be 200 each (sum = 400)
	// Scaled down to balance 100: share = 200 * 100 / 400 = 50 each
	st.RecordFeeEpochVote(v1, 100, true)
	st.RecordFeeEpochVote(v2, 100, true)

	payouts, err := st.DistributeFeeEpochPayout(0)
	if err != nil {
		t.Fatalf("payout error: %v", err)
	}

	for _, p := range payouts {
		if p.Amount != 50 {
			t.Errorf("operator %s payout = %d, want 50", p.Operator, p.Amount)
		}
	}

	// Treasury balance is 0, never negative or underflowed
	if bal := st.Get(treasury).Balance; bal != 0 {
		t.Fatalf("treasury balance = %d, want 0", bal)
	}
}

// 7. State root unchanged before H: a state with these fields at zero
// hashes exactly like before the change.
func TestFeeModelV2_StateRootUnchangedBeforeActivation(t *testing.T) {
	addr1 := Address("0x1111111111111111111111111111111111111111")
	st := NewState()
	st.Accounts[addr1] = Account{Balance: 1000, Nonce: 1}

	// With fields at zero
	st.FeeEpochTreasuryIncome = 0
	st.FeeEpochTotalPower = 0
	st.FeeEpochSignedPower = make(map[Address]uint64)

	rootWithZeroFields := st.Root()

	// Compare with pristine state
	stPristine := NewState()
	stPristine.Accounts[addr1] = Account{Balance: 1000, Nonce: 1}

	rootPristine := stPristine.Root()

	if rootWithZeroFields != rootPristine {
		t.Fatalf("state root mismatch with zero fee epoch fields:\nwith zero: %s\npristine:  %s", rootWithZeroFields, rootPristine)
	}

	// Verify that once non-zero, it DOES commit to the root
	st.FeeEpochTreasuryIncome = 100
	rootWithNonZero := st.Root()
	if rootWithNonZero == rootPristine {
		t.Fatal("state root did not change when FeeEpochTreasuryIncome was non-zero")
	}
}
