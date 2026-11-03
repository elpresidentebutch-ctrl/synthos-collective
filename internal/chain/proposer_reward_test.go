package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

// TestApplyBlockEconomics_CreditsRealProposerAddress guards against the
// exact bug the audit found: applyBlockEconomics used to build the
// proposer's reward address as Address("0x"+proposerID) directly. For a
// proposerID like "synthos-validator-12" that is not valid hex and not 20
// bytes, so every proposer reward was credited to an address nobody could
// ever control or spend from -- silently stranded. The fix resolves the
// proposer's real address from its registered public key (the same way
// every other address in this chain is derived) instead.
func TestApplyBlockEconomics_CreditsRealProposerAddress(t *testing.T) {
	proposerPub, proposerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	proposerID := "synthos-validator-12"
	realProposerAddr := AddressFromPublicKey(proposerPub)
	bogusOldStyleAddr := Address("0x" + proposerID)
	if realProposerAddr == bogusOldStyleAddr {
		t.Fatal("test setup invalid: real and bogus addresses coincide")
	}

	senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	senderAddr := AddressFromPublicKey(senderPub)

	g := Genesis{
		ChainID: "test-chain",
		Alloc:   map[Address]uint64{senderAddr: 1_000},
	}
	c, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	c.SetValidatorSet(map[string]ed25519.PublicKey{proposerID: proposerPub}, 1)

	tx := Tx{
		ChainID: c.TransactionChainID(),
		From:    senderAddr,
		To:      senderAddr,
		Amount:  1,
		Fee:     100,
		Nonce:   0,
	}
	tx.PublicKey = "0x" + bytesToHex(senderPub)
	if err := tx.Sign(senderPriv); err != nil {
		t.Fatal(err)
	}

	b, err := c.BuildBlock(proposerID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Tx) != 0 {
		t.Fatalf("expected mempool empty (no submitted tx), got %d tx in block", len(b.Tx))
	}

	// BuildBlock only pulls from the mempool, so submit through the normal
	// path and rebuild.
	if err := c.SubmitTx(tx); err != nil {
		t.Fatalf("SubmitTx: %v", err)
	}
	b, err = c.BuildBlock(proposerID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Tx) != 1 {
		t.Fatalf("expected 1 tx in block, got %d", len(b.Tx))
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

	realBalance := c.State.Get(realProposerAddr).Balance
	bogusBalance := c.State.Get(bogusOldStyleAddr).Balance
	if realBalance == 0 {
		t.Fatalf("expected the real proposer address to receive the fee reward, got balance 0")
	}
	if bogusBalance != 0 {
		t.Fatalf("expected the old-style \"0x\"+proposerID address to receive nothing, got balance %d", bogusBalance)
	}
	t.Logf("fee reward correctly credited to real proposer address %s (got %d), nothing stranded at %s", realProposerAddr, realBalance, bogusOldStyleAddr)
}

// TestApplyBlockEconomics_UnresolvableProposerBurnsFeeEntirely covers a
// proposer ID with no registered key (e.g. no validator set configured):
// the fee must be fully burned, never credited to a fabricated address.
func TestApplyBlockEconomics_UnresolvableProposerBurnsFeeEntirely(t *testing.T) {
	senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	senderAddr := AddressFromPublicKey(senderPub)

	g := Genesis{
		ChainID: "test-chain",
		Alloc:   map[Address]uint64{senderAddr: 1_000},
	}
	c, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately no SetValidatorSet call: proposerID has no known key.

	tx := Tx{
		ChainID: c.TransactionChainID(),
		From:    senderAddr,
		To:      senderAddr,
		Amount:  1,
		Fee:     100,
		Nonce:   0,
	}
	tx.PublicKey = "0x" + bytesToHex(senderPub)
	if err := tx.Sign(senderPriv); err != nil {
		t.Fatal(err)
	}
	if err := c.SubmitTx(tx); err != nil {
		t.Fatalf("SubmitTx: %v", err)
	}

	proposerID := "unregistered-proposer"
	b, err := c.BuildBlock(proposerID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	if err := c.FinalizeBlock(b); err != nil {
		t.Fatalf("FinalizeBlock: %v", err)
	}

	bogusOldStyleAddr := Address("0x" + proposerID)
	if bal := c.State.Get(bogusOldStyleAddr).Balance; bal != 0 {
		t.Fatalf("expected nothing credited to the old-style fabricated address, got balance %d", bal)
	}
}


// TestApplyBlockEconomics_CreditsTreasuryWhenConfigured verifies that when
// TreasuryAddress is configured in genesis/state, all transaction fee rewards
// flow directly into the Protocol Treasury rather than the individual proposer.
func TestApplyBlockEconomics_CreditsTreasuryWhenConfigured(t *testing.T) {
	senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	senderAddr := AddressFromPublicKey(senderPub)
	treasuryAddr := Address("0xtreasury0000000000000000000000000000000")

	g := Genesis{
		ChainID:  "test-chain",
		Alloc:    map[Address]uint64{senderAddr: 1_000},
		Metadata: map[string]any{"treasury_address": string(treasuryAddr)},
	}
	c, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}

	proposerPub, proposerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	proposerID := "synthos-validator-1"
	proposerAddr := AddressFromPublicKey(proposerPub)
	c.SetValidatorSet(map[string]ed25519.PublicKey{proposerID: proposerPub}, 1)

	tx := Tx{
		ChainID: c.TransactionChainID(),
		From:    senderAddr,
		To:      senderAddr,
		Amount:  1,
		Fee:     100,
		Nonce:   0,
	}
	tx.PublicKey = "0x" + bytesToHex(senderPub)
	if err := tx.Sign(senderPriv); err != nil {
		t.Fatal(err)
	}
	if err := c.SubmitTx(tx); err != nil {
		t.Fatalf("SubmitTx: %v", err)
	}

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

	treasuryBalance := c.State.Get(treasuryAddr).Balance
	proposerBalance := c.State.Get(proposerAddr).Balance
	if treasuryBalance == 0 {
		t.Fatalf("expected treasury to receive fee reward, got %d", treasuryBalance)
	}
	if proposerBalance != 0 {
		t.Fatalf("expected individual proposer to receive 0 direct fees under treasury model, got %d", proposerBalance)
	}
	t.Logf("treasury correctly received %d fees, individual proposer received 0 direct fees", treasuryBalance)
}

func TestApplyBlockEconomics_ZeroFeesNoOp(t *testing.T) {
	st := NewState()
	st.TreasuryAddress = "0xtreasury0000000000000000000000000000000"
	proposer := Address("0xproposer000000000000000000000000000000")

	// Empty txs
	if err := applyBlockEconomics(st, nil, proposer); err != nil {
		t.Fatalf("unexpected error on nil txs: %v", err)
	}
	if bal := st.Get(st.TreasuryAddress).Balance; bal != 0 {
		t.Fatalf("expected 0 treasury balance on empty txs, got %d", bal)
	}

	// Txs with 0 fee
	txs := []Tx{
		{Fee: 0},
		{Fee: 0},
	}
	if err := applyBlockEconomics(st, txs, proposer); err != nil {
		t.Fatalf("unexpected error on 0 fee txs: %v", err)
	}
	if bal := st.Get(st.TreasuryAddress).Balance; bal != 0 {
		t.Fatalf("expected 0 treasury balance on 0-fee txs, got %d", bal)
	}
}

func TestApplyBlockEconomics_FallbackToProposerWhenNoTreasury(t *testing.T) {
	st := NewState()
	st.TreasuryAddress = "" // No treasury configured
	proposer := Address("0xproposer000000000000000000000000000000")

	txs := []Tx{{Fee: 100}}
	if err := applyBlockEconomics(st, txs, proposer); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 50% burn, 50% reward to proposer
	proposerBal := st.Get(proposer).Balance
	if proposerBal != 50 {
		t.Fatalf("expected proposer to receive 50, got %d", proposerBal)
	}
}

func TestApplyBlockEconomics_BoundaryAndBurnFractions(t *testing.T) {
	st := NewState()
	treasury := Address("0xtreasury0000000000000000000000000000000")
	st.TreasuryAddress = treasury

	// Fee = 1: burn = (1 * 50) / 100 = 0, reward = 1 - 0 = 1
	txs := []Tx{{Fee: 1}}
	if err := applyBlockEconomics(st, txs, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bal := st.Get(treasury).Balance; bal != 1 {
		t.Fatalf("expected treasury balance 1 on 1 fee, got %d", bal)
	}

	// Fee = 3: total fees = 4? Wait, this is second block, tx fee = 3
	// burn = (3 * 50) / 100 = 1, reward = 3 - 1 = 2
	txs = []Tx{{Fee: 3}}
	if err := applyBlockEconomics(st, txs, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Prior balance 1 + 2 = 3
	if bal := st.Get(treasury).Balance; bal != 3 {
		t.Fatalf("expected treasury balance 3, got %d", bal)
	}
}

func TestApplyBlockEconomics_FeeOverflowGuarded(t *testing.T) {
	st := NewState()
	st.TreasuryAddress = "0xtreasury0000000000000000000000000000000"

	txs := []Tx{
		{Fee: ^uint64(0)},
		{Fee: 1},
	}
	if err := applyBlockEconomics(st, txs, ""); err == nil {
		t.Fatal("expected error on total fee overflow, got nil")
	}
}

func bytesToHex(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}
