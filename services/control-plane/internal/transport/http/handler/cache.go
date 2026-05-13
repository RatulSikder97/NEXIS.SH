package handler

import (
	"sync"
	"time"
)

// ttlCache is a tiny in-memory keyed cache with a per-entry TTL. Used by the
// /v1/system-health and /v1/system-status endpoints so the probe fanout
// (dial Postgres/Redis/Neo4j/MinIO/Temporal) runs at most every TTL seconds
// even when the dashboard polls aggressively.
//
// The cache is intentionally simple — no LRU, no size cap, no async sweep.
// Both probe endpoints use a single fixed key ("system-health",
// "system-status") so the upper-bound footprint is two entries.
type ttlCache[T any] struct {
	mu      sync.Mutex
	entries map[string]ttlEntry[T]
	ttl     time.Duration
}

type ttlEntry[T any] struct {
	value     T
	expiresAt time.Time
}

// newTTLCache constructs a cache with the supplied TTL. ttl <= 0 disables
// caching (every Get returns a miss).
func newTTLCache[T any](ttl time.Duration) *ttlCache[T] {
	return &ttlCache[T]{
		entries: map[string]ttlEntry[T]{},
		ttl:     ttl,
	}
}

// Get returns the stored value when the entry is still fresh; otherwise
// (false). Concurrency-safe.
func (c *ttlCache[T]) Get(key string) (T, bool) {
	var zero T
	if c == nil || c.ttl <= 0 {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return zero, false
	}
	if time.Now().After(e.expiresAt) {
		delete(c.entries, key)
		return zero, false
	}
	return e.value, true
}

// Set writes a value with the cache's configured TTL.
func (c *ttlCache[T]) Set(key string, v T) {
	if c == nil || c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = ttlEntry[T]{value: v, expiresAt: time.Now().Add(c.ttl)}
}
