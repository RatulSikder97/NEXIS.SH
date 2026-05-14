package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// okHandler returns a fixed 200 + body — the canonical "the request reached the
// handler" sentinel used across the middleware tests.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
})

// ----- CORS -------------------------------------------------------------

func TestCORS_AllowedOriginMirrored(t *testing.T) {
	mw := CORS("https://app.example.com")
	h := mw(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/v1/ping", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("ACAO mirror: got %q want %q", got, "https://app.example.com")
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("credentials header: got %q want %q", got, "true")
	}
	if got := rr.Header().Get("Vary"); got != "Origin" {
		t.Errorf("vary: got %q want %q", got, "Origin")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d want 200", rr.Code)
	}
}

func TestCORS_DisallowedOriginNotMirrored(t *testing.T) {
	mw := CORS("https://app.example.com")
	h := mw(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/v1/ping", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("evil origin should not be mirrored: got %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("credentials should be absent for disallowed origin: got %q", got)
	}
	// Non-OPTIONS still reaches the handler — only the CORS headers are
	// withheld; the browser is the entity that refuses the response.
	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d want 200", rr.Code)
	}
}

func TestCORS_OptionsShortCircuit204(t *testing.T) {
	mw := CORS("https://app.example.com")
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/v1/ping", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if called {
		t.Fatalf("OPTIONS must short-circuit before the handler")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status: got %d want 204", rr.Code)
	}
	// Allow-headers etc. still set on the preflight response.
	if !strings.Contains(rr.Header().Get("Access-Control-Allow-Methods"), "POST") {
		t.Errorf("Allow-Methods should contain POST: got %q", rr.Header().Get("Access-Control-Allow-Methods"))
	}
}

func TestCORS_EmptyAllowedIsNoOp(t *testing.T) {
	mw := CORS("")
	h := mw(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/v1/ping", nil)
	req.Header.Set("Origin", "https://anything.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("empty allowed should not set ACAO: got %q", got)
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d want 200", rr.Code)
	}
}

// ----- RBAC ------------------------------------------------------------

func TestRequireRole_OwnerOnlyRejectsAdminAndMember(t *testing.T) {
	cases := []struct {
		name string
		role domain.Role
		want int
	}{
		{"owner accepted", domain.RoleOwner, http.StatusOK},
		{"admin rejected", domain.RoleAdmin, http.StatusForbidden},
		{"member rejected", domain.RoleMember, http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mw := RequireRole(domain.RoleOwner)
			h := mw(okHandler)

			req := httptest.NewRequest(http.MethodGet, "/owner-only", nil)
			ctx := WithPrincipal(req.Context(), domain.Principal{
				UserID: "u", OrgID: "o", Role: tc.role,
			})
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req.WithContext(ctx))

			if rr.Code != tc.want {
				t.Fatalf("status: got %d want %d body=%q", rr.Code, tc.want, rr.Body.String())
			}
			if tc.want == http.StatusForbidden {
				if !strings.Contains(rr.Body.String(), "forbidden") {
					t.Errorf("body should mention forbidden: %q", rr.Body.String())
				}
				if rr.Header().Get("Content-Type") != "application/json" {
					t.Errorf("forbidden response should be JSON")
				}
			}
		})
	}
}

func TestRequireRole_OwnerOrAdminAcceptsBothRejectsMember(t *testing.T) {
	mw := RequireRole(domain.RoleOwner, domain.RoleAdmin)
	h := mw(okHandler)

	for _, role := range []domain.Role{domain.RoleOwner, domain.RoleAdmin} {
		req := httptest.NewRequest(http.MethodGet, "/admin-or-owner", nil)
		ctx := WithPrincipal(req.Context(), domain.Principal{
			UserID: "u", OrgID: "o", Role: role,
		})
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req.WithContext(ctx))
		if rr.Code != http.StatusOK {
			t.Errorf("role %s: expected 200, got %d", role, rr.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/admin-or-owner", nil)
	ctx := WithPrincipal(req.Context(), domain.Principal{
		UserID: "u", OrgID: "o", Role: domain.RoleMember,
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req.WithContext(ctx))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("member should be rejected: got %d", rr.Code)
	}
}

func TestRequireRole_NoPrincipalReturns401(t *testing.T) {
	mw := RequireRole(domain.RoleOwner)
	h := mw(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/owner-only", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no principal: got %d want 401", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "unauthorized") {
		t.Errorf("body should mention unauthorized: %q", rr.Body.String())
	}
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Errorf("unauthorized response should be JSON")
	}
}

// ----- Principal helpers (round-trip) -------------------------------------

func TestWithPrincipal_PrincipalFromRoundTrip(t *testing.T) {
	want := domain.Principal{
		UserID:    "user-123",
		OrgID:     "org-456",
		Role:      domain.RoleAdmin,
		SessionID: "sess-789",
	}
	ctx := WithPrincipal(context.Background(), want)
	got, ok := PrincipalFrom(ctx)
	if !ok {
		t.Fatalf("PrincipalFrom: not found in ctx")
	}
	if got != want {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestPrincipalFrom_EmptyCtxReturnsFalse(t *testing.T) {
	_, ok := PrincipalFrom(context.Background())
	if ok {
		t.Fatalf("empty ctx should not have a principal")
	}
}
