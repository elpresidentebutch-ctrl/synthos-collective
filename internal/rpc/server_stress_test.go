package rpc_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/rpc"
)

// newStressServer creates a test RPC server backed by a fresh in-memory chain.
// rps sets the per-IP rate limit (use a large value to avoid throttling in
// throughput tests; use a small value to test the limiter itself).
func newStressServer(t *testing.T, rps int) (*httptest.Server, *chain.Chain) {
	t.Helper()
	gen := chain.Genesis{
		ChainID: "rpc-stress",
		Alloc:   map[chain.Address]uint64{chain.Address("0xdeadbeef"): 1_000_000_000},
	}
	c, err := chain.NewChain(gen)
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	srv := rpc.NewServerWithConfig(c, nil, nil, rps, 1024*1024)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, c
}

// genKeypair generates an ed25519 keypair for tests.
func genKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return pub, priv
}

// buildSignedTx creates and signs a transaction ready for submission.
func buildSignedTx(t *testing.T, pub ed25519.PublicKey, priv ed25519.PrivateKey,
	to chain.Address, nonce uint64) chain.Tx {
	t.Helper()
	fromAddr := chain.AddressFromPublicKey(pub)
	tx := chain.Tx{
		ChainID:   1,
		From:      fromAddr,
		To:        to,
		Amount:    100,
		Fee:       1,
		Nonce:     nonce,
		PublicKey: "0x" + hex.EncodeToString(pub),
	}
	if err := tx.Sign(priv); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return tx
}

// --- Rate-limiter unit stress tests ---

// TestStress_TokenBucket_ExhaustAndRefill verifies burst consumption and
// token refill behaviour of the underlying token bucket.
func TestStress_TokenBucket_ExhaustAndRefill(t *testing.T) {
	const capacity = 50
	tb := rpc.NewTokenBucket(capacity, float64(capacity))

	allowed := 0
	for i := 0; i < capacity*2; i++ {
		if tb.Allow() {
			allowed++
		}
	}
	if allowed != capacity {
		t.Fatalf("burst: expected %d allowed, got %d", capacity, allowed)
	}

	// Wait ~110 ms — at 50 tokens/s that refills ~5 tokens.
	time.Sleep(110 * time.Millisecond)
	refilled := 0
	for i := 0; i < capacity; i++ {
		if tb.Allow() {
			refilled++
		}
	}
	if refilled < 3 {
		t.Fatalf("refill: expected ≥3 tokens after 110 ms, got %d", refilled)
	}
	t.Logf("✅ TokenBucket: burst=%d refilled≥%d after 110 ms", allowed, refilled)
}

// TestStress_RateLimiter_ConcurrentSingleIP floods a single-IP bucket from many
// goroutines and checks that the allowed count never exceeds the burst capacity.
func TestStress_RateLimiter_ConcurrentSingleIP(t *testing.T) {
	const capacity = 100
	rl := rpc.NewRateLimiter(capacity)
	defer rl.Close()

	const total = capacity * 3
	var allowed int64
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rl.Allow("192.0.2.1") {
				atomic.AddInt64(&allowed, 1)
			}
		}()
	}
	wg.Wait()

	if allowed > capacity {
		t.Fatalf("allowed %d > capacity %d (concurrent over-counting)", allowed, capacity)
	}
	t.Logf("✅ ConcurrentSingleIP: allowed=%d/%d (capacity=%d)", allowed, total, capacity)
}

// TestStress_RateLimiter_ConcurrentMultipleIPs checks that each unique client IP
// gets its own independent token bucket under concurrent load.
func TestStress_RateLimiter_ConcurrentMultipleIPs(t *testing.T) {
	const (
		capacity = 20
		numIPs   = 50
		reqPerIP = 30
	)
	rl := rpc.NewRateLimiter(capacity)
	defer rl.Close()

	allowed := make([]int64, numIPs)
	var wg sync.WaitGroup
	for ip := 0; ip < numIPs; ip++ {
		clientIP := fmt.Sprintf("10.0.%d.%d", ip/255, ip%255)
		for r := 0; r < reqPerIP; r++ {
			ipIdx := ip
			wg.Add(1)
			go func(ipStr string, idx int) {
				defer wg.Done()
				if rl.Allow(ipStr) {
					atomic.AddInt64(&allowed[idx], 1)
				}
			}(clientIP, ipIdx)
		}
	}
	wg.Wait()

	for ip := 0; ip < numIPs; ip++ {
		if allowed[ip] > capacity {
			t.Errorf("IP %d: allowed=%d > capacity=%d", ip, allowed[ip], capacity)
		}
		if allowed[ip] == 0 {
			t.Errorf("IP %d: never allowed (expected up to %d)", ip, capacity)
		}
	}
	t.Logf("✅ MultipleIPs: %d IPs each capped at %d (reqPerIP=%d)", numIPs, capacity, reqPerIP)
}

// --- HTTP endpoint stress tests ---

// TestStress_RPC_ConcurrentReadEndpoints fires many concurrent GET requests
// against the read-only endpoints (/health, /status, /balance, /mempool).
// No writes occur so there are no data races on the chain state.
func TestStress_RPC_ConcurrentReadEndpoints(t *testing.T) {
	ts, _ := newStressServer(t, 1_000_000) // very high RPS — no throttling

	const (
		goroutines      = 100
		reqsPerGoroutine = 10
	)
	endpoints := []string{
		"/health",
		"/status",
		"/balance?address=0xdeadbeef",
		"/mempool",
	}

	var wg sync.WaitGroup
	var errCount int64
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for r := 0; r < reqsPerGoroutine; r++ {
				url := ts.URL + endpoints[(id+r)%len(endpoints)]
				resp, err := http.Get(url)
				if err != nil {
					atomic.AddInt64(&errCount, 1)
					return
				}
				io.Copy(io.Discard, resp.Body) //nolint:errcheck
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					atomic.AddInt64(&errCount, 1)
				}
			}
		}(g)
	}
	wg.Wait()

	total := int64(goroutines * reqsPerGoroutine)
	if errCount > 0 {
		t.Fatalf("ConcurrentReads: %d/%d requests failed", errCount, total)
	}
	t.Logf("✅ ConcurrentReadEndpoints: %d requests, 0 errors", total)
}

// TestStress_RPC_RateLimiterThrottlesHTTP verifies that HTTP 429 is returned
// when requests from the same IP exceed the configured burst capacity.
func TestStress_RPC_RateLimiterThrottlesHTTP(t *testing.T) {
	const capacity = 5
	ts, _ := newStressServer(t, capacity)

	var ok200, ok429 int
	client := &http.Client{}
	for i := 0; i < capacity*3; i++ {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/health", nil)
		req.Header.Set("X-Forwarded-For", "10.1.1.1") // same IP every request
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			ok200++
		case http.StatusTooManyRequests:
			ok429++
		}
	}

	if ok200 > capacity {
		t.Fatalf("allowed %d 200s, expected ≤%d (burst capacity)", ok200, capacity)
	}
	if ok429 == 0 {
		t.Fatalf("rate limiter never returned 429 (sent %d, capacity=%d)", capacity*3, capacity)
	}
	t.Logf("✅ HTTPRateLimit: 200=%d 429=%d (capacity=%d)", ok200, ok429, capacity)
}

// TestStress_RPC_SubmitTxHighVolume submits a large number of valid signed
// transactions through the HTTP RPC endpoint sequentially and measures
// end-to-end throughput including HTTP overhead.
func TestStress_RPC_SubmitTxHighVolume(t *testing.T) {
	const numSenders = 300

	// Pre-generate all keys so addresses can be included in the genesis allocation.
	// This ensures state is fully initialised before the server starts (no racy
	// mutation of live chain state after the httptest.Server is listening).
	pubs := make([]ed25519.PublicKey, numSenders)
	privs := make([]ed25519.PrivateKey, numSenders)
	alloc := make(map[chain.Address]uint64, numSenders+1)
	for i := 0; i < numSenders; i++ {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen[%d]: %v", i, err)
		}
		pubs[i] = pub
		privs[i] = priv
		alloc[chain.AddressFromPublicKey(pub)] = 10_000_000
	}
	recipPub, _ := genKeypair(t)
	toAddr := chain.AddressFromPublicKey(recipPub)
	alloc[toAddr] = 0 // ensure recipient exists in genesis

	c, err := chain.NewChain(chain.Genesis{ChainID: "rpc-submit-stress", Alloc: alloc})
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	srv := rpc.NewServerWithConfig(c, nil, nil, 1_000_000, 1024*1024)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{}
	start := time.Now()
	for i := 0; i < numSenders; i++ {
		tx := buildSignedTx(t, pubs[i], privs[i], toAddr, 0)
		body, _ := json.Marshal(tx)
		resp, err := client.Post(ts.URL+"/submitTx", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /submitTx[%d]: %v", i, err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("submitTx[%d]: status=%d body=%s", i, resp.StatusCode, respBody)
		}
	}
	elapsed := time.Since(start)

	if len(c.Mempool) != numSenders {
		t.Fatalf("mempool: want %d got %d", numSenders, len(c.Mempool))
	}
	t.Logf("✅ SubmitTxHighVolume: %d txs in %v (%.0f tx/s via HTTP)", numSenders, elapsed, float64(numSenders)/elapsed.Seconds())
}

// TestStress_RPC_OversizedBodyRejected verifies that the body-size middleware
// returns an error status for payloads exceeding the configured limit.
func TestStress_RPC_OversizedBodyRejected(t *testing.T) {
	gen := chain.Genesis{
		ChainID: "body-limit-test",
		Alloc:   map[chain.Address]uint64{chain.Address("0xabc"): 1},
	}
	c, _ := chain.NewChain(gen)
	srv := rpc.NewServerWithConfig(c, nil, nil, 10000, 256) // 256-byte body limit
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	oversized := strings.Repeat("x", 1024)
	resp, err := http.Post(ts.URL+"/submitTx", "application/json", strings.NewReader(oversized))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 413 or 400 for oversized body, got %d", resp.StatusCode)
	}
	t.Logf("✅ OversizedBodyRejected: status=%d", resp.StatusCode)
}

// --- Benchmarks ---

// BenchmarkRPC_StatusEndpoint measures full HTTP round-trip throughput for
// the lightest read-only endpoint.
func BenchmarkRPC_StatusEndpoint(b *testing.B) {
	gen := chain.Genesis{
		ChainID: "bench",
		Alloc:   map[chain.Address]uint64{chain.Address("0xbench"): 1},
	}
	c, _ := chain.NewChain(gen)
	srv := rpc.NewServerWithConfig(c, nil, nil, b.N+1, 1024*1024)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(ts.URL + "/status")
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		resp.Body.Close()
	}
}

// BenchmarkRPC_RateLimiter measures the per-request overhead of the token
// bucket decision in isolation (no network I/O).
func BenchmarkRPC_RateLimiter(b *testing.B) {
	rl := rpc.NewRateLimiter(b.N + 1) // always-allow bucket to measure pure overhead
	defer rl.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow("127.0.0.1")
	}
}
