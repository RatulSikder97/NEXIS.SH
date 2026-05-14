package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// passthrough is the inner handler used by the rate-limit tests — every
// request counts as a "200 OK from origin" so the assertion is "did the
// middleware short-circuit?".
func passthrough() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// reqWithPrincipal returns a /req request whose ctx carries the given org.
func reqWithPrincipal(orgID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/req", nil)
	if orgID != "" {
		ctx := WithPrincipal(r.Context(), domain.Principal{OrgID: orgID})
		r = r.WithContext(ctx)
	}
	return r
}

func TestRateLimit_DisabledIsPassthrough(t *testing.T) {
	h := RateLimit(RateLimitConfig{Enabled: false, RPS: 100, Burst: 100})(passthrough())
	for i := 0; i < 1000; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, reqWithPrincipal("acme"))
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: status=%d want 200 (limiter disabled should pass-through)", i, rec.Code)
		}
	}
}

func TestRateLimit_ZeroConfigIsPassthrough(t *testing.T) {
	// Zero RPS or zero Burst → middleware is a no-op even when Enabled.
	for _, cfg := range []RateLimitConfig{
		{Enabled: true, RPS: 0, Burst: 100},
		{Enabled: true, RPS: 100, Burst: 0},
	} {
		h := RateLimit(cfg)(passthrough())
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, reqWithPrincipal("acme"))
		if rec.Code != http.StatusOK {
			t.Errorf("zero-shaped cfg %+v: status=%d want 200", cfg, rec.Code)
		}
	}
}

func TestRateLimit_NoPrincipalPassesThrough(t *testing.T) {
	// Public webhooks / healthz arrive without a principal; the middleware
	// must NOT rate-limit them (they have their own HMAC + idempotency
	// controls; rate-limiting here would be the wrong layer).
	h := RateLimit(RateLimitConfig{Enabled: true, RPS: 0.01, Burst: 1})(passthrough())
	for i := 0; i < 50; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, reqWithPrincipal("")) // empty OrgID → pre-auth shape
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: status=%d want 200 (anonymous request should pass through)", i, rec.Code)
		}
	}
}

func TestRateLimit_BurstAllowedThenRejected(t *testing.T) {
	// Tight bucket: RPS=0.01 ⇒ 100s between refills, Burst=3 ⇒ first three
	// requests succeed, the fourth must 429 (the bucket is empty and the
	// next refill is far in the future).
	h := RateLimit(RateLimitConfig{Enabled: true, RPS: 0.01, Burst: 3})(passthrough())
	const org = "tight"
	for i := 1; i <= 3; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, reqWithPrincipal(org))
		if rec.Code != http.StatusOK {
			t.Fatalf("burst call %d: status=%d want 200 (within burst)", i, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, reqWithPrincipal(org))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-burst: status=%d want 429", rec.Code)
	}
	// Retry-After header must be present and >=1.
	retry := rec.Header().Get("Retry-After")
	if retry == "" {
		t.Fatalf("Retry-After header missing on 429")
	}
	if n, err := strconv.Atoi(retry); err != nil || n < 1 {
		t.Fatalf("Retry-After=%q invalid; expected positive int seconds", retry)
	}
	// Body envelope is the canonical {"error":"rate_limited", ...}.
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if body["error"] != "rate_limited" {
		t.Errorf("error=%v want rate_limited", body["error"])
	}
}

func TestRateLimit_PerOrgIsolation(t *testing.T) {
	// One org's exhausted bucket must not affect another org. Burst=1 + a
	// very low RPS so the bucket doesn't refill during the test window.
	h := RateLimit(RateLimitConfig{Enabled: true, RPS: 0.01, Burst: 1})(passthrough())

	// Drain org "a".
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, reqWithPrincipal("a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("a first: status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, reqWithPrincipal("a"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("a second: status=%d want 429", rec.Code)
	}

	// Org "b" still has its own bucket — must succeed.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, reqWithPrincipal("b"))
	if rec.Code != http.StatusOK {
		t.Fatalf("b first: status=%d want 200 (per-org isolation broken)", rec.Code)
	}
}
