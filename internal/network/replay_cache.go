package network

import (
	"sync"
	"time"
)

// ReplayCache tracks per-sender nonces to prevent replay attacks.
// It is intentionally simple to start; we can evolve it to an LRU later.
type ReplayCache struct {
	mu      sync.Mutex
	entries map[string]map[string]time.Time // fromAgentID -> nonce -> seenAt
	ttl     time.Duration
}

func NewReplayCache(ttl time.Duration) *ReplayCache {
	return &ReplayCache{
		entries: make(map[string]map[string]time.Time),
		ttl:     ttl,
	}
}

// SeenBefore returns true if nonce has already been observed for fromAgentID.
// If not observed, it records it and returns false.
func (c *ReplayCache) SeenBefore(fromAgentID, nonce string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	// periodic cleanup (cheap + conservative)
	c.cleanupLocked(now)

	m, ok := c.entries[fromAgentID]
	if !ok {
		m = make(map[string]time.Time)
		c.entries[fromAgentID] = m
	}
	if _, exists := m[nonce]; exists {
		return true
	}
	m[nonce] = now
	return false
}

func (c *ReplayCache) cleanupLocked(now time.Time) {
	if c.ttl <= 0 {
		return
	}
	cutoff := now.Add(-c.ttl)
	for from, m := range c.entries {
		for nonce, t := range m {
			if t.Before(cutoff) {
				delete(m, nonce)
			}
		}
		if len(m) == 0 {
			delete(c.entries, from)
		}
	}
}

