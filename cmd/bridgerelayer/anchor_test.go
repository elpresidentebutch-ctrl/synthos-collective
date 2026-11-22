package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEncodeAnchorCalldata(t *testing.T) {
	bHash := "0x1111111111111111111111111111111111111111111111111111111111111111"
	aHash := "0x2222222222222222222222222222222222222222222222222222222222222222"
	height := uint64(142500)
	blockTime := uint64(1732251600)

	calldata, err := EncodeAnchorCalldata(height, bHash, aHash, blockTime)
	if err != nil {
		t.Fatalf("unexpected error encoding calldata: %v", err)
	}

	// 0x (2) + 4 bytes selector (8) + 32 bytes height (64) + 32 bytes hash (64) + 32 bytes appHash (64) + 32 bytes blockTime (64) = 266
	if len(calldata) != 2+8+64+64+64+64 {
		t.Fatalf("calldata length = %d, want %d", len(calldata), 266)
	}

	if !strings.HasPrefix(calldata, "0x"+anchorMethodSelector) {
		t.Fatalf("expected selector prefix 0x%s, got %s", anchorMethodSelector, calldata[:10])
	}

	// Test error on short hash
	if _, err := EncodeAnchorCalldata(height, "0x1234", aHash, blockTime); err == nil {
		t.Fatal("expected error on invalid blockHash length")
	}
	if _, err := EncodeAnchorCalldata(height, bHash, "0x5678", blockTime); err == nil {
		t.Fatal("expected error on invalid appHash length")
	}
}

func TestComputeFingerprint(t *testing.T) {
	fp1 := ComputeFingerprint(100, "0xaaaa", "0xbbbb", 1700000000)
	fp2 := ComputeFingerprint(100, "0xAAAA", "0xBBBB", 1700000000)
	if fp1 != fp2 {
		t.Fatalf("ComputeFingerprint should be case-insensitive: %s != %s", fp1, fp2)
	}

	fp3 := ComputeFingerprint(101, "0xaaaa", "0xbbbb", 1700000000)
	if fp1 == fp3 {
		t.Fatal("ComputeFingerprint should vary by block height")
	}

	fp4 := ComputeFingerprint(100, "0xaaaa", "0xbbbb", 1700000001)
	if fp1 == fp4 {
		t.Fatal("ComputeFingerprint should vary by block time")
	}
}

func TestSimulateAnchorSubmissionAndVerifyBaseNetwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			ID     int    `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		switch req.Method {
		case "eth_chainId":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x2105"}`)) // 8453 (Base mainnet) in hex
		case "eth_call":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x0000000000000000000000000000000000000000000000000000000000000001"}`))
		default:
			http.Error(w, "unknown method", 404)
		}
	}))
	defer server.Close()

	chainID, err := VerifyBaseNetwork(server.URL)
	if err != nil {
		t.Fatalf("VerifyBaseNetwork failed for Base mainnet: %v", err)
	}
	if chainID != BaseMainnetChainID {
		t.Fatalf("got chainID %d, want %d", chainID, BaseMainnetChainID)
	}

	bHash := "1111111111111111111111111111111111111111111111111111111111111111"
	aHash := "2222222222222222222222222222222222222222222222222222222222222222"
	ok, fp, err := SimulateAnchorSubmission(server.URL, BaseMainnetAnchorContract, 1000, bHash, aHash, 1732251600, "0x1234567890123456789012345678901234567890")
	if err != nil {
		t.Fatalf("SimulateAnchorSubmission failed: %v", err)
	}
	if !ok || fp == "" {
		t.Fatalf("expected simulation success, got ok=%v fp=%s", ok, fp)
	}
}
