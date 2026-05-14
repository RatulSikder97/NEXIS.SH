package handler

// Unit coverage for the integration-OAuth helper functions. These are pure
// (no DB, no HTTP), so they can live in the same package as the source —
// no fixture, no test harness, just table-driven assertions.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// TestNewOAuthState_RandomNonEmpty exercises the happy-path mint of a state
// token. Two calls back-to-back should never collide given 256 bits of
// entropy, and the encoded length must match the unpadded base64-url of 32
// bytes (ceil(32*8/6) = 43 chars).
func TestNewOAuthState_RandomNonEmpty(t *testing.T) {
	a := newOAuthState()
	b := newOAuthState()
	if a == "" || b == "" {
		t.Fatalf("newOAuthState returned empty: %q %q", a, b)
	}
	if a == b {
		t.Fatalf("newOAuthState produced identical tokens: %q", a)
	}
	if len(a) != 43 {
		t.Fatalf("encoded length: got %d want 43 (base64-url-no-pad of 32 bytes)", len(a))
	}
}

// TestSetInstallStateCookie_WritesAttributes asserts the cookie shape lines
// up with the package contract — one-shot, 5min, HttpOnly, Lax. Secure flag
// flips on non-dev environments.
func TestSetInstallStateCookie_WritesAttributes(t *testing.T) {
	cases := []struct {
		name       string
		env        string
		wantSecure bool
	}{
		{"dev_insecure", "dev", false},
		{"prod_secure", "prod", true},
		{"staging_secure", "staging", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			setInstallStateCookie(rec, config.Config{AppEnv: tc.env}, "deadbeef")
			cookies := rec.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies set: got %d want 1", len(cookies))
			}
			c := cookies[0]
			if c.Name != oauthStateCookieName {
				t.Fatalf("cookie name: %q", c.Name)
			}
			if c.Value != "deadbeef" {
				t.Fatalf("cookie value: %q", c.Value)
			}
			if c.MaxAge != oauthStateMaxAge {
				t.Fatalf("max age: %d", c.MaxAge)
			}
			if !c.HttpOnly {
				t.Fatalf("HttpOnly must be true")
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Fatalf("SameSite: %v", c.SameSite)
			}
			if c.Secure != tc.wantSecure {
				t.Fatalf("Secure: got %v want %v", c.Secure, tc.wantSecure)
			}
		})
	}
}

// TestClearInstallStateCookie_NegativeMaxAge — the clear path should write a
// cookie with MaxAge=-1 + Expires far in the past so any browser drops it.
func TestClearInstallStateCookie_NegativeMaxAge(t *testing.T) {
	rec := httptest.NewRecorder()
	clearInstallStateCookie(rec, config.Config{AppEnv: "dev"})
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies set: got %d want 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != oauthStateCookieName {
		t.Fatalf("cookie name: %q", c.Name)
	}
	if c.MaxAge != -1 {
		t.Fatalf("MaxAge: %d want -1", c.MaxAge)
	}
}

// TestVerifyInstallState verifies the CSRF cookie check: match → true, all
// other combinations (empty, missing, drift) → false.
func TestVerifyInstallState(t *testing.T) {
	t.Run("happy_match", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/cb", nil)
		req.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: "abc123"})
		if !verifyInstallState(req, "abc123") {
			t.Fatalf("expected match")
		}
	})
	t.Run("missing_cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/cb", nil)
		if verifyInstallState(req, "abc123") {
			t.Fatalf("missing cookie must NOT match")
		}
	})
	t.Run("empty_state_query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/cb", nil)
		req.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: "abc123"})
		if verifyInstallState(req, "") {
			t.Fatalf("empty state must NOT match")
		}
	})
	t.Run("drift", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/cb", nil)
		req.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: "abc123"})
		if verifyInstallState(req, "xyz789") {
			t.Fatalf("drift must NOT match")
		}
	})
	t.Run("empty_cookie_value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/cb", nil)
		req.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: ""})
		if verifyInstallState(req, "any") {
			t.Fatalf("empty cookie value must NOT match")
		}
	})
}

// TestInstallRedirectURL builds /console/integrations?installed=<provider>
// with the base URL stripped of trailing slashes. Empty base falls back to
// localhost so dev callers still get a usable URL.
func TestInstallRedirectURL(t *testing.T) {
	cases := []struct {
		name string
		base string
		prov string
		want string
	}{
		{"plain", "http://example.com", "github", "http://example.com/console/integrations?installed=github"},
		{"trailing_slash", "http://example.com/", "slack", "http://example.com/console/integrations?installed=slack"},
		{"empty_base", "", "github", "http://localhost:3000/console/integrations?installed=github"},
		{"escaped_provider", "http://x", "weird name", "http://x/console/integrations?installed=weird+name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := installRedirectURL(tc.base, tc.prov)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestInstallErrorURL — error redirect builds /console/integrations with
// both install_error and provider query params.
func TestInstallErrorURL(t *testing.T) {
	got := installErrorURL("http://x", "github", "state_mismatch")
	if !strings.HasPrefix(got, "http://x/console/integrations?") {
		t.Fatalf("base wrong: %q", got)
	}
	if !strings.Contains(got, "install_error=state_mismatch") {
		t.Fatalf("install_error missing: %q", got)
	}
	if !strings.Contains(got, "provider=github") {
		t.Fatalf("provider missing: %q", got)
	}

	// Empty base falls back to localhost
	got2 := installErrorURL("", "slack", "exchange_failed")
	if !strings.HasPrefix(got2, "http://localhost:3000/console/integrations?") {
		t.Fatalf("empty base fallback wrong: %q", got2)
	}
}
