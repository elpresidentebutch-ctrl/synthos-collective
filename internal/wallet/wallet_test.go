package wallet

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synthos-collective/internal/chain"
)

func TestWallet_GenerationAndAddresses(t *testing.T) {
	w, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	addr, err := w.Address()
	if err != nil {
		t.Fatalf("Address() failed: %v", err)
	}
	if len(addr) != 42 || addr[:2] != "0x" {
		t.Fatalf("unexpected address format: %q", addr)
	}

	pub, err := w.PublicKeyHex()
	if err != nil {
		t.Fatalf("PublicKeyHex() failed: %v", err)
	}
	if len(pub) != 66 || pub[:2] != "0x" {
		t.Fatalf("unexpected public key format: %q", pub)
	}

	priv, err := w.PrivateKeyHex()
	if err != nil {
		t.Fatalf("PrivateKeyHex() failed: %v", err)
	}
	if len(priv) != 130 || priv[:2] != "0x" {
		t.Fatalf("unexpected private key format: %q", priv)
	}

	seed, err := w.SeedHex()
	if err != nil {
		t.Fatalf("SeedHex() failed: %v", err)
	}
	if len(seed) != 66 || seed[:2] != "0x" {
		t.Fatalf("unexpected seed format: %q", seed)
	}

	// Reconstruct from full private key
	w2, err := FromPrivateKeyHex(priv)
	if err != nil {
		t.Fatalf("FromPrivateKeyHex(full) failed: %v", err)
	}
	addr2, _ := w2.Address()
	if addr != addr2 {
		t.Fatalf("address mismatch: %s vs %s", addr, addr2)
	}

	// Reconstruct from 32-byte seed
	w3, err := FromPrivateKeyHex(seed)
	if err != nil {
		t.Fatalf("FromPrivateKeyHex(seed) failed: %v", err)
	}
	addr3, _ := w3.Address()
	if addr != addr3 {
		t.Fatalf("address mismatch from seed: %s vs %s", addr, addr3)
	}
}

func TestWallet_KeystoreEncryption(t *testing.T) {
	w, err := New()
	if err != nil {
		t.Fatal(err)
	}
	addr, _ := w.Address()

	pass := "synthos-secret-passphrase-2026"
	ks, err := w.Encrypt(pass)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if ks.Address != string(addr) {
		t.Fatalf("keystore address = %q, want %q", ks.Address, addr)
	}
	if ks.Crypto.Cipher != "aes-256-gcm" {
		t.Fatalf("keystore cipher = %q, want aes-256-gcm", ks.Crypto.Cipher)
	}

	// Decrypt with correct password
	restored, err := DecryptKeystore(ks, pass)
	if err != nil {
		t.Fatalf("DecryptKeystore failed: %v", err)
	}
	restoredAddr, _ := restored.Address()
	if restoredAddr != addr {
		t.Fatalf("restored address mismatch: %s vs %s", restoredAddr, addr)
	}

	// Decrypt with wrong password
	_, err = DecryptKeystore(ks, "wrong-password")
	if err != ErrInvalidPassphrase {
		t.Fatalf("expected ErrInvalidPassphrase, got: %v", err)
	}
}

func TestWallet_KeystoreFilePersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "synthos-wallet-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test-keystore.json")
	w, err := New()
	if err != nil {
		t.Fatal(err)
	}
	pass := "my-secure-password"

	if err := w.SaveKeystore(filePath, pass); err != nil {
		t.Fatalf("SaveKeystore failed: %v", err)
	}

	loaded, err := LoadKeystore(filePath, pass)
	if err != nil {
		t.Fatalf("LoadKeystore failed: %v", err)
	}

	wAddr, _ := w.Address()
	loadedAddr, _ := loaded.Address()
	if loadedAddr != wAddr {
		t.Fatalf("loaded address mismatch: %s vs %s", loadedAddr, wAddr)
	}
}

func TestWallet_TransactionSigningAndVerification(t *testing.T) {
	w, err := New()
	if err != nil {
		t.Fatal(err)
	}
	wAddr, _ := w.Address()

	recipient := chain.Address("0x25151dd517ef19003d8feb50a501d8a096b7f597")
	chainID := uint64(20260702)

	tx, err := w.CreateTransferTx(recipient, 1000, 10, 0, chainID)
	if err != nil {
		t.Fatalf("CreateTransferTx failed: %v", err)
	}

	if tx.From != wAddr {
		t.Fatalf("tx.From = %s, want %s", tx.From, wAddr)
	}
	if tx.To != recipient {
		t.Fatalf("tx.To = %s, want %s", tx.To, recipient)
	}
	if tx.Amount != 1000 || tx.Fee != 10 {
		t.Fatalf("unexpected tx amount/fee: %d / %d", tx.Amount, tx.Fee)
	}
	if tx.Signature == "" || tx.ID == "" {
		t.Fatalf("missing tx signature or ID")
	}

	// Verify the signed transaction using chain rules
	if err := tx.Verify(); err != nil {
		t.Fatalf("tx.Verify() failed: %v", err)
	}
}

func TestWallet_ClientRPC(t *testing.T) {
	w, _ := New()
	wAddr, _ := w.Address()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"chain_id":    "synthos-mainnet-1",
				"tx_chain_id": 20260702,
				"height":      300000,
				"engine":      "cometbft",
			})
		case "/account":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"address": string(wAddr),
				"balance": 50000,
				"nonce":   3,
			})
		case "/balance":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"address": string(wAddr),
				"balance": 50000,
			})
		case "/submitTx":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"tx_id":   "0xmocktxid",
				"tx_hash": hex.EncodeToString([]byte("mockhash")),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	ctx := context.Background()

	// Test GetStatus
	status, err := client.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.ChainID != "synthos-mainnet-1" || status.Height != 300000 {
		t.Fatalf("unexpected status: %+v", status)
	}

	// Test GetAccount
	acc, err := client.GetAccount(ctx, wAddr)
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}
	if acc.Balance != 50000 || acc.Nonce != 3 {
		t.Fatalf("unexpected account: %+v", acc)
	}

	// Test GetBalance
	bal, err := client.GetBalance(ctx, wAddr)
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}
	if bal != 50000 {
		t.Fatalf("unexpected balance: %d", bal)
	}

	// Test BroadcastTx
	tx, err := w.CreateTransferTx("0x1111111111111111111111111111111111111111", 100, 1, 0, 20260702)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.BroadcastTx(ctx, tx)
	if err != nil {
		t.Fatalf("BroadcastTx failed: %v", err)
	}
	if !res.OK {
		t.Fatalf("broadcast not ok: %+v", res)
	}
}

func TestWallet_BIP39_Generation(t *testing.T) {
	for _, count := range []int{12, 15, 24} {
		m, err := NewMnemonic(count)
		if err != nil {
			t.Fatalf("NewMnemonic(%d) failed: %v", count, err)
		}
		words := len(splitWords(m))
		if words != count {
			t.Fatalf("expected %d words, got %d: %q", count, words, m)
		}
		if err := ValidateMnemonic(m); err != nil {
			t.Fatalf("ValidateMnemonic failed on generated %d-word phrase: %v", count, err)
		}

		// Wallet from mnemonic
		w, err := FromMnemonic(m, "")
		if err != nil {
			t.Fatalf("FromMnemonic failed on %d-word phrase: %v", count, err)
		}
		addr, err := w.Address()
		if err != nil || len(addr) != 42 {
			t.Fatalf("invalid address %q derived from %d-word phrase", addr, count)
		}
	}

	// Test invalid word count
	if _, err := NewMnemonic(13); err != ErrInvalidWordCount {
		t.Fatalf("expected ErrInvalidWordCount for 13 words, got: %v", err)
	}
}

func TestWallet_BIP39_DeterministicVectors(t *testing.T) {
	phrase := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	expectedSeedHex := "0x560f9f3c94558b6551928bb781cf6092c6b8800b4fc544af2c9444ed126d51aa"
	expectedPubHex := "0xe96b1c6b8769fdb0b34fbecfdf85c33b053cecad9517e1ab88cba614335775c1"
	expectedAddr := "0xada8e3423e041d247dca60598e6d3d8834161fe5"

	w, err := FromMnemonic(phrase, "")
	if err != nil {
		t.Fatalf("FromMnemonic failed: %v", err)
	}

	seedHex, err := w.SeedHex()
	if err != nil {
		t.Fatalf("SeedHex() failed: %v", err)
	}
	if seedHex != expectedSeedHex {
		t.Fatalf("seed mismatch:\n got:  %s\n want: %s", seedHex, expectedSeedHex)
	}

	pubHex, err := w.PublicKeyHex()
	if err != nil {
		t.Fatalf("PublicKeyHex() failed: %v", err)
	}
	if pubHex != expectedPubHex {
		t.Fatalf("public key mismatch:\n got:  %s\n want: %s", pubHex, expectedPubHex)
	}

	addr, err := w.Address()
	if err != nil {
		t.Fatalf("Address() failed: %v", err)
	}
	if string(addr) != expectedAddr {
		t.Fatalf("address mismatch:\n got:  %s\n want: %s", addr, expectedAddr)
	}

	// Corrupted mnemonic checksum detection
	corrupted := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon"
	if err := ValidateMnemonic(corrupted); err == nil {
		t.Fatalf("expected checksum error on invalid mnemonic, got nil")
	}
}

func splitWords(s string) []string {
	return strings.Fields(s)
}

