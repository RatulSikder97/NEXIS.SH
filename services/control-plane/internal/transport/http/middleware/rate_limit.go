// Package middleware — per-org rate limiting (SQA F-7).
//
// The RateLimit middleware enforces a token-bucket rate limit keyed by the
// principal's OrgID. It must be mounted AFTER appmw.Auth (which decorates
// the request with the principal) so the keying step has something to read.
//
// Anonymous / pre-auth requests are passed through without a limiter — the
// public webhook + signup surface has its own rate-control story (HMAC-
// signed payloads with idempotency keys, body-limit middleware). The /v1
// protected surface is what this middleware exists to protect.
//
// The limiter map is sharded per orgID. We trade a small lock-contention
// surface for the simplicity of one global sync.Map; the token bucket
// itself is lock-free inside rate.Limiter so the hot path is one Allow()
// call per request.
//
// On exceed: 429 Too Many Requests + Retry-After header (seconds until the
// next token is available, rounded up to 1) + canonical JSON envelope so
// dashboards / SDKs can grep the error key.
package middleware

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimitConfig carries the bucket parameters. RPS is the steady-state
// token-replenish rate; Burst is the bucket capacity (the largest
// instantaneous burst we accept).
//
// Both values are per-org — every distinct OrgID gets its own limiter
// instance. Zero or negative values short-circuit the middleware (it
// becomes a pass-through), which is convenient for tests that want to
// bypass without untangling the chain.
type RateLimitConfig struct {
	RPS     float64
	Burst   int
	Enabled bool
}

// RateLimit returns a middleware that enforces the configured per-org
// token bucket. When cfg.Enabled is false (or the params are zero-shaped),
// the middleware is a pass-through so the chain composes cleanly even
// when the limiter is disabled.
//
// The limiter map is keyed by OrgID and grows monotonically across the
// process lifetime. For a control-plane with O(10²) orgs this is fine; if
// we ever scale past that we can swap in a periodic GC (LRU sweep on map
// growth) without changing the call-site signature.
//
// Requests without a principal in ctx (pre-auth webhook surface, healthz)
// are passed through — RequireAuth elsewhere enforces presence on routes
// that should be authenticated.
func RateLimit(cfg RateLimitConfig) func(http.Handler) http.Handler {
	if !cfg.Enabled || cfg.RPS <= 0 || cfg.Burst <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	limiters := &orgLimiters{
		rps:   rate.Limit(cfg.RPS),
		burst: cfg.Burst,
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			princ, ok := PrincipalFrom(r.Context())
			if !ok || princ.OrgID == "" {
				next.ServeHTTP(w, r)
				return
			}
			lim := limiters.get(princ.OrgID)
			reservation := lim.Reserve()
			if !reservation.OK() {
				// Burst > 0 should mean OK is always true, but defend
				// against a misconfigured limiter by treating an invalid
				// reservation as a denial.
				writeRateLimitError(w, time.Second)
				return
			}
			delay := reservation.Delay()
			if delay > 0 {
				// We don't want to actually wait — that would queue the
				// request on the server's worker pool and breaches the
				// fail-fast contract of the middleware. Cancel the
				// reservation (returns the token to the bucket) and reject
				// with 429 + Retry-After hint.
				reservation.Cancel()
				retry := time.Duration(math.Ceil(delay.Seconds())) * time.Second
				if retry < time.Second {
					retry = time.Second
				}
				writeRateLimitError(w, retry)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// orgLimiters is a sharded sync.Map of *rate.Limiter keyed by orgID. Get
// is lock-free for the steady-state hit case (sync.Map.Load) and only
// takes a short write lock on the first request for a new org.
type orgLimiters struct {
	m     sync.Map // orgID → *rate.Limiter
	rps   rate.Limit
	burst int
}

func (o *orgLimiters) get(orgID string) *rate.Limiter {
	if v, ok := o.m.Load(orgID); ok {
		return v.(*rate.Limiter)
	}
	// First-time miss — construct a fresh limiter and store. LoadOrStore
	// resolves the race between two concurrent first-time callers: the
	// loser's just-built limiter is dropped on the floor.
	lim := rate.NewLimiter(o.rps, o.burst)
	actual, _ := o.m.LoadOrStore(orgID, lim)
	return actual.(*rate.Limiter)
}

// writeRateLimitError emits the canonical 429 envelope. The Retry-After
// header is the integer-seconds form per RFC 7231 §7.1.3 — dashboards and
// SDKs can read it without parsing the body.
func writeRateLimitError(w http.ResponseWriter, retryAfter time.Duration) {
	secs := int(math.Ceil(retryAfter.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":          "rate_limited",
		"retry_after_ms": retryAfter.Milliseconds(),
	})
}
