// Package github — Redis-backed installation token cache.
//
// Installation tokens are short-lived (~1h) and idempotent to fetch: any caller
// holding the App private key + installation_id can mint a fresh one. The
// cache exists to keep us under GitHub's /app/installations/{id}/access_tokens
// rate limit when many concurrent requests need the same installation token
// inside a short window (e.g. a Status poll followed by a PR open).
//
// The cache is intentionally simple: a string get/set keyed by installation
// ID with the TTL pinned to GitHub's reported expires_at minus a 5-minute
// safety margin. If Config.InstallationCache is nil the Provider re-mints on
// every call — correct, just slower.
package github

import (
	"context"
	"time"
)

// RedisCache is the minimal subset of *redis.Client the cache uses. Keeping it
// as an interface here avoids pulling go-redis into the github package — main.go
// can pass any *redis.Client (which satisfies this interface structurally via
// the methods it already exposes).
//
// NOTE: the methods match go-redis v9 *exactly* (Get returns a *redis.StringCmd
// with a .Result() (string, error) method; Set returns *redis.StatusCmd). To
// avoid leaking those concrete types, the adapter accepts something thinner:
// see CacheAdapter below.
type RedisCache interface {
	// Get returns the value at key, or ("", redis.Nil-equivalent) when missing.
	// The implementer maps redis.Nil to its own sentinel; the cache treats any
	// non-nil error as a miss (so a Redis outage falls back to re-minting).
	Get(ctx context.Context, key string) (string, error)
	// Set writes value with the supplied TTL. err propagates back to the
	// caller; on Set errors the Provider still uses the token but the next
	// caller will re-mint.
	Set(ctx context.Context, key, value string, ttl time.Duration) error
}

// installationTokenCache wraps a RedisCache with the GitHub-specific keying
// + TTL math. nil-safe: if backend is nil, Get always misses and Set is a no-op,
// so the Provider operates correctly without Redis.
type installationTokenCache struct {
	backend RedisCache
}

// newInstallationTokenCache builds a cache. A nil backend disables caching.
func newInstallationTokenCache(b RedisCache) *installationTokenCache {
	return &installationTokenCache{backend: b}
}

// key returns the Redis key for an installation. Documented as gh:inst:<id>.
func (c *installationTokenCache) key(installationID int64) string {
	return cacheKeyPrefix + itoa(installationID)
}

const cacheKeyPrefix = "gh:inst:"

// Get returns a cached token, or ("", false) when missing / errored. We never
// surface backend errors to the caller — a cold cache and a broken cache both
// degrade to re-minting, which is safe.
func (c *installationTokenCache) Get(ctx context.Context, installationID int64) (string, bool) {
	if c == nil || c.backend == nil {
		return "", false
	}
	v, err := c.backend.Get(ctx, c.key(installationID))
	if err != nil || v == "" {
		return "", false
	}
	return v, true
}

// Set stores token with TTL = expiresAt - now - 5min (clamped to ≥ 0). A
// nil backend is a no-op. Set errors are swallowed for the same reason as
// Get: cache failures must never block the integration path.
func (c *installationTokenCache) Set(ctx context.Context, installationID int64, token string, expiresAt time.Time, now time.Time) {
	if c == nil || c.backend == nil {
		return
	}
	ttl := expiresAt.Sub(now) - 5*time.Minute
	if ttl <= 0 {
		// Token is already within the safety margin — skip caching.
		return
	}
	_ = c.backend.Set(ctx, c.key(installationID), token, ttl)
}

// itoa is a tiny dependency-free int64→string helper so this file does not
// need to import strconv (avoiding a one-symbol-per-file dance).
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
