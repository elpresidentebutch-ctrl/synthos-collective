package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	abcitypes "github.com/cometbft/cometbft/abci/types"

	"synthos-collective/internal/chain"
)

func TestFeeModelV3_CheckTxAndSurge(t *testing.T) {
	f := newFixture(t, 2_000)
	treasury := newAccount(t)
	f.gen.Chain.Metadata = map[string]any{"treasury_address": string(treasury.addr)}
	a, _ := f.newApp("")

	ctx := context.Background()
	h := chain.FeeModelV3ActivationHeight

	// Fast-forward height to activation height
	a.mu.Lock()
	a.height = h - 1
	a.committed.TreasuryAddress = treasury.addr
	a.committed.FeeModelV3BaseFee = 20 // active base fee 20 SYN
	a.check = a.committed.Clone()
	a.mu.Unlock()

	// 1. Transaction with fee = 10 (below base fee 20) -> CheckTx must reject
	underpricedTx := f.tx(f.user, 0, 100, 10, nil)
	res, err := a.CheckTx(ctx, &abcitypes.RequestCheckTx{Tx: underpricedTx})
	if err != nil {
		t.Fatalf("CheckTx returned unexpected error: %v", err)
	}
	if res.Code == 0 {
		t.Fatal("CheckTx accepted transaction below RequiredFeeV3")
	}

	// 2. Transaction with fee = 25 (base fee 20 + 5 tip) -> CheckTx must accept
	validTx := f.tx(f.user, 0, 100, 25, nil)
	resValid, err := a.CheckTx(ctx, &abcitypes.RequestCheckTx{Tx: validTx})
	if err != nil {
		t.Fatalf("CheckTx returned error for valid tx: %v", err)
	}
	if resValid.Code != 0 {
		t.Fatalf("CheckTx rejected valid tx: %s", resValid.Log)
	}
}

func TestFeeModelV3_PrepareProposalPriorityOrdering(t *testing.T) {
	f := newFixture(t, 2_000)
	treasury := newAccount(t)
	f.gen.Chain.Metadata = map[string]any{"treasury_address": string(treasury.addr)}
	a, _ := f.newApp("")

	ctx := context.Background()
	h := chain.FeeModelV3ActivationHeight

	a.mu.Lock()
	a.height = h - 1
	a.committed.TreasuryAddress = treasury.addr
	a.committed.FeeModelV3BaseFee = 10
	a.check = a.committed.Clone()
	a.mu.Unlock()

	// Create 3 accounts
	u1, u2, u3 := newAccount(t), newAccount(t), newAccount(t)
	a.mu.Lock()
	a.committed.Set(u1.addr, chain.Account{Balance: 50_000})
	a.committed.Set(u2.addr, chain.Account{Balance: 50_000})
	a.committed.Set(u3.addr, chain.Account{Balance: 50_000})
	a.check = a.committed.Clone()
	a.mu.Unlock()

	// tx1: fee 15 (tip 5)
	raw1 := f.tx(u1, 0, 10, 15, nil)
	// tx2: fee 50 (tip 40)
	raw2 := f.tx(u2, 0, 10, 50, nil)
	// tx3: fee 20 (tip 10)
	raw3 := f.tx(u3, 0, 10, 20, nil)

	// Send them to PrepareProposal in arbitrary order (tx1, tx2, tx3)
	req := &abcitypes.RequestPrepareProposal{
		Height: h,
		Time:   time.Now(),
		Txs:    [][]byte{raw1, raw2, raw3},
	}

	resp, err := a.PrepareProposal(ctx, req)
	if err != nil {
		t.Fatalf("PrepareProposal error: %v", err)
	}
	if len(resp.Txs) != 3 {
		t.Fatalf("PrepareProposal returned %d txs, want 3", len(resp.Txs))
	}

	// Verify order: tx2 (tip 40) must be first, tx3 (tip 10) second, tx1 (tip 5) third
	var decodedFirst, decodedSecond, decodedThird chain.Tx
	_ = json.Unmarshal(resp.Txs[0], &decodedFirst)
	_ = json.Unmarshal(resp.Txs[1], &decodedSecond)
	_ = json.Unmarshal(resp.Txs[2], &decodedThird)

	if decodedFirst.From != u2.addr {
		t.Fatalf("first tx sender is %s, want u2 (tip 40)", decodedFirst.From)
	}
	if decodedSecond.From != u3.addr {
		t.Fatalf("second tx sender is %s, want u3 (tip 10)", decodedSecond.From)
	}
	if decodedThird.From != u1.addr {
		t.Fatalf("third tx sender is %s, want u1 (tip 5)", decodedThird.From)
	}
}

func TestFeeModelV3_PrepareProposal_SenderNonceOrderPreserved(t *testing.T) {
	f := newFixture(t, 2_000)
	treasury := newAccount(t)
	f.gen.Chain.Metadata = map[string]any{"treasury_address": string(treasury.addr)}
	a, _ := f.newApp("")

	ctx := context.Background()
	h := chain.FeeModelV3ActivationHeight

	a.mu.Lock()
	a.height = h - 1
	a.committed.TreasuryAddress = treasury.addr
	a.committed.FeeModelV3BaseFee = 10
	a.check = a.committed.Clone()
	a.mu.Unlock()

	u1 := newAccount(t)
	u2 := newAccount(t)
	a.mu.Lock()
	a.committed.Set(u1.addr, chain.Account{Balance: 50_000})
	a.committed.Set(u2.addr, chain.Account{Balance: 50_000})
	a.check = a.committed.Clone()
	a.mu.Unlock()

	// u1 sends two txs:
	// tx1: nonce 0, fee 15 (tip 5)
	// tx2: nonce 1, fee 50 (tip 40)
	// u2 sends one tx:
	// tx3: nonce 0, fee 30 (tip 20)
	// If sorting was purely by tip without nonce awareness, tx2 (tip 40) would be first, but it would fail execution or execute out of order.
	// With nonce preservation, at the start available heads are u1.tx1 (tip 5) and u2.tx3 (tip 20).
	// u2.tx3 is picked first (tip 20 > 5).
	// Next available heads are u1.tx1 (tip 5). u1.tx1 is picked.
	// Now u1.tx2 (tip 40) becomes available and is picked!
	tx1 := f.tx(u1, 0, 10, 15, nil)
	tx2 := f.tx(u1, 1, 10, 50, nil)
	tx3 := f.tx(u2, 0, 10, 30, nil)

	req := &abcitypes.RequestPrepareProposal{
		Height: h,
		Time:   time.Now(),
		Txs:    [][]byte{tx2, tx1, tx3}, // input intentionally scrambled
	}

	resp, err := a.PrepareProposal(ctx, req)
	if err != nil {
		t.Fatalf("PrepareProposal error: %v", err)
	}
	if len(resp.Txs) != 3 {
		t.Fatalf("PrepareProposal returned %d txs, want 3", len(resp.Txs))
	}

	var d0, d1, d2 chain.Tx
	_ = json.Unmarshal(resp.Txs[0], &d0)
	_ = json.Unmarshal(resp.Txs[1], &d1)
	_ = json.Unmarshal(resp.Txs[2], &d2)

	// Verify order:
	// d0 must be u2 nonce 0 (tip 20 beat u1 nonce 0 tip 5)
	if d0.From != u2.addr || d0.Nonce != 0 {
		t.Fatalf("tx 0 expected u2 nonce 0, got %s nonce %d", d0.From, d0.Nonce)
	}
	// d1 must be u1 nonce 0 (tip 5)
	if d1.From != u1.addr || d1.Nonce != 0 {
		t.Fatalf("tx 1 expected u1 nonce 0, got %s nonce %d", d1.From, d1.Nonce)
	}
	// d2 must be u1 nonce 1 (tip 40)
	if d2.From != u1.addr || d2.Nonce != 1 {
		t.Fatalf("tx 2 expected u1 nonce 1, got %s nonce %d", d2.From, d2.Nonce)
	}
}

func TestFeeModelV3_PrepareProposal_BelowActivation_MaxTxBytes(t *testing.T) {
	f := newFixture(t, 2_000)
	a, _ := f.newApp("")
	ctx := context.Background()

	u := newAccount(t)
	tx1 := f.tx(u, 0, 10, 10, nil)
	tx2 := f.tx(u, 1, 10, 10, nil)
	tx3 := f.tx(u, 2, 10, 10, nil)

	size1 := int64(len(tx1))
	size2 := int64(len(tx2))

	// Height below activation:
	req := &abcitypes.RequestPrepareProposal{
		Height:     1000,
		MaxTxBytes: size1 + size2, // fits only first 2
		Time:       time.Now(),
		Txs:        [][]byte{tx1, tx2, tx3},
	}

	resp, err := a.PrepareProposal(ctx, req)
	if err != nil {
		t.Fatalf("PrepareProposal error: %v", err)
	}
	if len(resp.Txs) != 2 {
		t.Fatalf("expected 2 txs, got %d", len(resp.Txs))
	}
}

func TestFeeModelV3_FinalizeBlockSurgeAdjustment(t *testing.T) {
	f := newFixture(t, 2_000)
	treasury := newAccount(t)
	f.gen.Chain.Metadata = map[string]any{"treasury_address": string(treasury.addr)}
	a, _ := f.newApp("")

	ctx := context.Background()
	h := chain.FeeModelV3ActivationHeight

	a.mu.Lock()
	a.height = h - 1
	a.committed.TreasuryAddress = treasury.addr
	a.committed.FeeModelV3BaseFee = 100 // starting base fee 100
	a.mu.Unlock()

	// Propose a block with 50 txs (2x Target of 25)
	// Base fee should surge up by ~12.5% to 112
	var txs [][]byte
	for i := 0; i < 50; i++ {
		u := newAccount(t)
		a.mu.Lock()
		a.committed.Set(u.addr, chain.Account{Balance: 10_000})
		a.mu.Unlock()
		txs = append(txs, f.tx(u, 0, 10, 100, nil))
	}

	req := &abcitypes.RequestFinalizeBlock{
		Height: h,
		Time:   time.Now(),
		Txs:    txs,
	}

	res, err := a.FinalizeBlock(ctx, req)
	if err != nil {
		t.Fatalf("FinalizeBlock error: %v", err)
	}

	// Commit block
	_, err = a.Commit(ctx, &abcitypes.RequestCommit{})
	if err != nil {
		t.Fatalf("Commit error: %v", err)
	}

	// Check updated base fee in state
	a.mu.Lock()
	newBaseFee := a.committed.FeeModelV3BaseFee
	a.mu.Unlock()

	if newBaseFee != 112 {
		t.Fatalf("expected surged base fee of 112, got %d", newBaseFee)
	}

	// Verify fee surge event was emitted
	foundEvent := false
	for _, ev := range res.Events {
		if ev.Type == "fee_model_v3_surge" {
			foundEvent = true
			break
		}
	}
	if !foundEvent {
		t.Fatal("fee_model_v3_surge event was not emitted")
	}
}
