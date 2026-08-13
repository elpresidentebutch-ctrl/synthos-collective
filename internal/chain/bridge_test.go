package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

func TestBridgeReleaseRequiresValidatorQuorumWhenConfigured(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	v1pub, v1priv, _ := ed25519.GenerateKey(rand.Reader)
	v2pub, v2priv, _ := ed25519.GenerateKey(rand.Reader)
	v3pub, _, _ := ed25519.GenerateKey(rand.Reader)
	bridgeAuthority := AddressFromPublicKey(pub)
	recipient := Address("0x3333333333333333333333333333333333333333")
	sourceChainID := "84532"
	sourceEventID := "0xsource-lock-quorum"
	amount := uint64(10_000)
	c, err := NewChain(Genesis{
		ChainID:   "synthos-test",
		TxChainID: 20260702,
		Alloc: map[Address]uint64{
			bridgeAuthority: 1_000_000,
		},
		Metadata: map[string]any{
			"bridge_quorum": float64(2),
			"bridge_validators": []any{
				map[string]any{"id": "validator-1", "public_key": "0x" + hex.EncodeToString(v1pub)},
				map[string]any{"id": "validator-2", "public_key": "0x" + hex.EncodeToString(v2pub)},
				map[string]any{"id": "validator-3", "public_key": "0x" + hex.EncodeToString(v3pub)},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	noProof := signedBridgeTx(t, c, priv, bridgeAuthority, recipient, amount, releaseMetadata(sourceChainID, sourceEventID, nil))
	if err := c.SubmitTx(noProof); err != nil {
		t.Fatalf("submit no-proof release: %v", err)
	}
	noProofBlock, err := c.BuildBlock("validator-1", "proof", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(noProofBlock.Tx) != 0 {
		t.Fatalf("expected no-proof release excluded, got %d tx", len(noProofBlock.Tx))
	}

	oneSig := []BridgeValidatorSignature{
		signBridgeRelease("validator-1", v1priv, sourceChainID, sourceEventID, recipient, "syn", amount),
	}
	oneProof := signedBridgeTx(t, c, priv, bridgeAuthority, recipient, amount, releaseMetadata(sourceChainID, sourceEventID, oneSig))
	if err := c.SubmitTx(oneProof); err != nil {
		t.Fatalf("submit one-proof release: %v", err)
	}
	oneProofBlock, err := c.BuildBlock("validator-1", "proof", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(oneProofBlock.Tx) != 0 {
		t.Fatalf("expected one-signature release excluded, got %d tx", len(oneProofBlock.Tx))
	}

	twoSigs := []BridgeValidatorSignature{
		signBridgeRelease("validator-1", v1priv, sourceChainID, sourceEventID, recipient, "syn", amount),
		signBridgeRelease("validator-2", v2priv, sourceChainID, sourceEventID, recipient, "syn", amount),
	}
	valid := signedBridgeTx(t, c, priv, bridgeAuthority, recipient, amount, releaseMetadata(sourceChainID, sourceEventID, twoSigs))
	if err := c.SubmitTx(valid); err != nil {
		t.Fatalf("submit valid release: %v", err)
	}
	validBlock, err := c.BuildBlock("validator-1", "proof", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(validBlock.Tx) != 1 {
		t.Fatalf("expected quorum release included, got %d tx", len(validBlock.Tx))
	}
	if err := c.FinalizeBlock(validBlock); err != nil {
		t.Fatal(err)
	}
	if got := c.State.Get(recipient).Balance; got != amount {
		t.Fatalf("recipient balance=%d want %d", got, amount)
	}
}

func releaseMetadata(sourceChainID string, sourceEventID string, signatures []BridgeValidatorSignature) []KeyValuePair {
	out := []KeyValuePair{
		{Key: "type", Value: "bridge_release_native"},
		{Key: "source_chain_id", Value: sourceChainID},
		{Key: "source_event_id", Value: sourceEventID},
	}
	if signatures != nil {
		data, _ := json.Marshal(signatures)
		out = append(out, KeyValuePair{Key: "validator_signatures", Value: string(data)})
	}
	return out
}

func signBridgeRelease(id string, priv ed25519.PrivateKey, sourceChainID string, sourceEventID string, recipient Address, assetID string, amount uint64) BridgeValidatorSignature {
	message := BridgeReleaseSigningMessage(sourceChainID, sourceEventID, string(recipient), assetID, amount)
	return BridgeValidatorSignature{
		ValidatorID: id,
		Signature:   "0x" + hex.EncodeToString(ed25519.Sign(priv, message)),
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
