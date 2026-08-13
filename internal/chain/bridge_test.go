package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func signedBridgeTx(t *testing.T, c *Chain, priv ed25519.PrivateKey, from Address, to Address, amount uint64, metadata []KeyValuePair) Tx {
	t.Helper()
	pub := priv.Public().(ed25519.PublicKey)
	tx := Tx{
		ChainID:   c.TxChainID,
		From:      from,
		To:        to,
		Amount:    amount,
		Fee:       MIN_FEE,
		Nonce:     c.State.GetNextNonce(from),
		PublicKey: "0x" + hexString(pub),
		Metadata:  metadata,
		Timestamp: time.Now().UTC().Unix(),
	}
	if err := tx.Sign(priv); err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	return tx
}

func TestBridgeNativeLockRecordsReceipt(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	from := AddressFromPublicKey(pub)
	bridgeEscrow := Address("0x000000000000000000000000000000000000b07d")
	c, err := NewChain(Genesis{
		ChainID:   "synthos-test",
		TxChainID: 20260702,
		Alloc: map[Address]uint64{
			from: 1_000_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	tx := signedBridgeTx(t, c, priv, from, bridgeEscrow, 50_000, []KeyValuePair{
		{Key: "type", Value: "bridge_lock_native"},
		{Key: "destination_chain_id", Value: "84532"},
		{Key: "destination_recipient", Value: "0x1111111111111111111111111111111111111111"},
	})
	if err := c.SubmitTx(tx); err != nil {
		t.Fatalf("submit lock: %v", err)
	}
	block, err := c.BuildBlock("validator-1", "proof", 10)
	if err != nil {
		t.Fatalf("build block: %v", err)
	}
	if err := c.FinalizeBlock(block); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	status := c.State.BridgeStatus()
	if status.NativeLocks != 1 {
		t.Fatalf("native locks=%d want 1", status.NativeLocks)
	}
	events := c.State.BridgeEventsSnapshot()
	if len(events) != 1 {
		t.Fatalf("events=%d want 1", len(events))
	}
	if events[0].DestinationChainID != "84532" || events[0].Amount != 50_000 {
		t.Fatalf("unexpected bridge event: %+v", events[0])
	}
}

func TestBridgeReleaseRejectsSourceReplay(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bridgeAuthority := AddressFromPublicKey(pub)
	recipient := Address("0x2222222222222222222222222222222222222222")
	c, err := NewChain(Genesis{
		ChainID:   "synthos-test",
		TxChainID: 20260702,
		Alloc: map[Address]uint64{
			bridgeAuthority: 1_000_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata := []KeyValuePair{
		{Key: "type", Value: "bridge_release_native"},
		{Key: "source_chain_id", Value: "84532"},
		{Key: "source_event_id", Value: "0xsource-lock-1"},
	}
	first := signedBridgeTx(t, c, priv, bridgeAuthority, recipient, 10_000, metadata)
	if err := c.SubmitTx(first); err != nil {
		t.Fatalf("submit first release: %v", err)
	}
	block, err := c.BuildBlock("validator-1", "proof", 10)
	if err != nil {
		t.Fatalf("build first block: %v", err)
	}
	if err := c.FinalizeBlock(block); err != nil {
		t.Fatalf("finalize first release: %v", err)
	}
	if got := c.State.Get(recipient).Balance; got != 10_000 {
		t.Fatalf("recipient balance=%d want 10000", got)
	}

	replay := signedBridgeTx(t, c, priv, bridgeAuthority, recipient, 10_000, metadata)
	if err := c.SubmitTx(replay); err != nil {
		t.Fatalf("submit replay to mempool: %v", err)
	}
	replayBlock, err := c.BuildBlock("validator-1", "proof", 10)
	if err != nil {
		t.Fatalf("build replay block: %v", err)
	}
	if len(replayBlock.Tx) != 0 {
		t.Fatalf("expected replayed bridge source event to be excluded, got %d txs", len(replayBlock.Tx))
	}
}

func hexString(pub ed25519.PublicKey) string {
	const alphabet = "0123456789abcdef"
	out := make([]byte, len(pub)*2)
	for i, b := range pub {
		out[i*2] = alphabet[b>>4]
		out[i*2+1] = alphabet[b&0x0f]
	}
	return string(out)
}
