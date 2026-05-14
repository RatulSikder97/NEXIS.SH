package handler

// Coverage for the auth-OAuth helper functions: resolveReturnTo,
// redirectWithError, clearOAuthStateCookie. The WorkOSCallback wire path
// exercises these indirectly; here we cover the unit logic so the open-
// redirect mitigations are exhaustively tested.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// TestResolveReturnTo — the open-redirect guard. Only safe destinations are
// accepted:
//   - empty/missing → default /console
//   - relative path starting with "/" → app base + path
//   - absolute URL matching cfg.AppBaseURL prefix → echoed back
//   - anything else (protocol-relative, other-origin, malformed) → default
func TestResolveReturnTo(t *testing.T) {
	cfg := config.Config{AppBaseURL: "https://example.com"}
	const wantDefault = "https://example.com/console"

	cases := []struct {
		name string
		rt   string
		want string
	}{
		{"empty_falls_back", "", wantDefault},
		{"missing_query_falls_back", "  ", wantDefault},
		{"relative_path_kept", "/console/integrations", "https://example.com/console/integrations"},
		{"relative_path_with_query", "/console?tab=x", "https://example.com/console?tab=x"},
		{"absolute_matching_base_ok", "https://example.com/dashboards", "https://example.com/dashboards"},
		{"absolute_other_origin_rejected", "https://evil.com/x", wantDefault},
		{"protocol_relative_rejected", "//evil.com/x", wantDefault},
		{"non_leading_slash_rejected", "console/x", wantDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/cb", nil)
			// Build the query directly so test inputs containing odd
			// characters don't crash httptest.NewRequest.
			q := req.URL.Query()
			q.Set("return_to", tc.rt)
			req.URL.RawQuery = q.Encode()

			got := resolveReturnTo(req, cfg)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestResolveReturnTo_MalformedURLFallsBack — url.Parse errors are treated
// as untrusted destinations.
func TestResolveReturnTo_MalformedURLFallsBack(t *testing.T) {
	cfg := config.Config{AppBaseURL: "https://example.com"}
	req := httptest.NewRequest(http.MethodGet, "/cb", nil)
	q := req.URL.Query()
	// Embed a control char that url.Parse rejects.
	q.Set("return_to", "http://\x00bad")
	req.URL.RawQuery = q.Encode()
	got := resolveReturnTo(req, cfg)
	if got != "https://example.com/console" {
		t.Fatalf("malformed return_to: got %q want default", got)
	}
}

// TestRedirectWithError — emits a 302 to the sign-in page with the
// canonical `?oauth_error=<code>` query param.
func TestRedirectWithError(t *testing.T) {
	cfg := config.Config{AppBaseURL: "https://example.com"}
	req := httptest.NewRequest(http.MethodGet, "/cb", nil)
	rec := httptest.NewRecorder()
	redirectWithError(rec, req, cfg, "state_mismatch")
	if rec.Code != http.StatusFound {
		t.Fatalf("status: %d want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/sign-in?") {
		t.Fatalf("location: %q", loc)
	}
	if !strings.Contains(loc, "oauth_error=state_mismatch") {
		t.Fatalf("oauth_error missing: %q", loc)
	}
}

// TestClearOAuthStateCookie — cookie with MaxAge=-1 + empty value, attrs
// match the SetCookie contract.
func TestClearOAuthStateCookie(t *testing.T) {
	cases := []struct {
		env        string
		wantSecure bool
	}{
		{"dev", false},
		{"prod", true},
	}
	for _, tc := range cases {
		t.Run(tc.env, func(t *testing.T) {
			rec := httptest.NewRecorder()
			clearOAuthStateCookie(rec, config.Config{AppEnv: tc.env})
			cookies := rec.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies: %d", len(cookies))
			}
			c := cookies[0]
			if c.Name != oauthStateCookieName {
				t.Fatalf("name: %q", c.Name)
			}
			if c.MaxAge != -1 {
				t.Fatalf("MaxAge: %d", c.MaxAge)
			}
			if c.Value != "" {
				t.Fatalf("value: %q", c.Value)
			}
			if c.Secure != tc.wantSecure {
				t.Fatalf("secure: got %v want %v", c.Secure, tc.wantSecure)
			}
		})
	}
}
