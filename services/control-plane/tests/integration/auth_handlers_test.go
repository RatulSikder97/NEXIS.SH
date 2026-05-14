package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
)

// newAuthServer builds an httptest-ready handler wired with an in-memory
// auth provider on a fixed clock. The returned local.Provider is exposed for
// tests that need to drive MFA verification directly without going through
// the QR-scan dance.
func newAuthServer(t *testing.T) (http.Handler, *local.Provider) {
	t.Helper()
	store := local.NewMemStore()
	p := local.New(local.Config{
		Store:         store,
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        &local.TestMailer{},
		BaseURL:       "http://localhost:3000",
		Clock:         func() time.Time { return time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC) },
	})
	srv := httpserver.New(
		config.Config{AppEnv: "test", AppBaseURL: "http://localhost:3000"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpserver.Deps{Auth: p},
	)
	return srv, p
}

// doJSON issues a request with a JSON body, returning the recorder for assertions.
func doJSON(t *testing.T, srv http.Handler, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	if rdr != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// sessionCookieFrom extracts the nexis_session cookie from a Set-Cookie header,
// failing the test if none was emitted.
func sessionCookieFrom(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "nexis_session" {
			return c
		}
	}
	t.Fatalf("no nexis_session cookie in response (status=%d headers=%v)", rec.Code, rec.Result().Header)
	return nil
}

// signup runs a /v1/auth/signup and returns the resulting session cookie. It
// asserts a 201 status code; tests that expect different outcomes should not
// use this helper.
func signup(t *testing.T, srv http.Handler, email, password, org string) *http.Cookie {
	t.Helper()
	rec := doJSON(t, srv, http.MethodPost, "/v1/auth/signup", map[string]string{
		"email":    email,
		"password": password,
		"org_name": org,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	return sessionCookieFrom(t, rec)
}

func TestAuth_Signup_SetsHttpOnlyCookie(t *testing.T) {
	srv, _ := newAuthServer(t)
	rec := doJSON(t, srv, http.MethodPost, "/v1/auth/signup", map[string]string{
		"email":    "alice@example.com",
		"password": "correct horse battery staple",
		"org_name": "Alice Co",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	c := sessionCookieFrom(t, rec)
	if !c.HttpOnly {
		t.Errorf("session cookie is not HttpOnly")
	}
	// AppEnv != "dev" → Secure flag is set. We're running under AppEnv=test,
	// so Secure should be true. Dev mode is the only env that strips Secure.
	if !c.Secure {
		t.Errorf("session cookie missing Secure flag (AppEnv=test, non-dev)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if c.Value == "" {
		t.Errorf("empty session cookie value")
	}
	var body dto.AuthResp
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.UserID == "" || body.OrgID == "" {
		t.Errorf("missing ids in response: %+v", body)
	}
}

func TestAuth_Signup_DevEnvDropsSecureFlag(t *testing.T) {
	// Build a dev-env server so we can confirm the Secure flag is suppressed.
	store := local.NewMemStore()
	p := local.New(local.Config{
		Store:         store,
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        &local.TestMailer{},
		BaseURL:       "http://localhost:3000",
		Clock:         func() time.Time { return time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC) },
	})
	srv := httpserver.New(
		config.Config{AppEnv: "dev", AppBaseURL: "http://localhost:3000"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpserver.Deps{Auth: p},
	)
	rec := doJSON(t, srv, http.MethodPost, "/v1/auth/signup", map[string]string{
		"email":    "dev-secure@example.com",
		"password": "x",
		"org_name": "DevCo",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	c := sessionCookieFrom(t, rec)
	if c.Secure {
		t.Errorf("Secure flag set in AppEnv=dev; want false")
	}
}

func TestAuth_Signup_DuplicateEmailFails(t *testing.T) {
	srv, _ := newAuthServer(t)
	_ = signup(t, srv, "dup@example.com", "x", "X")
	rec := doJSON(t, srv, http.MethodPost, "/v1/auth/signup", map[string]string{
		"email":    "dup@example.com",
		"password": "x",
		"org_name": "Y",
	})
	if rec.Code < 400 {
		t.Fatalf("status = %d, want non-2xx for duplicate signup", rec.Code)
	}
}

func TestAuth_Login_WrongPassword(t *testing.T) {
	srv, _ := newAuthServer(t)
	_ = signup(t, srv, "alice@example.com", "right", "X")
	rec := doJSON(t, srv, http.MethodPost, "/v1/auth/login", map[string]string{
		"email":    "alice@example.com",
		"password": "wrong",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	var er dto.ErrorResp
	if err := json.NewDecoder(rec.Body).Decode(&er); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if er.Error == "" {
		t.Errorf("missing error message in 401 body")
	}
}

func TestAuth_Login_Success_SetsCookie(t *testing.T) {
	srv, _ := newAuthServer(t)
	_ = signup(t, srv, "bob@example.com", "secret", "B")
	rec := doJSON(t, srv, http.MethodPost, "/v1/auth/login", map[string]string{
		"email":    "bob@example.com",
		"password": "secret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	c := sessionCookieFrom(t, rec)
	if c.Value == "" {
		t.Errorf("empty session cookie")
	}
}

func TestAuth_Logout_RequiresAuth(t *testing.T) {
	srv, _ := newAuthServer(t)
	rec := doJSON(t, srv, http.MethodPost, "/v1/auth/logout", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuth_FullFlow_Logout_RevokesSession(t *testing.T) {
	srv, _ := newAuthServer(t)
	c := signup(t, srv, "carol@example.com", "x", "C")

	// Use the session — /v1/me should succeed.
	rec := doJSON(t, srv, http.MethodGet, "/v1/me", nil, c)
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1/me before logout = %d; body=%s", rec.Code, rec.Body.String())
	}

	// Logout.
	rec = doJSON(t, srv, http.MethodPost, "/v1/auth/logout", nil, c)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}

	// Reusing the same cookie must now be rejected.
	rec = doJSON(t, srv, http.MethodGet, "/v1/me", nil, c)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/v1/me after logout = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_Me_HappyPath(t *testing.T) {
	srv, _ := newAuthServer(t)
	c := signup(t, srv, "dave@example.com", "x", "Dave Co")
	rec := doJSON(t, srv, http.MethodGet, "/v1/me", nil, c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var me dto.MeResp
	if err := json.NewDecoder(rec.Body).Decode(&me); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if me.User.Email != "dave@example.com" {
		t.Errorf("user.email = %q", me.User.Email)
	}
	if me.Org.Name != "Dave Co" {
		t.Errorf("org.name = %q", me.Org.Name)
	}
	if me.Org.Slug != "dave-co" {
		t.Errorf("org.slug = %q, want dave-co", me.Org.Slug)
	}
	if me.Role != "owner" {
		t.Errorf("role = %q, want owner", me.Role)
	}
}

func TestAuth_MFA_EnrollReturnsDataURL(t *testing.T) {
	srv, _ := newAuthServer(t)
	c := signup(t, srv, "erin@example.com", "x", "E")
	rec := doJSON(t, srv, http.MethodPost, "/v1/auth/mfa/enroll", nil, c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body dto.MFAEnrollResp
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(body.QRDataURL, "data:image/png;base64,") {
		t.Errorf("qr_data_url prefix = %q (truncated)", first(body.QRDataURL, 40))
	}
	if body.RecoveryCodes == nil {
		t.Errorf("recovery_codes is nil, want empty slice")
	}
	// Secret-gating: newAuthServer uses AppEnv="test" (non-dev), so the
	// handler omits the raw TOTP secret to prevent a phishing-replay
	// scenario in production. The QR data URL is sufficient for a real
	// authenticator app; tooling that needs the secret should hit a
	// dev-mode server or read the value via the local.Provider directly.
	if body.Secret != "" {
		t.Errorf("secret should be empty for non-dev AppEnv; got %q", body.Secret)
	}
}

// TestAuth_MFA_EnrollLeaksSecretInDev asserts the dev escape hatch: when
// AppEnv == "dev" the raw TOTP secret IS surfaced so the E2E harness can
// deterministically compute a TOTP code without decoding the QR PNG. This
// guards the other direction of the gating: a refactor that accidentally
// strips the secret unconditionally would break the test harness.
func TestAuth_MFA_EnrollLeaksSecretInDev(t *testing.T) {
	store := local.NewMemStore()
	p := local.New(local.Config{
		Store:         store,
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        &local.TestMailer{},
		BaseURL:       "http://localhost:3000",
		Clock:         func() time.Time { return time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC) },
	})
	srv := httpserver.New(
		config.Config{AppEnv: "dev", AppBaseURL: "http://localhost:3000"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpserver.Deps{Auth: p},
	)
	c := signup(t, srv, "erin-dev@example.com", "x", "EDev")
	rec := doJSON(t, srv, http.MethodPost, "/v1/auth/mfa/enroll", nil, c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body dto.MFAEnrollResp
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Secret == "" {
		t.Errorf("AppEnv=dev should surface the raw TOTP secret for E2E tooling; got empty")
	}
}

func TestAuth_MFA_VerifyWrongCode(t *testing.T) {
	srv, _ := newAuthServer(t)
	c := signup(t, srv, "frank@example.com", "x", "F")
	// Enroll first so the user has a secret.
	rec := doJSON(t, srv, http.MethodPost, "/v1/auth/mfa/enroll", nil, c)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll status = %d; body=%s", rec.Code, rec.Body.String())
	}
	// Send an obviously wrong code.
	rec = doJSON(t, srv, http.MethodPost, "/v1/auth/mfa/verify",
		map[string]string{"code": "000000"}, c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_MFA_VerifyCorrectCode(t *testing.T) {
	srv, p := newAuthServer(t)
	c := signup(t, srv, "gina@example.com", "x", "G")
	rec := doJSON(t, srv, http.MethodPost, "/v1/auth/mfa/enroll", nil, c)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll: %d", rec.Code)
	}
	// Pull the principal/user so we can fetch the secret to produce a valid TOTP.
	// The handler discards the secret, so we go via the provider helper.
	princ, err := p.VerifyToken(t.Context(), c.Value)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	u, err := p.GetUser(t.Context(), princ.UserID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	code, err := totp.GenerateCode(u.MFASecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	rec = doJSON(t, srv, http.MethodPost, "/v1/auth/mfa/verify",
		map[string]string{"code": code}, c)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("verify status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_APIKey_CreateListRevoke(t *testing.T) {
	srv, _ := newAuthServer(t)
	c := signup(t, srv, "hank@example.com", "x", "H")

	// Create.
	rec := doJSON(t, srv, http.MethodPost, "/v1/apikeys",
		map[string]any{"name": "ci-token", "scopes": []string{"read"}}, c)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created dto.APIKeyCreatedResp
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if !strings.HasPrefix(created.Plaintext, "nx_live_") {
		t.Errorf("plaintext prefix = %q", first(created.Plaintext, 12))
	}
	if created.ID == "" {
		t.Fatal("empty id in create response")
	}

	// List shows the one key.
	rec = doJSON(t, srv, http.MethodGet, "/v1/apikeys", nil, c)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var keys []dto.APIKeyResp
	if err := json.NewDecoder(rec.Body).Decode(&keys); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(keys) != 1 || keys[0].ID != created.ID {
		t.Fatalf("list = %+v, want one entry with id %q", keys, created.ID)
	}

	// Revoke.
	rec = doJSON(t, srv, http.MethodDelete, "/v1/apikeys/"+created.ID, nil, c)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}

	// List again — the revoked key should still surface, but with a revoked
	// marker. The spec doesn't yet require hiding revoked rows from list, so
	// just make sure the endpoint still returns 200 and a JSON array.
	rec = doJSON(t, srv, http.MethodGet, "/v1/apikeys", nil, c)
	if rec.Code != http.StatusOK {
		t.Fatalf("list after revoke status = %d", rec.Code)
	}
	// Verifying the plaintext now must fail — confirm via the API by trying
	// to use the key as a bearer to access /v1/me.
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+created.Plaintext)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key still works: status=%d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestAuth_APIKey_AsBearer(t *testing.T) {
	srv, _ := newAuthServer(t)
	c := signup(t, srv, "ivy@example.com", "x", "I")
	rec := doJSON(t, srv, http.MethodPost, "/v1/apikeys",
		map[string]any{"name": "t1"}, c)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created dto.APIKeyCreatedResp
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Use the plaintext as a Bearer to call /v1/me.
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+created.Plaintext)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("/v1/me with api-key bearer = %d, body=%s", rec2.Code, rec2.Body.String())
	}
}

// first returns up to n characters of s — used in error messages.
func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
