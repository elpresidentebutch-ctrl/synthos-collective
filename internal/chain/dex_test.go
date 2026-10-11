package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"
)

func generateTestKeyPair(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey, Address) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}
	addr := AddressFromPublicKey(pub)
	return priv, pub, addr
}

func signTestTx(t *testing.T, tx *Tx, priv ed25519.PrivateKey, pub ed25519.PublicKey) {
	t.Helper()
	tx.PublicKey = "0x" + hex.EncodeToString(pub)
	if err := tx.Sign(priv); err != nil {
		t.Fatalf("failed to sign tx: %v", err)
	}
}

func TestDEX_ConstantProductMath(t *testing.T) {
	dex := NewDEX()

	// Initial pool: 1,000,000 SYN and 100,000 USDC
	synReserve := uint64(1_000_000)
	usdcReserve := uint64(100_000)

	// Swap 10,000 SYN for USDC
	// Expected: (10,000 * 997 * 100,000) / (1,000,000 * 1000 + 10,000 * 997)
	// = 997,000,000,000 / 1,009,970,000 = 987 USDC
	out, err := dex.GetAmountOut(10_000, synReserve, usdcReserve)
	if err != nil {
		t.Fatalf("GetAmountOut failed: %v", err)
	}
	if out != 987 {
		t.Fatalf("expected 987 USDC out, got %d", out)
	}

	// Verify constant product k grows due to 0.3% fee
	kBefore := synReserve * usdcReserve
	kAfter := (synReserve + 10_000) * (usdcReserve - out)
	if kAfter < kBefore {
		t.Fatalf("k must never decrease: before=%d, after=%d", kBefore, kAfter)
	}
}

func TestDEX_LifecycleInConsensus(t *testing.T) {
	privLP, pubLP, addrLP := generateTestKeyPair(t)
	privTrader, pubTrader, addrTrader := generateTestKeyPair(t)

	gen := Genesis{
		ChainID:   "synthos-testnet-1",
		TxChainID: 20260702,
		Alloc: map[Address]uint64{
			addrLP:     10_000_000,
			addrTrader: 1_000_000,
		},
	}

	c, err := NewChain(gen)
	if err != nil {
		t.Fatalf("failed to create chain: %v", err)
	}

	// Step 1: LP issues 500,000 USDC
	txIssue := Tx{
		ChainID: c.TxChainID,
		From:    addrLP,
		To:      addrLP,
		Amount:  500_000,
		Fee:     MIN_FEE,
		Nonce:   0,
		AssetID: "USDC",
		Metadata: []KeyValuePair{
			{Key: "type", Value: "asset_issue"},
			{Key: "asset_id", Value: "USDC"},
			{Key: "amount", Value: "500000"},
		},
	}
	signTestTx(t, &txIssue, privLP, pubLP)
	if err := c.SubmitTx(txIssue); err != nil {
		t.Fatalf("failed to submit issue tx: %v", err)
	}

	block1, err := c.BuildBlock("validator-1", "0x0", 100)
	if err != nil {
		t.Fatalf("failed to build block 1: %v", err)
	}
	if err := c.FinalizeBlock(block1); err != nil {
		t.Fatalf("failed to finalize block 1: %v", err)
	}

	lpAcc := c.State.Get(addrLP)
	if lpAcc.Assets["USDC"] != 500_000 {
		t.Fatalf("expected 500,000 USDC for LP, got %d", lpAcc.Assets["USDC"])
	}

	// Step 2: LP adds liquidity (100,000 SYN + 50,000 USDC)
	txAddLP := Tx{
		ChainID: c.TxChainID,
		From:    addrLP,
		To:      PoolAddress("USDC"),
		Amount:  100_000,
		Fee:     MIN_FEE,
		Nonce:   lpAcc.Nonce,
		Metadata: []KeyValuePair{
			{Key: "type", Value: "dex_add_liquidity"},
			{Key: "asset_id", Value: "USDC"},
			{Key: "syn_amount", Value: "100000"},
			{Key: "asset_amount", Value: "50000"},
			{Key: "min_shares", Value: "1"},
		},
	}
	signTestTx(t, &txAddLP, privLP, pubLP)
	if err := c.SubmitTx(txAddLP); err != nil {
		t.Fatalf("failed to submit add_liquidity tx: %v", err)
	}

	block2, err := c.BuildBlock("validator-1", "0x0", 100)
	if err != nil {
		t.Fatalf("failed to build block 2: %v", err)
	}
	if err := c.FinalizeBlock(block2); err != nil {
		t.Fatalf("failed to finalize block 2: %v", err)
	}

	pool := c.State.GetDEXPool("USDC")
	if pool == nil {
		t.Fatalf("expected USDC pool to exist")
	}
	if pool.SynReserve != 100_000 || pool.AssetReserve != 50_000 {
		t.Fatalf("unexpected pool reserves: syn=%d, asset=%d", pool.SynReserve, pool.AssetReserve)
	}
	if pool.Shares[addrLP] == 0 {
		t.Fatalf("expected LP to have shares")
	}

	// Step 3: Trader swaps 10,000 SYN for USDC
	traderAcc := c.State.Get(addrTrader)
	expectedOut, err := c.DEX.GetAmountOut(10_000, 100_000, 50_000)
	if err != nil {
		t.Fatalf("quote failed: %v", err)
	}

	txSwap := Tx{
		ChainID: c.TxChainID,
		From:    addrTrader,
		To:      PoolAddress("USDC"),
		Amount:  10_000,
		Fee:     MIN_FEE,
		Nonce:   traderAcc.Nonce,
		AssetID: "syn",
		Metadata: []KeyValuePair{
			{Key: "type", Value: "dex_swap"},
			{Key: "asset_id", Value: "USDC"},
			{Key: "direction", Value: "syn_for_asset"},
			{Key: "min_amount_out", Value: strconv.FormatUint(expectedOut, 10)},
		},
	}
	signTestTx(t, &txSwap, privTrader, pubTrader)
	if err := c.SubmitTx(txSwap); err != nil {
		t.Fatalf("failed to submit swap tx: %v", err)
	}

	block3, err := c.BuildBlock("validator-1", "0x0", 100)
	if err != nil {
		t.Fatalf("failed to build block 3: %v", err)
	}
	if err := c.FinalizeBlock(block3); err != nil {
		t.Fatalf("failed to finalize block 3: %v", err)
	}

	traderAfter := c.State.Get(addrTrader)
	if traderAfter.Assets["USDC"] != expectedOut {
		t.Fatalf("expected trader to receive %d USDC, got %d", expectedOut, traderAfter.Assets["USDC"])
	}

	// Step 4: Trader swaps back half their USDC for SYN
	swapBackAmount := expectedOut / 2
	traderExpectedSyn, err := c.DEX.GetAmountOut(swapBackAmount, pool.AssetReserve-expectedOut, pool.SynReserve+10_000)
	if err != nil {
		t.Fatalf("quote back failed: %v", err)
	}

	txSwapBack := Tx{
		ChainID: c.TxChainID,
		From:    addrTrader,
		To:      PoolAddress("USDC"),
		Amount:  swapBackAmount,
		Fee:     MIN_FEE,
		Nonce:   traderAfter.Nonce,
		AssetID: "USDC",
		Metadata: []KeyValuePair{
			{Key: "type", Value: "dex_swap"},
			{Key: "asset_id", Value: "USDC"},
			{Key: "direction", Value: "asset_for_syn"},
			{Key: "min_amount_out", Value: "1"},
		},
	}
	signTestTx(t, &txSwapBack, privTrader, pubTrader)
	if err := c.SubmitTx(txSwapBack); err != nil {
		t.Fatalf("failed to submit swap back tx: %v", err)
	}

	block4, err := c.BuildBlock("validator-1", "0x0", 100)
	if err != nil {
		t.Fatalf("failed to build block 4: %v", err)
	}
	if err := c.FinalizeBlock(block4); err != nil {
		t.Fatalf("failed to finalize block 4: %v", err)
	}

	traderFinal := c.State.Get(addrTrader)
	if traderFinal.Assets["USDC"] != expectedOut-swapBackAmount {
		t.Fatalf("expected remaining USDC %d, got %d", expectedOut-swapBackAmount, traderFinal.Assets["USDC"])
	}
	if traderFinal.Balance < 1_000_000-10_000-MIN_FEE*2+traderExpectedSyn {
		t.Fatalf("expected trader SYN balance to increase by swap output")
	}

	// Step 5: Slippage protection triggers on bad min_amount_out
	txBadSlippage := Tx{
		ChainID: c.TxChainID,
		From:    addrTrader,
		To:      PoolAddress("USDC"),
		Amount:  1_000,
		Fee:     MIN_FEE,
		Nonce:   traderFinal.Nonce,
		AssetID: "syn",
		Metadata: []KeyValuePair{
			{Key: "type", Value: "dex_swap"},
			{Key: "asset_id", Value: "USDC"},
			{Key: "direction", Value: "syn_for_asset"},
			{Key: "min_amount_out", Value: "9999999"}, // Unattainable output
		},
	}
	signTestTx(t, &txBadSlippage, privTrader, pubTrader)
	err = c.State.ApplyTx(txBadSlippage)
	if err == nil {
		t.Fatalf("expected slippage error, got nil")
	}

	// Step 6: Multi-node consensus agreement
	// Another node replaying block 1-4 must reach the EXACT same state root!
	c2, err := NewChain(gen)
	if err != nil {
		t.Fatalf("failed to create chain 2: %v", err)
	}
	for i, b := range []*Block{block1, block2, block3, block4} {
		if err := c2.FinalizeBlock(b); err != nil {
			t.Fatalf("node 2 failed to finalize block %d: %v", i+1, err)
		}
	}
	if c2.State.Root() != c.State.Root() {
		t.Fatalf("state root mismatch between nodes: node1=%s, node2=%s", c.State.Root(), c2.State.Root())
	}
	fmt.Printf("State roots match in 100%% consensus: %s\n", c.State.Root())
}
