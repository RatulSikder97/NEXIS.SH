// Package sentry — fingerprint LRU.
//
// lru.go is a tiny bounded-FIFO set used to dedupe Sentry issue fingerprints
// across the webhook handler and the backfill cron. The set evicts the
// oldest fingerprint when the cap is reached.
//
// Why not container/list + map? We never need O(1) move-to-front access —
// fingerprint visits are write-only, so a ring + lookup map is enough and
// avoids the boxing overhead of container/list.
package sentry

import "sync"

// fingerprintLRU is a small bounded set with FIFO eviction. Safe for
// concurrent use; the Provider shares one instance across webhooks + cron
// goroutines.
type fingerprintLRU struct {
	mu    sync.Mutex
	cap   int
	set   map[string]struct{}
	order []string // FIFO ring; len == len(set); first slot is oldest
}

// newFingerprintLRU builds an LRU with the given capacity (clamped to 1).
func newFingerprintLRU(capacity int) *fingerprintLRU {
	if capacity <= 0 {
		capacity = 1
	}
	return &fingerprintLRU{
		cap:   capacity,
		set:   make(map[string]struct{}, capacity),
		order: make([]string, 0, capacity),
	}
}

// Add inserts a fingerprint. Returns true if the fingerprint was new
// (and thus added), false if it was already present.
func (l *fingerprintLRU) Add(fp string) bool { return l.AddIfAbsent(fp) }

// AddIfAbsent is an explicit alias used by the backfill loop for
// readability — "Add" can be misread as "force-add".
func (l *fingerprintLRU) AddIfAbsent(fp string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.set[fp]; ok {
		return false
	}
	if len(l.order) >= l.cap {
		oldest := l.order[0]
		l.order = l.order[1:]
		delete(l.set, oldest)
	}
	l.set[fp] = struct{}{}
	l.order = append(l.order, fp)
	return true
}

// Drop removes a fingerprint. Used by the backfill loop to roll back a
// fingerprint when its sink insert fails, so the next tick can retry.
func (l *fingerprintLRU) Drop(fp string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.set[fp]; !ok {
		return
	}
	delete(l.set, fp)
	for i, v := range l.order {
		if v == fp {
			l.order = append(l.order[:i], l.order[i+1:]...)
			return
		}
	}
}

// Contains reports whether fp is currently in the LRU. Test helper.
func (l *fingerprintLRU) Contains(fp string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.set[fp]
	return ok
}

// Len returns the current set size. Test helper.
func (l *fingerprintLRU) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.set)
}
