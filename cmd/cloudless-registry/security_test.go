package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newSecurityTestServer builds a bare in-memory server with all the maps
// verifyPaymentIntent/handleRegister expect to already be non-nil (mirrors
// what load()/main() do for a real deployment). stateFile is left empty, so
// persist() is a no-op and these tests never touch disk.
func newSecurityTestServer() *server {
	return &server{
		state: registryState{
			Peers:                   map[string]peer{},
			Mailbox:                 map[string][]mailboxMessage{},
			Contacts:                []contactMessage{},
			EarlyAccessPayments:     map[string]earlyAccessPaymentIntent{},
			ConsumedPaymentTxHashes: map[string]string{},
		},
	}
}

const testTxHash = "0xab12cd34ab12cd34ab12cd34ab12cd34ab12cd34ab12cd34ab12cd34ab12cd34"

func init() {
	if !isHexHash(testTxHash) {
		panic("testTxHash fixture must be a valid 0x-prefixed 32-byte hex hash")
	}
}

// ---------------------------------------------------------------------
// Task #26a: payment-intent txHash replay across separately-created
// intents. Before this fix, verifyPaymentIntent had no notion that a
// txHash had already been spent, so the same real on-chain payment could
// be submitted against any number of intents and each would independently
// mint its own SynAmount of SYN.
// ---------------------------------------------------------------------

func TestReservePaymentTxHash_BlocksReuseAcrossIntents(t *testing.T) {
	s := newSecurityTestServer()

	if err := s.reservePaymentTxHash(testTxHash, "intent-a"); err != nil {
		t.Fatalf("first reservation for intent-a should succeed: %v", err)
	}
	if err := s.reservePaymentTxHash(testTxHash, "intent-b"); err == nil {
		t.Fatal("expected reservation for a different intent to be rejected while intent-a still holds this txHash")
	}
	// A retry by the SAME intent that already holds it must still succeed
	// (e.g. re-verifying after allocateNativeSYN failed and left the
	// intent at "allocation_pending").
	if err := s.reservePaymentTxHash(testTxHash, "intent-a"); err != nil {
		t.Fatalf("re-reservation by the same intent should succeed: %v", err)
	}

	// A stale release from intent-b (which never actually held the
	// reservation) must not free intent-a's hold.
	s.releasePaymentTxHash(testTxHash, "intent-b")
	if err := s.reservePaymentTxHash(testTxHash, "intent-b"); err == nil {
		t.Fatal("a no-op release from a non-owning intent must not free the reservation")
	}

	// The real owner releasing it (e.g. after a failed verification) frees
	// it for reuse.
	s.releasePaymentTxHash(testTxHash, "intent-a")
	if err := s.reservePaymentTxHash(testTxHash, "intent-b"); err != nil {
		t.Fatalf("after the owning intent releases it, another intent should be able to reserve it: %v", err)
	}
}

func TestVerifyPaymentIntent_AlreadyAllocatedIsIdempotent(t *testing.T) {
	s := newSecurityTestServer()
	intent := earlyAccessPaymentIntent{
		ID:             "intent-done",
		Status:         "syn_allocated",
		AssetSymbol:    "USDC",
		PaymentAmount:  "1000000",
		PaymentAddress: "0x5d6f8fbaab199e788ed9cfcb3f7fe2ac9c0450d2",
		TxHash:         testTxHash,
		SynthosTxID:    "already-minted-tx",
	}
	s.savePaymentIntent(intent)

	body, _ := json.Marshal(map[string]string{"txHash": testTxHash})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/early-access/payment-intents/intent-done/verify", bytes.NewReader(body))
	s.verifyPaymentIntent(w, r, "intent-done")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for an already-allocated intent, got %d: %s", w.Code, w.Body.String())
	}
	if _, reserved := s.state.ConsumedPaymentTxHashes[testTxHash]; reserved {
		t.Fatal("re-verifying an already-allocated intent must not touch the txHash reservation table")
	}
	got, _ := s.getPaymentIntent("intent-done")
	if got.SynthosTxID != "already-minted-tx" {
		t.Fatalf("idempotent re-verify must not change the recorded mint tx, got %q", got.SynthosTxID)
	}
}

func TestVerifyPaymentIntent_RejectsTxHashAlreadyConsumedByAnotherIntent(t *testing.T) {
	s := newSecurityTestServer()
	// Simulate a txHash that a prior, successful verification already
	// consumed on behalf of a different intent.
	s.state.ConsumedPaymentTxHashes[testTxHash] = "intent-original"
	s.savePaymentIntent(earlyAccessPaymentIntent{
		ID:             "intent-original",
		Status:         "syn_allocated",
		AssetSymbol:    "USDC",
		PaymentAmount:  "1000000",
		PaymentAddress: "0x5d6f8fbaab199e788ed9cfcb3f7fe2ac9c0450d2",
		TxHash:         testTxHash,
	})
	s.savePaymentIntent(earlyAccessPaymentIntent{
		ID:             "intent-replay-attempt",
		Status:         "pending",
		AssetSymbol:    "USDC",
		PaymentAmount:  "1000000",
		PaymentAddress: "0x5d6f8fbaab199e788ed9cfcb3f7fe2ac9c0450d2",
	})

	body, _ := json.Marshal(map[string]string{"txHash": testTxHash})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/early-access/payment-intents/intent-replay-attempt/verify", bytes.NewReader(body))
	s.verifyPaymentIntent(w, r, "intent-replay-attempt")

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 replaying a consumed txHash against a different intent, got %d: %s", w.Code, w.Body.String())
	}
	replay, _ := s.getPaymentIntent("intent-replay-attempt")
	if replay.Status == "syn_allocated" {
		t.Fatal("the replay attempt must not have been allocated any SYN")
	}
}

func TestVerifyPaymentIntent_FailedVerificationReleasesTxHashForRetry(t *testing.T) {
	s := newSecurityTestServer()
	// AssetSymbol "NOPE" doesn't match any configured asset, so this fails
	// deterministically at the asset-lookup step, before any network call
	// -- the reservation it took must be released so a genuine retry (or a
	// correction to the right intent) isn't blocked forever by a failed
	// attempt.
	s.savePaymentIntent(earlyAccessPaymentIntent{
		ID:             "intent-bad-asset",
		Status:         "pending",
		AssetSymbol:    "NOPE",
		PaymentAmount:  "1000000",
		PaymentAddress: "0x5d6f8fbaab199e788ed9cfcb3f7fe2ac9c0450d2",
	})

	body, _ := json.Marshal(map[string]string{"txHash": testTxHash})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/early-access/payment-intents/intent-bad-asset/verify", bytes.NewReader(body))
	s.verifyPaymentIntent(w, r, "intent-bad-asset")

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for a missing asset config, got %d: %s", w.Code, w.Body.String())
	}
	if _, reserved := s.state.ConsumedPaymentTxHashes[testTxHash]; reserved {
		t.Fatal("a failed verification attempt must release its txHash reservation")
	}
}

// ---------------------------------------------------------------------
// Task #26b: node identity takeover via /register. Before this fix,
// handleRegister had no authentication at all, and even with the shared
// secret gate alone, any caller holding that secret could silently
// reassign an existing peer name to a different public key.
// ---------------------------------------------------------------------

func doRegister(t *testing.T, s *server, secretHeader string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	if secretHeader != "" {
		r.Header.Set("X-Registry-Secret", secretHeader)
	}
	w := httptest.NewRecorder()
	s.handleRegister(w, r)
	return w
}

func TestHandleRegister_RequiresSecretWhenConfigured(t *testing.T) {
	s := newSecurityTestServer()
	s.secret = "top-secret"

	w := doRegister(t, s, "", map[string]any{"name": "syn-validator-1", "public_key": "aaaa"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 registering without the configured secret, got %d: %s", w.Code, w.Body.String())
	}
	if _, exists := s.state.Peers["syn-validator-1"]; exists {
		t.Fatal("an unauthorized registration must not be stored")
	}
}

func TestHandleRegister_RejectsReassigningAnExistingNameToADifferentKey(t *testing.T) {
	s := newSecurityTestServer()

	w := doRegister(t, s, "", map[string]any{
		"name":       "syn-validator-1",
		"url":        "https://validator-1.example.com",
		"public_key": "0xaaaaaaaaaaaaaaaa",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("initial registration should succeed, got %d: %s", w.Code, w.Body.String())
	}

	// An impostor (or anyone else who can reach /register) tries to
	// silently take over the name with a different key.
	w = doRegister(t, s, "", map[string]any{
		"name":       "syn-validator-1",
		"url":        "https://attacker.example.com",
		"public_key": "0xbbbbbbbbbbbbbbbb",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 reassigning an existing name to a different public key, got %d: %s", w.Code, w.Body.String())
	}

	entry := s.state.Peers["syn-validator-1"]
	if entry.PublicKey != "0xaaaaaaaaaaaaaaaa" || entry.URL != "https://validator-1.example.com" {
		t.Fatalf("the original registration must be left untouched, got %+v", entry)
	}
}

func TestHandleRegister_AllowsReRegistrationWithTheSameKey(t *testing.T) {
	s := newSecurityTestServer()

	doRegister(t, s, "", map[string]any{
		"name":       "syn-validator-1",
		"url":        "https://validator-1.example.com",
		"public_key": "0xaaaaaaaaaaaaaaaa",
	})
	// A real heartbeat re-registration: same name, same key, refreshed url.
	w := doRegister(t, s, "", map[string]any{
		"name":       "syn-validator-1",
		"url":        "https://validator-1.example.com:8090",
		"public_key": "0xaaaaaaaaaaaaaaaa",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("re-registering with the same key should succeed, got %d: %s", w.Code, w.Body.String())
	}
	if s.state.Peers["syn-validator-1"].URL != "https://validator-1.example.com:8090" {
		t.Fatal("re-registration with a matching key should still update mutable fields like url")
	}
}

func TestHandleRegister_AllowsClaimingANameThatHasNoKeyYet(t *testing.T) {
	s := newSecurityTestServer()
	// A legacy/never-keyed entry has no identity to protect yet.
	s.state.Peers["syn-legacy"] = peer{Name: "syn-legacy", URL: "https://legacy.example.com"}

	w := doRegister(t, s, "", map[string]any{
		"name":       "syn-legacy",
		"url":        "https://legacy.example.com",
		"public_key": "0xcccccccccccccccc",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("claiming a never-keyed name should succeed, got %d: %s", w.Code, w.Body.String())
	}
	if s.state.Peers["syn-legacy"].PublicKey != "0xcccccccccccccccc" {
		t.Fatal("the new key should now be bound to the name")
	}
}
