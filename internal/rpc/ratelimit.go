package rpc

import (
	"net/http"
	"sync"
	"time"
)

// TokenBucket implements a simple token bucket rate limiter.
// Used to rate limit RPC endpoints per-client IP.
type TokenBucket struct {
	capacity  int       // Max tokens available
	tokens    float64   // Current token count
	refillRate float64  // Tokens per second
	lastRefill time.Time
	mu        sync.Mutex
}

// NewTokenBucket creates a new token bucket with the given capacity
// and refill rate (tokens per second).
func NewTokenBucket(capacity int, tokensPerSecond float64) *TokenBucket {
	return &TokenBucket{
		capacity:   capacity,
		tokens:     float64(capacity),
		refillRate: tokensPerSecond,
		lastRefill: time.Now(),
	}
}

// Allow checks if a token is available. Returns true if allowed, false if rate-limited.
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	tb.lastRefill = now

	// Cap at capacity
	if tb.tokens > float64(tb.capacity) {
		tb.tokens = float64(tb.capacity)
	}

	// Try to consume one token
	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

// RateLimiter tracks per-IP token buckets and enforces rate limits.
type RateLimiter struct {
	capacity       int
	tokensPerSecond float64
	buckets        map[string]*TokenBucket
	mu             sync.RWMutex
	cleanupTicker  *time.Ticker
	stopCleanup    chan struct{}
}

// NewRateLimiter creates a new rate limiter with given RPS (requests per second).
func NewRateLimiter(rps int) *RateLimiter {
	rl := &RateLimiter{
		capacity:        rps,
		tokensPerSecond: float64(rps),
		buckets:         make(map[string]*TokenBucket),
		cleanupTicker:   time.NewTicker(1 * time.Minute),
		stopCleanup:     make(chan struct{}),
	}
	// Start cleanup goroutine to remove stale buckets
	go rl.cleanupStale()
	return rl
}

// Allow checks if the given IP is rate-limited.
func (rl *RateLimiter) Allow(clientIP string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[clientIP]
	if !exists {
		bucket = NewTokenBucket(rl.capacity, rl.tokensPerSecond)
		rl.buckets[clientIP] = bucket
	}

	return bucket.Allow()
}

// cleanupStale removes unused buckets periodically to prevent memory leak.
func (rl *RateLimiter) cleanupStale() {
	for {
		select {
		case <-rl.cleanupTicker.C:
			rl.mu.Lock()
			// Remove buckets that haven't been refilled in 10 minutes
			cutoff := time.Now().Add(-10 * time.Minute)
			for ip, bucket := range rl.buckets {
				bucket.mu.Lock()
				if bucket.lastRefill.Before(cutoff) {
					delete(rl.buckets, ip)
				}
				bucket.mu.Unlock()
			}
			rl.mu.Unlock()
		case <-rl.stopCleanup:
			rl.cleanupTicker.Stop()
			return
		}
	}
}

// Close stops the cleanup goroutine.
func (rl *RateLimiter) Close() {
	close(rl.stopCleanup)
}

// Middleware returns an HTTP middleware that enforces rate limiting.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.RemoteAddr
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			clientIP = xff
		}

		if !rl.Allow(clientIP) {
			http.Error(w, "429 Too Many Requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
