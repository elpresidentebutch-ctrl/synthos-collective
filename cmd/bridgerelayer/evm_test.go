package main

import (
	"encoding/hex"
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
