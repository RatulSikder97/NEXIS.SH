package handler

// Coverage for the auth-handler helper functions. These are pure (no DB,
// no AuthProvider), so table-driven assertions are sufficient. The
// end-to-end auth flow (Signup/Login/MFA/APIKey) is exercised by the
// integration tests under tests/integration.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// TestClearSessionCookie sets the Max-Age=-1 sentinel so any browser drops
// the session cookie. Verifies cookie attributes match the package contract.
func TestClearSessionCookie(t *testing.T) {
	cases := []struct {
		name       string
		env        string
		wantSecure bool
	}{
		{"dev_insecure", "dev", false},
		{"prod_secure", "prod", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			clearSessionCookie(rec, config.Config{AppEnv: tc.env})
			cookies := rec.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("expected 1 cookie, got %d", len(cookies))
			}
			c := cookies[0]
			if c.Name != sessionCookieName {
				t.Fatalf("cookie name: %q", c.Name)
			}
			if c.MaxAge != -1 {
				t.Fatalf("MaxAge: %d want -1", c.MaxAge)
			}
			if c.Value != "" {
				t.Fatalf("cookie value: %q (must be empty)", c.Value)
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

// TestMapAuthError walks every domain.Err* sentinel + the unknown-error
// fallback. Verifies status + body shape end-to-end.
func TestMapAuthError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantSub    string
	}{
		{"invalid_credentials", domain.ErrInvalidCredentials, http.StatusUnauthorized, "invalid credentials"},
		{"mfa_required", domain.ErrMFARequired, http.StatusBadRequest, "mfa required"},
		{"mfa_invalid", domain.ErrMFAInvalid, http.StatusBadRequest, "mfa invalid"},
		{"session_revoked", domain.ErrSessionRevoked, http.StatusUnauthorized, "session revoked"},
		{"session_expired", domain.ErrSessionExpired, http.StatusUnauthorized, "session expired"},
		{"not_found", domain.ErrNotFound, http.StatusNotFound, "not found"},
		{"conflict", domain.ErrConflict, http.StatusConflict, "email already registered"},
		{"unknown", errors.New("postgres timeout"), http.StatusInternalServerError, "internal error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mapAuthError(rec, tc.err, "test")
			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d want %d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantSub) {
				t.Fatalf("body: got %q want substring %q", rec.Body.String(), tc.wantSub)
			}
		})
	}
}

// TestMapAuthErrorSafe — the env-aware variant returns the wrapped error
// detail in dev mode and the generic "internal server error" in prod.
func TestMapAuthErrorSafe(t *testing.T) {
	t.Run("dev_surfaces_detail", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mapAuthErrorSafe(rec, errors.New("postgres: connection refused"), "signup", config.Config{AppEnv: "dev"})
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status: %d", rec.Code)
		}
		// Dev mode surfaces the operation tag at minimum.
		if !strings.Contains(rec.Body.String(), "signup") && !strings.Contains(rec.Body.String(), "postgres") {
			t.Fatalf("dev body should surface details, got %q", rec.Body.String())
		}
	})
	t.Run("prod_generic", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mapAuthErrorSafe(rec, errors.New("postgres: connection refused"), "signup", config.Config{AppEnv: "prod"})
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status: %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "postgres") {
			t.Fatalf("prod body must NOT surface error detail: %q", rec.Body.String())
		}
	})
	t.Run("known_error_still_maps_normally", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mapAuthErrorSafe(rec, domain.ErrInvalidCredentials, "login", config.Config{AppEnv: "prod"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status: %d", rec.Code)
		}
	})
}

// recAud is a tiny capturing AuditWriter for the auditWrite helper test.
type recAud struct {
	calls   int
	lastErr error
}

func (r *recAud) Write(_ context.Context, _ domain.Principal, _, _ string, _ map[string]any) error {
	r.calls++
	return r.lastErr
}

// TestAuditWrite covers the three branches: nil writer (no-op), happy path
// (one call), error (logged but doesn't abort).
func TestAuditWrite(t *testing.T) {
	t.Run("nil_writer_noop", func(t *testing.T) {
		// Must not panic.
		auditWrite(httptest.NewRequest(http.MethodGet, "/", nil), nil,
			domain.Principal{}, "action", "subj", nil)
	})
	t.Run("happy_path_calls_once", func(t *testing.T) {
		aud := &recAud{}
		auditWrite(httptest.NewRequest(http.MethodGet, "/", nil), aud,
			domain.Principal{OrgID: "org-1"}, "action.x", "target-1", map[string]any{"k": "v"})
		if aud.calls != 1 {
			t.Fatalf("calls: %d want 1", aud.calls)
		}
	})
	t.Run("error_logged_but_no_panic", func(t *testing.T) {
		aud := &recAud{lastErr: errors.New("audit boom")}
		auditWrite(httptest.NewRequest(http.MethodGet, "/", nil), aud,
			domain.Principal{}, "a", "t", nil)
		if aud.calls != 1 {
			t.Fatalf("calls: %d", aud.calls)
		}
	})
}

// TestDecodeBody — happy path decodes; invalid JSON 400s with the canonical
// "invalid request body" message; unknown fields are also rejected (the
// DisallowUnknownFields() guard fires).
func TestDecodeBody(t *testing.T) {
	type body struct {
		Name string `json:"name"`
	}
	t.Run("happy_decode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"name":"alice"}`))
		rec := httptest.NewRecorder()
		var got body
		if !decodeBody(rec, req, &got) {
			t.Fatalf("expected ok decode, body=%s", rec.Body.String())
		}
		if got.Name != "alice" {
			t.Fatalf("name: %q", got.Name)
		}
	})
	t.Run("invalid_json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{bogus`))
		rec := httptest.NewRecorder()
		var got body
		if decodeBody(rec, req, &got) {
			t.Fatalf("expected decode to fail")
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status: %d want 400", rec.Code)
		}
	})
	t.Run("unknown_field_rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"name":"x","extra":"y"}`))
		rec := httptest.NewRecorder()
		var got body
		if decodeBody(rec, req, &got) {
			t.Fatalf("unknown field should be rejected by DisallowUnknownFields")
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status: %d want 400", rec.Code)
		}
	})
}

// TestWriteJSON — the JSON-response helper writes the body + sets the
// Content-Type header.
func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusCreated, map[string]string{"foo": "bar"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type: %q", got)
	}
	if !strings.Contains(rec.Body.String(), `"foo":"bar"`) {
		t.Fatalf("body: %q", rec.Body.String())
	}
}

// TestWriteError — the canonical error envelope shape.
func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusForbidden, "no")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error":"no"`) {
		t.Fatalf("body: %q", rec.Body.String())
	}
}

// TestHasWorkspace — returns false when checker is nil OR when checker
// reports false; true otherwise.
func TestHasWorkspace(t *testing.T) {
	t.Run("nil_checker", func(t *testing.T) {
		if hasWorkspace(context.Background(), nil, "org-1") {
			t.Fatalf("nil checker must return false")
		}
	})
	t.Run("checker_false", func(t *testing.T) {
		ck := stubWsChecker{exists: false}
		if hasWorkspace(context.Background(), ck, "org-1") {
			t.Fatalf("checker false must return false")
		}
	})
	t.Run("checker_true", func(t *testing.T) {
		ck := stubWsChecker{exists: true}
		if !hasWorkspace(context.Background(), ck, "org-1") {
			t.Fatalf("checker true must return true")
		}
	})
	t.Run("checker_error_returns_false", func(t *testing.T) {
		ck := stubWsChecker{err: errors.New("db down")}
		if hasWorkspace(context.Background(), ck, "org-1") {
			t.Fatalf("checker err must surface as false (best-effort)")
		}
	})
}

// stubWsChecker satisfies the WorkspaceChecker port used by hasWorkspace.
type stubWsChecker struct {
	exists bool
	err    error
}

func (s stubWsChecker) HasAny(_ context.Context, _ string) (bool, error) {
	return s.exists, s.err
}
