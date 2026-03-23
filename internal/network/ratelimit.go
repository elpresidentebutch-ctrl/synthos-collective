package network

import (
	"sync"
	"time"
)

// TokenBucket is a simple per-peer rate limiter.
// It is intentionally minimal and deterministic (no randomness).
type TokenBucket struct {
	mu sync.Mutex

	capacity float64
	rate     float64 // tokens per second

	tokens float64
	last   time.Time
}

func NewTokenBucket(capacity int, refillPerSec float64) *TokenBucket {
	now := time.Now().UTC()
	return &TokenBucket{
		capacity: float64(capacity),
		rate:     refillPerSec,
		tokens:   float64(capacity),
		last:     now,
	}
}

// Allow consumes 1 token if available.
func (b *TokenBucket) Allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.last.IsZero() {
		b.last = now
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 && b.rate > 0 {
		b.tokens += elapsed * b.rate
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.last = now
	}

	if b.tokens >= 1 {
		b.tokens -= 1
		return true
	}
	return false
}

// PeerLimiter is a map of token buckets keyed by peer ID.
type PeerLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*TokenBucket
	capacity int
	rate     float64
}

func NewPeerLimiter(capacity int, refillPerSec float64) *PeerLimiter {
	return &PeerLimiter{
		buckets:  make(map[string]*TokenBucket),
		capacity: capacity,
		rate:     refillPerSec,
	}
}

func (l *PeerLimiter) Allow(peerID string, now time.Time) bool {
	l.mu.Lock()
	b := l.buckets[peerID]
	if b == nil {
		b = NewTokenBucket(l.capacity, l.rate)
		l.buckets[peerID] = b
	}
	l.mu.Unlock()

	return b.Allow(now)
}

