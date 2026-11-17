package chain_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"synthos-collective/internal/chain"
)

func TestFeeModelV3_CalculateNextBaseFee(t *testing.T) {
	// Case 1: Target utilization (25 txs) -> Base fee remains identical
	base := uint64(100)
	next := chain.CalculateNextBaseFee(base, chain.FeeModelV3TargetBlockTxs)
	if next != base {
		t.Fatalf("target utilization changed base fee: got %d, want %d", next, base)
	}

	// Case 2: Full surge (50 txs = 2x target) -> Delta is 25.
	// adjustment = 100 * 25 / (25 * 8) = 100 / 8 = 12.
	// next = 100 + 12 = 112 (+12%)
	nextSurge := chain.CalculateNextBaseFee(base, 50)
	if nextSurge != 112 {
		t.Fatalf("surge base fee unexpected: got %d, want 112", nextSurge)
	}

	// Case 3: Empty block (0 txs) -> Decay
	// delta = 25. adjustment = 100 * 25 / (25 * 8) = 12.
	// next = 100 - 12 = 88 (-12%)
	nextDecay := chain.CalculateNextBaseFee(base, 0)
	if nextDecay != 88 {
		t.Fatalf("decay base fee unexpected: got %d, want 88", nextDecay)
	}

	// Case 4: MinBaseFee floor protection
	lowBase := chain.FeeModelV3MinBaseFee
	decayFloor := chain.CalculateNextBaseFee(lowBase, 0)
	if decayFloor != chain.FeeModelV3MinBaseFee {
		t.Fatalf("base fee dropped below MinBaseFee floor: got %d, want %d", decayFloor, chain.FeeModelV3MinBaseFee)
	}

	// Case 5: Zero base fee defaults to InitialBaseFee
	initBase := chain.CalculateNextBaseFee(0, chain.FeeModelV3TargetBlockTxs)
	if initBase != chain.FeeModelV3InitialBaseFee {
		t.Fatalf("zero base fee did not initialize: got %d, want %d", initBase, chain.FeeModelV3InitialBaseFee)
	}
}

func TestFeeModelV3_RequiredFeeV3(t *testing.T) {
	// Standard small transfer: baseFee is 10, amount is 500 (ceil = 1).
	// Required fee should be 10.
	txSmall := chain.Tx{Amount: 500}
	if req := chain.RequiredFeeV3(txSmall, 10); req != 10 {
		t.Fatalf("small transfer fee: got %d, want 10", req)
	}

	// Surged base fee: baseFee is 50.
	if req := chain.RequiredFeeV3(txSmall, 50); req != 50 {
		t.Fatalf("surged base fee: got %d, want 50", req)
	}

	// High value transfer: amount is 100,000 (ceil = 100). Base fee is 10.
	txLarge := chain.Tx{Amount: 100_000}
	if req := chain.RequiredFeeV3(txLarge, 10); req != 100 {
		t.Fatalf("large transfer fee: got %d, want 100", req)
	}

	// High value transfer with higher base fee: amount is 100,000 (ceil = 100), base fee is 150.
	if req := chain.RequiredFeeV3(txLarge, 150); req != 150 {
		t.Fatalf("large transfer fee under surge: got %d, want 150", req)
	}

	// Very large transfer: 10,000,000 SYN should be capped at 1,000 SYN when base fee is 10.
	txHuge := chain.Tx{Amount: 10_000_000}
	if req := chain.RequiredFeeV3(txHuge, 10); req != 1000 {
		t.Fatalf("huge transfer fee: got %d, want 1000", req)
	}

	// Very large transfer under extreme surge: base fee is 1,500. Required fee should be 1,500.
	if req := chain.RequiredFeeV3(txHuge, 1500); req != 1500 {
		t.Fatalf("huge transfer fee under extreme surge: got %d, want 1500", req)
	}
}

func TestFeeModelV3_ApplyBlockFees_WithPriorityTip(t *testing.T) {
	st := chain.NewState()
	treasury := chain.Address("0xtreasury0000000000000000000000000000000")
	proposer := chain.Address("0xproposer0000000000000000000000000000000")
	st.TreasuryAddress = treasury

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sender := chain.AddressFromPublicKey(pub)
	st.Set(sender, chain.Account{Balance: 1_000_000})

	// Set base fee in state for v3
	st.FeeModelV3BaseFee = 20
	h := chain.FeeModelV3ActivationHeight

	// Transaction paying 30 fee: base fee is 20, priority tip is 10
	tx := chain.Tx{
		ChainID:   1,
		From:      sender,
		To:        treasury,
		Amount:    100,
		Fee:       30,
		Nonce:     0,
		PublicKey: "0x" + toHex(pub),
	}
	if err := tx.Sign(priv); err != nil {
		t.Fatal(err)
	}

	// Apply transaction
	ctx := chain.TxContext{Height: uint64(h)}
	if err := chain.ApplyTransaction(st, tx, ctx); err != nil {
		t.Fatalf("apply tx failed: %v", err)
	}

	// Apply block fees
	split, err := chain.ApplyBlockFees(st, []chain.Tx{tx}, proposer, h)
	if err != nil {
		t.Fatalf("apply block fees failed: %v", err)
	}

	// Expected breakdown:
	// Total fee: 30
	// Base fee portion: 20 -> 50% burn (10), 50% treasury (10)
	// Tip portion: 10 -> 100% proposer (10)
	if split.TotalFees != 30 {
		t.Errorf("TotalFees = %d, want 30", split.TotalFees)
	}
	if split.BurnAmount != 10 {
		t.Errorf("BurnAmount = %d, want 10", split.BurnAmount)
	}
	if split.ToTreasury != 10 {
		t.Errorf("ToTreasury = %d, want 10", split.ToTreasury)
	}
	if split.ToProposer != 10 {
		t.Errorf("ToProposer = %d, want 10", split.ToProposer)
	}

	// Verify account balances
	if bal := st.Get(proposer).Balance; bal != 10 {
		t.Errorf("proposer balance = %d, want 10", bal)
	}
	if bal := st.Get(treasury).Balance; bal != 100+10 { // 100 transferred + 10 fee
		t.Errorf("treasury balance = %d, want 110", bal)
	}
}

func TestFeeModelV3_StateRootByteCompatibility(t *testing.T) {
	st1 := chain.NewState()
	st1.TreasuryAddress = chain.Address("0xtreasury")

	// State without V3 base fee
	root1 := st1.Root()

	// Clone should have identical root
	st2 := st1.Clone()
	if st2.Root() != root1 {
		t.Fatalf("clone root mismatch: %s vs %s", st2.Root(), root1)
	}

	// Setting base fee alters root
	st2.FeeModelV3BaseFee = 50
	if st2.Root() == root1 {
		t.Fatal("expected root to change when FeeModelV3BaseFee is set")
	}
}
