package main

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
)

func TestDecodeBridgeLockedData(t *testing.T) {
	asset := "0000000000000000000000001111111111111111111111111111111111111111"
	sender := "0000000000000000000000002222222222222222222222222222222222222222"
	offset := wordHex(160)
	amount := wordHex(12345)
	nonce := wordHex(7)
	recipient := []byte("0x3333333333333333333333333333333333333333")
	data := "0x" + asset + sender + offset + amount + nonce + wordHex(uint64(len(recipient))) + rightPadHex(hex.EncodeToString(recipient), 32)

	gotAsset, gotSender, gotRecipient, gotAmount, err := decodeBridgeLockedData(data)
	if err != nil {
		t.Fatal(err)
	}
	if gotAsset != "0x1111111111111111111111111111111111111111" {
		t.Fatalf("asset=%s", gotAsset)
	}
	if gotSender != "0x2222222222222222222222222222222222222222" {
		t.Fatalf("sender=%s", gotSender)
	}
	if string(gotRecipient) != string(recipient) {
		t.Fatalf("recipient=%s", string(gotRecipient))
	}
	if gotAmount != 12345 {
		t.Fatalf("amount=%d", gotAmount)
	}
}

func TestProofFromBridgeLockedLog(t *testing.T) {
	recipient := []byte("0x3333333333333333333333333333333333333333")
	data := "0x" +
		"0000000000000000000000001111111111111111111111111111111111111111" +
		"0000000000000000000000002222222222222222222222222222222222222222" +
		wordHex(160) +
		wordHex(12345) +
		wordHex(7) +
		wordHex(uint64(len(recipient))) +
		rightPadHex(hex.EncodeToString(recipient), 32)
	log := evmLog{
		Topics: []string{
			bridgeLockedTopic,
			"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			wordTopic(84532),
			wordTopic(20260702),
		},
		Data:            data,
		BlockNumber:     "0x64",
		TransactionHash: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		LogIndex:        "0x2",
	}
	proof, err := proofFromBridgeLockedLog(log, 120, 12)
	if err != nil {
		t.Fatal(err)
	}
	if proof.SourceChainID != "84532" || proof.DestinationChainID != "20260702" {
		t.Fatalf("bad chain ids: %+v", proof)
	}
	if proof.Recipient != "0x3333333333333333333333333333333333333333" {
		t.Fatalf("recipient=%s", proof.Recipient)
	}
	if proof.Amount != 12345 {
		t.Fatalf("amount=%d", proof.Amount)
	}
	if proof.Confirmations != 21 {
		t.Fatalf("confirmations=%d", proof.Confirmations)
	}
	if proof.SourceEventID != strings.ToLower(log.TransactionHash+":0x2") {
		t.Fatalf("source event=%s", proof.SourceEventID)
	}
}

func TestDecodeSynBurnedData(t *testing.T) {
	recipient := []byte("0x3333333333333333333333333333333333333333")
	rawAmount := new(big.Int).Mul(big.NewInt(500), wrappedSynUnitScale) // 500 wrapped SYN, 18 decimals
	data := "0x" +
		wordHex(96) + // offset to nativeRecipient tail (3 head words = 96 bytes)
		wordHexBig(rawAmount) +
		wordHex(7) + // nonce
		wordHex(uint64(len(recipient))) +
		rightPadHex(hex.EncodeToString(recipient), 32)

	gotRecipient, gotAmount, gotNonce, err := decodeSynBurnedData(data)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotRecipient) != string(recipient) {
		t.Fatalf("recipient=%s", string(gotRecipient))
	}
	if gotAmount.Cmp(rawAmount) != 0 {
		t.Fatalf("amount=%s want %s", gotAmount.String(), rawAmount.String())
	}
	if gotNonce != 7 {
		t.Fatalf("nonce=%d", gotNonce)
	}
}

func TestNativeAmountFromWrappedSynUnits(t *testing.T) {
	exact := new(big.Int).Mul(big.NewInt(123), wrappedSynUnitScale)
	got, err := nativeAmountFromWrappedSynUnits(exact)
	if err != nil {
		t.Fatal(err)
	}
	if got != 123 {
		t.Fatalf("got=%d want 123", got)
	}

	fractional := new(big.Int).Add(exact, big.NewInt(1))
	if _, err := nativeAmountFromWrappedSynUnits(fractional); err == nil {
		t.Fatal("expected error for a non-whole-SYN burn amount, got nil")
	}

	if _, err := nativeAmountFromWrappedSynUnits(big.NewInt(0)); err == nil {
		t.Fatal("expected error for a zero burn amount, got nil")
	}

	// A realistic large amount (the entire 100B SYN supply) must not
	// overflow uint64 once converted back to native whole units, even
	// though the raw 18-decimal EVM value itself overflows uint64.
	wholeSupply := new(big.Int).Mul(big.NewInt(100_000_000_000), wrappedSynUnitScale)
	if wholeSupply.IsUint64() {
		t.Fatal("test assumption broken: expected the raw 18-decimal amount to overflow uint64")
	}
	got, err = nativeAmountFromWrappedSynUnits(wholeSupply)
	if err != nil {
		t.Fatal(err)
	}
	if got != 100_000_000_000 {
		t.Fatalf("got=%d want 100000000000", got)
	}
}

func TestProofFromSynBurnedLog(t *testing.T) {
	recipient := []byte("0x4444444444444444444444444444444444444444")
	rawAmount := new(big.Int).Mul(big.NewInt(250), wrappedSynUnitScale)
	data := "0x" +
		wordHex(96) +
		wordHexBig(rawAmount) +
		wordHex(1) +
		wordHex(uint64(len(recipient))) +
		rightPadHex(hex.EncodeToString(recipient), 32)

	log := evmLog{
		Topics: []string{
			synBurnedForNativeReleaseTopic,
			"0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", // burnId
			"0x0000000000000000000000005555555555555555555555555555555555555555", // sender
		},
		Data:            data,
		BlockNumber:     "0x64",
		TransactionHash: "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		LogIndex:        "0x3",
	}
	proof, err := proofFromSynBurnedLog(log, "8453", 120, 12)
	if err != nil {
		t.Fatal(err)
	}
	if proof.SourceChainID != "8453" {
		t.Fatalf("source chain id=%s", proof.SourceChainID)
	}
	if proof.Recipient != "0x4444444444444444444444444444444444444444" {
		t.Fatalf("recipient=%s", proof.Recipient)
	}
	if proof.Amount != 250 {
		t.Fatalf("amount=%d want 250 (native whole units, not raw 18-decimal wei)", proof.Amount)
	}
	if proof.AssetID != "syn" {
		t.Fatalf("asset id=%s", proof.AssetID)
	}
	if proof.Confirmations != 21 {
		t.Fatalf("confirmations=%d", proof.Confirmations)
	}
	wantSourceEventID := strings.ToLower(log.TransactionHash + ":0x3")
	if proof.SourceEventID != wantSourceEventID {
		t.Fatalf("source event=%s want %s", proof.SourceEventID, wantSourceEventID)
	}

	// Removed logs must never be treated as a valid release trigger.
	removed := log
	removed.Removed = true
	if _, err := proofFromSynBurnedLog(removed, "8453", 120, 12); err == nil {
		t.Fatal("expected error for a removed log, got nil")
	}
}

func wordHexBig(value *big.Int) string {
	b := value.Bytes()
	if len(b) > 32 {
		panic("value does not fit in a 32-byte ABI word")
	}
	return strings.Repeat("0", 64-len(hex.EncodeToString(b))) + hex.EncodeToString(b)
}

func wordHex(value uint64) string {
	return strings.Repeat("0", 64-len(hexNoPrefix(value))) + hexNoPrefix(value)
}

func wordTopic(value uint64) string {
	return "0x" + wordHex(value)
}

func hexNoPrefix(value uint64) string {
	const alphabet = "0123456789abcdef"
	if value == 0 {
		return "0"
	}
	var out []byte
	for value > 0 {
		out = append([]byte{alphabet[value&0xf]}, out...)
		value >>= 4
	}
	return string(out)
}

func rightPadHex(value string, wordBytes int) string {
	target := wordBytes * 2
	if len(value)%target == 0 {
		return value
	}
	return value + strings.Repeat("0", target-(len(value)%target))
}
