package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------
// Heartbeat nonce ordering (nonceLess). See its doc comment in main.go for
// the exact lexicographic-vs-numeric bug this guards against.
// -----------------------------------------------------------------------

func sendTestHeartbeat(t *testing.T, s *server, nodeID string, privateKey ed25519.PrivateKey, nonce string, wantStatus int) {
	t.Helper()
	timestamp := time.Now().UTC().Format(time.RFC3339)
	message := canonicalHeartbeatMessage(nodeID, 1, "tip-1", "state-1", timestamp, nonce)
	postJSON(t, s.handleAPINodeHeartbeat, "/api/nodes/heartbeat", map[string]any{
		"node_id":    nodeID,
		"height":     1,
		"tip":        "tip-1",
		"state_root": "state-1",
		"timestamp":  timestamp,
		"nonce":      nonce,
		"signature":  hex.EncodeToString(ed25519.Sign(privateKey, message)),
	}, wantStatus)
}

func newHardeningTestServer(t *testing.T, nodeID string) (*server, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{
		state: registryState{
			Peers:               map[string]peer{},
			Mailbox:             map[string][]mailboxMessage{},
			Contacts:            []contactMessage{},
			EarlyAccessPayments: map[string]earlyAccessPaymentIntent{},
		},
	}
	postJSON(t, s.handleAPINodeRegister, "/api/nodes/register", map[string]any{
		"publicId":   nodeID,
		"public_key": hex.EncodeToString(publicKey),
		"mode":       "public-validator",
	}, http.StatusOK)
	return s, privateKey
}

// TestHeartbeatNonceReplayRejectedAcrossDigitLengths guards against the
// exact bug the audit found: a captured, validly-signed heartbeat with a
// shorter numeral ("9") replayed after a longer one ("10") was already
// recorded used to bypass the replay check entirely, because plain string
// "<=" is lexicographic, not numeric ("9" > "10" character-by-character,
// since '9' > '1').
func TestHeartbeatNonceReplayRejectedAcrossDigitLengths(t *testing.T) {
	s, privateKey := newHardeningTestServer(t, "syn-nonce-order")

	// First heartbeat: LastNonce starts empty, so any nonce is accepted.
	sendTestHeartbeat(t, s, "syn-nonce-order", privateKey, "10", http.StatusOK)

	// Replaying an OLDER, shorter-digit-width nonce must now be rejected --
	// this is the exact case that slipped through pre-fix.
	sendTestHeartbeat(t, s, "syn-nonce-order", privateKey, "9", http.StatusBadRequest)

	// A genuinely newer nonce must still be accepted afterward.
	sendTestHeartbeat(t, s, "syn-nonce-order", privateKey, "11", http.StatusOK)

	status := getNodeStatus(t, s, "/api/nodes/syn-nonce-order/status")
	if status.Node.ValidHeartbeats != 2 {
		t.Fatalf("valid heartbeats = %d, want 2 (the replayed \"9\" must not have counted)", status.Node.ValidHeartbeats)
	}
}

// TestHeartbeatNonceAcceptsGrowingDigitWidth is the control: a legitimately
// increasing nonce sequence that crosses a digit-count boundary (as any
// long-running node's real timestamp-based nonce eventually will) must not
// be rejected as "non-increasing".
func TestHeartbeatNonceAcceptsGrowingDigitWidth(t *testing.T) {
	s, privateKey := newHardeningTestServer(t, "syn-nonce-growth")

	sendTestHeartbeat(t, s, "syn-nonce-growth", privateKey, "9", http.StatusOK)
	sendTestHeartbeat(t, s, "syn-nonce-growth", privateKey, "10", http.StatusOK) // crosses 1 -> 2 digits

	status := getNodeStatus(t, s, "/api/nodes/syn-nonce-growth/status")
	if status.Node.ValidHeartbeats != 2 {
		t.Fatalf("valid heartbeats = %d, want 2", status.Node.ValidHeartbeats)
	}
}

// TestHeartbeatNonceRealFormatOrdering exercises the actual fixed-width
// "<19-digit ms>-<8-digit counter>" shape real clients send
// (cmd/silentnode/main.go), across a millisecond boundary, to confirm the
// fix doesn't regress the format lexicographic comparison already handled
// correctly.
func TestHeartbeatNonceRealFormatOrdering(t *testing.T) {
	s, privateKey := newHardeningTestServer(t, "syn-nonce-real-format")

	sendTestHeartbeat(t, s, "syn-nonce-real-format", privateKey, "0000001700000000000-00000005", http.StatusOK)
	// Replay of an earlier counter at the same millisecond: rejected.
	sendTestHeartbeat(t, s, "syn-nonce-real-format", privateKey, "0000001700000000000-00000003", http.StatusBadRequest)
	// Next millisecond, counter reset: accepted.
	sendTestHeartbeat(t, s, "syn-nonce-real-format", privateKey, "0000001700000000001-00000000", http.StatusOK)

	status := getNodeStatus(t, s, "/api/nodes/syn-nonce-real-format/status")
	if status.Node.ValidHeartbeats != 2 {
		t.Fatalf("valid heartbeats = %d, want 2", status.Node.ValidHeartbeats)
	}
}

func TestNonceLessDirect(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"10", "9", false}, // 10 is not less than 9
		{"9", "10", true},  // 9 is less than 10
		{"9", "9", false},  // equal is not less
		{"09", "10", true}, // both digit-only, different length still compares numerically once padded? "09" len2 vs "10" len2 -> lexicographic: "09" < "10" true (matches numeric 9 < 10)
		{"0000001700000000000-00000005", "0000001700000000000-00000003", false},
		{"0000001700000000000-00000003", "0000001700000000000-00000005", true},
		{"abc", "abd", true}, // non-numeric fallback still works via strings.Compare
	}
	for _, c := range cases {
		if got := nonceLess(c.a, c.b); got != c.want {
			t.Errorf("nonceLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// -----------------------------------------------------------------------
// authorized(): constant-time secret comparison.
// -----------------------------------------------------------------------

func TestAuthorizedConstantTimeComparison(t *testing.T) {
	s := &server{secret: "correct-horse-battery-staple"}

	req := httptest.NewRequest(http.MethodPost, "/register", nil)
	req.Header.Set("X-Registry-Secret", "correct-horse-battery-staple")
	if !s.authorized(req) {
		t.Fatal("expected the correct secret to be authorized")
	}

	req = httptest.NewRequest(http.MethodPost, "/register", nil)
	req.Header.Set("X-Registry-Secret", "wrong")
	if s.authorized(req) {
		t.Fatal("expected an incorrect secret to be rejected")
	}

	// Different-length guess (a natural case for subtle.ConstantTimeCompare,
	// which requires matching operand length internally -- confirm it's
	// still handled safely rather than panicking or false-accepting).
	req = httptest.NewRequest(http.MethodPost, "/register", nil)
	req.Header.Set("X-Registry-Secret", "correct-horse-battery-staple-but-longer")
	if s.authorized(req) {
		t.Fatal("expected a longer, prefix-matching guess to be rejected")
	}

	req = httptest.NewRequest(http.MethodPost, "/register", nil)
	if s.authorized(req) {
		t.Fatal("expected a missing header to be rejected when a secret is configured")
	}

	// No secret configured at all: always authorized (unchanged behavior).
	open := &server{secret: ""}
	req = httptest.NewRequest(http.MethodPost, "/register", nil)
	if !open.authorized(req) {
		t.Fatal("expected an unconfigured secret to leave the endpoint open, as before")
	}
}

// -----------------------------------------------------------------------
// Rate limiting and body size caps.
// -----------------------------------------------------------------------

func TestRegistryRateLimiterAllowsThenBlocksBursts(t *testing.T) {
	rl := newRegistryRateLimiter()
	allowed := 0
	for i := 0; i < registryRateBurst; i++ {
		if rl.allow("203.0.113.1") {
			allowed++
		}
	}
	if allowed != registryRateBurst {
		t.Fatalf("expected all %d burst-capacity requests to be allowed, got %d", registryRateBurst, allowed)
	}
	if rl.allow("203.0.113.1") {
		t.Fatal("expected the request beyond burst capacity to be rate-limited")
	}
	// A different client IP has its own, unaffected bucket.
	if !rl.allow("203.0.113.2") {
		t.Fatal("expected a different client IP to have its own untouched bucket")
	}
}

func TestLimitRequestMiddlewareEnforcesRateLimit(t *testing.T) {
	rl := newRegistryRateLimiter()
	calls := 0
	handler := limitRequest(rl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	remote := net.JoinHostPort("198.51.100.7", "5555")
	for i := 0; i < registryRateBurst; i++ {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status after exhausting burst = %d, want 429", rec.Code)
	}
	if calls != registryRateBurst {
		t.Fatalf("handler was called %d times, want exactly %d (the 429 must be rejected before reaching it)", calls, registryRateBurst)
	}
}

func TestLimitRequestMiddlewareCapsBodySize(t *testing.T) {
	rl := newRegistryRateLimiter()
	handler := limitRequest(rl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	oversized := strings.Repeat("a", registryMaxBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(oversized))
	req.RemoteAddr = net.JoinHostPort("198.51.100.8", "5555")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status for an oversized body = %d, want 400 (rejected by http.MaxBytesReader)", rec.Code)
	}

	underSized := strings.Repeat("a", 16)
	req = httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(underSized))
	req.RemoteAddr = net.JoinHostPort("198.51.100.9", "5555")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status for a normal-sized body = %d, want 200", rec.Code)
	}
}

// -----------------------------------------------------------------------
// Payment-intent ID entropy.
// -----------------------------------------------------------------------

func TestPaymentIntentIDHasHighEntropyAndIsUnique(t *testing.T) {
	id := paymentIntentID()
	if len(id) != 32 { // 16 random bytes, hex-encoded
		t.Fatalf("paymentIntentID() length = %d, want 32 (16 bytes hex-encoded, 128 bits of entropy)", len(id))
	}
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		next := paymentIntentID()
		if seen[next] {
			t.Fatalf("paymentIntentID() produced a duplicate (%q) within 1000 calls", next)
		}
		seen[next] = true
	}
}
