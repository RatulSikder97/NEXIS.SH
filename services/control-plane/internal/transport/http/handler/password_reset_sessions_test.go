package handler_test

// Phase 9 coverage — password-reset + session handlers. The routes are
// registered by the router wiring pass; here we drive the handler funcs
// directly (chi RouteContext for the {id} param, appmw.WithPrincipal for the
// authenticated pair) against a MemStore-backed local provider.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// newResetTestProvider builds a live-clock provider (JWT exp is validated
// against wall-clock by jwt/v5, so a fixed past clock would break VerifyToken).
func newResetTestProvider(t *testing.T) (*local.Provider, *local.TestMailer) {
	t.Helper()
	mailer := &local.TestMailer{}
	p := local.New(local.Config{
		Store:         local.NewMemStore(),
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        mailer,
		BaseURL:       "http://localhost:3000",
		Clock:         func() time.Time { return time.Now().UTC() },
	})
	return p, mailer
}

func postJSON(t *testing.T, h http.HandlerFunc, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func resetTokenFrom(t *testing.T, mailer *local.TestMailer) string {
	t.Helper()
	link := mailer.LastLink()
	const marker = "token="
	i := strings.Index(link, marker)
	if i < 0 {
		t.Fatalf("no token in link %q", link)
	}
	return link[i+len(marker):]
}

func TestPasswordResetRequest_Returns202ForKnownAndUnknown(t *testing.T) {
	p, mailer := newResetTestProvider(t)
	if _, err := p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "pw-longenough", OrgName: "X"}); err != nil {
		t.Fatalf("setup signup: %v", err)
	}
	h := handler.PasswordResetRequest(p)

	rec := postJSON(t, h, "/v1/auth/password-reset/request", map[string]string{"email": "a@b.com"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("known email status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(mailer.LastLink(), "/reset-password?token=") {
		t.Fatalf("reset link shape: %q", mailer.LastLink())
	}

	// Unknown email: same 202, no mail — no account-existence oracle.
	before := mailer.LastLink()
	rec = postJSON(t, h, "/v1/auth/password-reset/request", map[string]string{"email": "ghost@b.com"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("unknown email status: %d", rec.Code)
	}
	if mailer.LastLink() != before {
		t.Fatalf("mailer fired for unknown email")
	}

	// Missing email → 400.
	rec = postJSON(t, h, "/v1/auth/password-reset/request", map[string]string{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty email status: %d", rec.Code)
	}
}

func TestPasswordResetConfirm_HappyPathThenReplayFails(t *testing.T) {
	p, mailer := newResetTestProvider(t)
	ctx := context.Background()
	if _, err := p.Signup(ctx, domain.SignupInput{Email: "a@b.com", Password: "old-password", OrgName: "X"}); err != nil {
		t.Fatalf("setup signup: %v", err)
	}
	if err := p.RequestPasswordReset(ctx, "a@b.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	tok := resetTokenFrom(t, mailer)
	h := handler.PasswordResetConfirm(p, config.Config{AppEnv: "test"})

	rec := postJSON(t, h, "/v1/auth/password-reset/confirm", map[string]string{
		"token": tok, "new_password": "brand-new-password",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("confirm status: %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := p.Login(ctx, domain.LoginInput{Email: "a@b.com", Password: "brand-new-password"}); err != nil {
		t.Fatalf("login with new password: %v", err)
	}

	// Replay → 401 with the non-leaky message.
	rec = postJSON(t, h, "/v1/auth/password-reset/confirm", map[string]string{
		"token": tok, "new_password": "another-password",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replay status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid or expired reset token") {
		t.Fatalf("replay body: %s", rec.Body.String())
	}
}

func TestPasswordResetConfirm_Validation(t *testing.T) {
	p, _ := newResetTestProvider(t)
	h := handler.PasswordResetConfirm(p, config.Config{AppEnv: "test"})

	rec := postJSON(t, h, "/v1/auth/password-reset/confirm", map[string]string{
		"token": "", "new_password": "long-enough-pw",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty token status: %d", rec.Code)
	}
	rec = postJSON(t, h, "/v1/auth/password-reset/confirm", map[string]string{
		"token": "sometoken", "new_password": "short",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("short password status: %d", rec.Code)
	}
	// Unknown token with valid shape → 401, not 404 (no token oracle).
	rec = postJSON(t, h, "/v1/auth/password-reset/confirm", map[string]string{
		"token": "definitely-not-issued", "new_password": "long-enough-pw",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token status: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSessionsListAndRevoke(t *testing.T) {
	p, _ := newResetTestProvider(t)
	ctx := context.Background()
	res, err := p.Signup(ctx, domain.SignupInput{
		Email: "a@b.com", Password: "pw-longenough", OrgName: "X",
		UserAgent: "SignupBrowser/1.0",
	})
	if err != nil {
		t.Fatalf("setup signup: %v", err)
	}
	if _, err := p.Login(ctx, domain.LoginInput{Email: "a@b.com", Password: "pw-longenough", UserAgent: "LoginBrowser/2.0"}); err != nil {
		t.Fatalf("setup login: %v", err)
	}
	princ, err := p.VerifyToken(ctx, res.Session.Token)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}

	// GET /v1/me/sessions
	req := httptest.NewRequest(http.MethodGet, "/v1/me/sessions", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	handler.SessionsList(p)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status: %d body=%s", rec.Code, rec.Body.String())
	}
	var sessions []dto.SessionResp
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(sessions))
	}
	var current, other *dto.SessionResp
	for i := range sessions {
		if sessions[i].IsCurrent {
			current = &sessions[i]
		} else {
			other = &sessions[i]
		}
	}
	if current == nil || other == nil {
		t.Fatalf("current/other split: %+v", sessions)
	}
	if current.ID != princ.SessionID {
		t.Fatalf("is_current mismatch: %q vs %q", current.ID, princ.SessionID)
	}
	if current.UserAgent != "SignupBrowser/1.0" {
		t.Fatalf("current UA: %q", current.UserAgent)
	}

	// DELETE /v1/me/sessions/{id} on the other session.
	req = httptest.NewRequest(http.MethodDelete, "/v1/me/sessions/"+other.ID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", other.ID)
	req = req.WithContext(context.WithValue(appmw.WithPrincipal(req.Context(), princ), chi.RouteCtxKey, rctx))
	rec = httptest.NewRecorder()
	handler.SessionRevoke(p, nil)(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status: %d body=%s", rec.Code, rec.Body.String())
	}

	// The list shrinks to the current session only.
	req = httptest.NewRequest(http.MethodGet, "/v1/me/sessions", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec = httptest.NewRecorder()
	handler.SessionsList(p)(rec, req)
	sessions = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode after revoke: %v", err)
	}
	if len(sessions) != 1 || !sessions[0].IsCurrent {
		t.Fatalf("after revoke: %+v", sessions)
	}

	// Revoking an unknown id → 404.
	req = httptest.NewRequest(http.MethodDelete, "/v1/me/sessions/nope", nil)
	rctx = chi.NewRouteContext()
	rctx.URLParams.Add("id", "nope")
	req = req.WithContext(context.WithValue(appmw.WithPrincipal(req.Context(), princ), chi.RouteCtxKey, rctx))
	rec = httptest.NewRecorder()
	handler.SessionRevoke(p, nil)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id status: %d", rec.Code)
	}
}
