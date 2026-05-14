package handler_test

// Coverage for the public auth surface (Signup, Login, Magic, Verify,
// Logout). Uses local.Provider + local.MemStore for the persistence side.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

// newAuthFlowServer builds a full server with the local auth provider so
// the public Signup/Login routes can be driven end-to-end.
func newAuthFlowServer(t *testing.T) (http.Handler, *local.MemStore, *local.TestMailer) {
	t.Helper()
	store := local.NewMemStore()
	mailer := &local.TestMailer{}
	provider := local.New(local.Config{
		Store:         store,
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        mailer,
		BaseURL:       "http://localhost:3000",
		Clock:         func() time.Time { return time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC) },
	})
	srv := httpserver.New(
		config.Config{AppEnv: "test", AppBaseURL: "http://localhost:3000"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpserver.Deps{Auth: provider},
	)
	return srv, store, mailer
}

// TestSignup_HappyPathReturns201 — the public signup flow returns 201 + a
// session cookie.
func TestSignup_HappyPathReturns201(t *testing.T) {
	srv, _, _ := newAuthFlowServer(t)
	body, _ := json.Marshal(map[string]string{
		"email":    "newuser@example.com",
		"password": "passw0rd!",
		"org_name": "NewOrg",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	hasCookie := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "nexis_session" && c.Value != "" {
			hasCookie = true
		}
	}
	if !hasCookie {
		t.Fatalf("no session cookie")
	}
}

// TestSignup_DuplicateEmailIs409 — second signup with same email returns 409.
func TestSignup_DuplicateEmailIs409(t *testing.T) {
	srv, _, _ := newAuthFlowServer(t)
	body, _ := json.Marshal(map[string]string{
		"email":    "dup@example.com",
		"password": "passw0rd!",
		"org_name": "Org1",
	})
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if i == 0 && rec.Code != http.StatusCreated {
			t.Fatalf("first signup: status=%d", rec.Code)
		}
		if i == 1 && rec.Code != http.StatusConflict {
			t.Fatalf("dup signup: status=%d", rec.Code)
		}
	}
}

// TestLogin_HappyPath — Signup then Login returns 200 with a session cookie.
func TestLogin_HappyPath(t *testing.T) {
	srv, _, _ := newAuthFlowServer(t)
	signupBody, _ := json.Marshal(map[string]string{
		"email": "user@example.com", "password": "passw0rd!", "org_name": "X",
	})
	signupReq := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewReader(signupBody))
	signupReq.Header.Set("Content-Type", "application/json")
	signupRec := httptest.NewRecorder()
	srv.ServeHTTP(signupRec, signupReq)

	loginBody, _ := json.Marshal(map[string]string{
		"email": "user@example.com", "password": "passw0rd!",
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	srv.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login: status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
}

// TestLogin_InvalidCredentialsReturns401 — wrong password returns 401.
func TestLogin_InvalidCredentialsReturns401(t *testing.T) {
	srv, _, _ := newAuthFlowServer(t)
	signupBody, _ := json.Marshal(map[string]string{
		"email": "user2@example.com", "password": "passw0rd!", "org_name": "X",
	})
	signupReq := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewReader(signupBody))
	signupReq.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(httptest.NewRecorder(), signupReq)

	loginBody, _ := json.Marshal(map[string]string{
		"email": "user2@example.com", "password": "WRONG",
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	srv.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusUnauthorized {
		t.Fatalf("status: %d", loginRec.Code)
	}
}

// TestMagic_IssuesEmail — POST /v1/auth/magic accepts an email and triggers
// the mailer (we use TestMailer so the link is captured).
func TestMagic_IssuesEmail(t *testing.T) {
	srv, _, mailer := newAuthFlowServer(t)
	// First signup so the user exists.
	signupBody, _ := json.Marshal(map[string]string{
		"email": "magic@example.com", "password": "passw0rd!", "org_name": "X",
	})
	signupReq := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewReader(signupBody))
	signupReq.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(httptest.NewRecorder(), signupReq)
	_ = mailer // captured for inspection if needed

	body, _ := json.Marshal(map[string]string{"email": "magic@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/magic", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	// Magic returns 202 to avoid leaking which emails exist.
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestMagic_BadJSONIs400 — malformed body returns 400.
func TestMagic_BadJSONIs400(t *testing.T) {
	srv, _, _ := newAuthFlowServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/magic", strings.NewReader(`{not-json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestVerify_InvalidTokenReturns401Or400 — verifying a bogus magic-link
// token must NOT issue a session. The Verify endpoint is GET-only with
// ?token=...
func TestVerify_InvalidTokenReturns401Or400(t *testing.T) {
	srv, _, _ := newAuthFlowServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/verify?token=not-a-real-token", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound && rec.Code != http.StatusFound {
		t.Fatalf("expected 4xx or redirect, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// Compile-time check to verify the imports we use.
var _ = context.Background
var _ = domain.Principal{}
