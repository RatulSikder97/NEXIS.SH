package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeAuthProvider is a minimal stub satisfying domain.AuthProvider. Only the
// two verify methods Auth uses are implemented; everything else panics if
// touched — surfaces an accidental dependency immediately instead of returning
// a misleading zero value.
type fakeAuthProvider struct {
	verifyToken func(ctx context.Context, token string) (domain.Principal, error)
	verifyAPI   func(ctx context.Context, key string) (domain.Principal, error)
}

func (f *fakeAuthProvider) Name() string { return "fake" }
func (f *fakeAuthProvider) Signup(_ context.Context, _ domain.SignupInput) (domain.SignupResult, error) {
	panic("Signup not used in middleware tests")
}
func (f *fakeAuthProvider) Login(_ context.Context, _ domain.LoginInput) (domain.SessionToken, error) {
	panic("Login not used in middleware tests")
}
func (f *fakeAuthProvider) VerifyToken(ctx context.Context, token string) (domain.Principal, error) {
	if f.verifyToken == nil {
		return domain.Principal{}, errors.New("VerifyToken not configured")
	}
	return f.verifyToken(ctx, token)
}
func (f *fakeAuthProvider) Logout(_ context.Context, _ string) error { return nil }
func (f *fakeAuthProvider) IssueMagicLink(_ context.Context, _, _ string) error {
	panic("IssueMagicLink not used")
}
func (f *fakeAuthProvider) ConsumeMagicLink(_ context.Context, _ string) (domain.SessionToken, error) {
	panic("ConsumeMagicLink not used")
}
func (f *fakeAuthProvider) ConsumeOAuthCode(_ context.Context, _ string) (domain.SignupResult, error) {
	panic("ConsumeOAuthCode not used")
}
func (f *fakeAuthProvider) EnrollMFA(_ context.Context, _ string) ([]byte, string, error) {
	panic("EnrollMFA not used")
}
func (f *fakeAuthProvider) VerifyMFA(_ context.Context, _, _ string) error {
	panic("VerifyMFA not used")
}
func (f *fakeAuthProvider) DisableMFA(_ context.Context, _ string) error {
	panic("DisableMFA not used")
}
func (f *fakeAuthProvider) CreateAPIKey(_ context.Context, _ domain.Principal, _ string, _ []string) (domain.APIKeyCreated, error) {
	panic("CreateAPIKey not used")
}
func (f *fakeAuthProvider) ListAPIKeys(_ context.Context, _ domain.Principal) ([]domain.APIKey, error) {
	panic("ListAPIKeys not used")
}
func (f *fakeAuthProvider) RevokeAPIKey(_ context.Context, _ domain.Principal, _ string) error {
	panic("RevokeAPIKey not used")
}
func (f *fakeAuthProvider) VerifyAPIKey(ctx context.Context, key string) (domain.Principal, error) {
	if f.verifyAPI == nil {
		return domain.Principal{}, errors.New("VerifyAPIKey not configured")
	}
	return f.verifyAPI(ctx, key)
}
func (f *fakeAuthProvider) GetUser(_ context.Context, _ string) (domain.User, error) {
	panic("GetUser not used")
}
func (f *fakeAuthProvider) GetOrg(_ context.Context, _ string) (domain.Organization, error) {
	panic("GetOrg not used")
}
func (f *fakeAuthProvider) IssueInvite(_ context.Context, _ domain.Principal, _ string, _ domain.Role) (string, error) {
	panic("IssueInvite not used")
}
func (f *fakeAuthProvider) GetInviteInfo(_ context.Context, _ string) (domain.InviteInfo, error) {
	panic("GetInviteInfo not used")
}
func (f *fakeAuthProvider) ClaimInvite(_ context.Context, _, _ string) (domain.SessionToken, error) {
	panic("ClaimInvite not used")
}
func (f *fakeAuthProvider) ListInvites(_ context.Context, _ domain.Principal) ([]domain.Invite, error) {
	panic("ListInvites not used")
}
func (f *fakeAuthProvider) RevokeInvite(_ context.Context, _ domain.Principal, _ string) error {
	panic("RevokeInvite not used")
}
func (f *fakeAuthProvider) RequestPasswordReset(_ context.Context, _ string) error {
	panic("RequestPasswordReset not used")
}
func (f *fakeAuthProvider) ResetPassword(_ context.Context, _, _ string) error {
	panic("ResetPassword not used")
}
func (f *fakeAuthProvider) ListSessions(_ context.Context, _ string) ([]domain.Session, error) {
	panic("ListSessions not used")
}
func (f *fakeAuthProvider) RevokeSession(_ context.Context, _, _ string) error {
	panic("RevokeSession not used")
}

// compile-time conformance check
var _ domain.AuthProvider = (*fakeAuthProvider)(nil)

// ----- Auth middleware --------------------------------------------------

func TestAuth_NoTokenLeavesCtxAlone(t *testing.T) {
	fp := &fakeAuthProvider{
		verifyToken: func(_ context.Context, _ string) (domain.Principal, error) {
			t.Fatalf("VerifyToken must not be called when no token is present")
			return domain.Principal{}, nil
		},
	}
	var seenPrincipal bool
	h := Auth(fp)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, seenPrincipal = PrincipalFrom(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if seenPrincipal {
		t.Fatalf("no token: principal must be absent in ctx")
	}
}

func TestAuth_ValidJWT_PutsPrincipalInCtx(t *testing.T) {
	want := domain.Principal{
		UserID: "user-1", OrgID: "org-1", Role: domain.RoleOwner, SessionID: "sess-1",
	}
	fp := &fakeAuthProvider{
		verifyToken: func(_ context.Context, token string) (domain.Principal, error) {
			if token != "good.jwt.token" {
				t.Errorf("unexpected token %q", token)
			}
			return want, nil
		},
		verifyAPI: func(_ context.Context, _ string) (domain.Principal, error) {
			t.Fatalf("VerifyAPIKey must not be called for non-nx_live_ tokens")
			return domain.Principal{}, nil
		},
	}
	var got domain.Principal
	var ok bool
	h := Auth(fp)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok = PrincipalFrom(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer good.jwt.token")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !ok {
		t.Fatalf("principal missing from ctx after Auth")
	}
	if got != want {
		t.Fatalf("principal mismatch: got %+v want %+v", got, want)
	}
}

func TestAuth_ExpiredOrBadJWT_LeavesCtxEmpty(t *testing.T) {
	fp := &fakeAuthProvider{
		verifyToken: func(_ context.Context, _ string) (domain.Principal, error) {
			return domain.Principal{}, errors.New("token expired")
		},
	}
	var sawPrincipal bool
	h := Auth(fp)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, sawPrincipal = PrincipalFrom(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer expired.jwt.token")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if sawPrincipal {
		t.Fatalf("bad token should leave ctx without a principal")
	}
	// Auth alone does NOT reject — RequireAuth issues the 401.
	if rr.Code != http.StatusOK {
		t.Fatalf("Auth alone should not write a status; got %d", rr.Code)
	}
}

func TestAuth_APIKeyPrefixRoutesToVerifyAPIKey(t *testing.T) {
	want := domain.Principal{
		UserID: "u", OrgID: "o", Role: domain.RoleAdmin,
	}
	fp := &fakeAuthProvider{
		verifyAPI: func(_ context.Context, key string) (domain.Principal, error) {
			if !strings.HasPrefix(key, "nx_live_") {
				t.Errorf("expected nx_live_ prefix, got %q", key)
			}
			return want, nil
		},
		verifyToken: func(_ context.Context, _ string) (domain.Principal, error) {
			t.Fatalf("VerifyToken must NOT be called for nx_live_ tokens")
			return domain.Principal{}, nil
		},
	}
	var got domain.Principal
	var ok bool
	h := Auth(fp)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok = PrincipalFrom(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer nx_live_abc123")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !ok {
		t.Fatalf("principal missing")
	}
	if got != want {
		t.Fatalf("principal mismatch: got %+v want %+v", got, want)
	}
}

func TestAuth_SessionCookieExtraction(t *testing.T) {
	want := domain.Principal{UserID: "u", OrgID: "o", Role: domain.RoleMember}
	fp := &fakeAuthProvider{
		verifyToken: func(_ context.Context, token string) (domain.Principal, error) {
			if token != "cookie.jwt" {
				t.Errorf("unexpected cookie token %q", token)
			}
			return want, nil
		},
	}
	var got domain.Principal
	var ok bool
	h := Auth(fp)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok = PrincipalFrom(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "cookie.jwt", Expires: time.Now().Add(time.Hour)})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !ok {
		t.Fatalf("principal missing — cookie token not extracted")
	}
	if got != want {
		t.Fatalf("principal mismatch: got %+v want %+v", got, want)
	}
}

func TestAuth_AuthorizationHeaderPreferredOverCookie(t *testing.T) {
	saw := []string{}
	fp := &fakeAuthProvider{
		verifyToken: func(_ context.Context, token string) (domain.Principal, error) {
			saw = append(saw, token)
			return domain.Principal{UserID: "u", OrgID: "o", Role: domain.RoleOwner}, nil
		},
	}
	h := Auth(fp)(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer header.jwt")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "cookie.jwt"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if len(saw) != 1 || saw[0] != "header.jwt" {
		t.Fatalf("expected exactly one VerifyToken call with header token, got %+v", saw)
	}
}

// ----- RequireAuth ------------------------------------------------------

func TestRequireAuth_NoPrincipalReturns401JSON(t *testing.T) {
	h := RequireAuth(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", rr.Code)
	}
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type: got %q want application/json", rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Body.String(), "unauthorized") {
		t.Errorf("body must mention unauthorized: %q", rr.Body.String())
	}
}

func TestRequireAuth_PrincipalInCtxPassesThrough(t *testing.T) {
	called := false
	h := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	ctx := WithPrincipal(req.Context(), domain.Principal{
		UserID: "u", OrgID: "o", Role: domain.RoleOwner,
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req.WithContext(ctx))

	if !called {
		t.Fatalf("handler not called despite principal")
	}
	if rr.Code != http.StatusTeapot {
		t.Errorf("status forwarded: got %d want 418", rr.Code)
	}
}
