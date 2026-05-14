package handler_test

// Coverage for the org-invite handlers. Uses local.MemStore + local.Provider
// so the persistence side runs without Postgres.

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
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// newInviteAuth constructs a local Provider + a seeded principal owner.
func newInviteAuth(t *testing.T) (*local.Provider, domain.Principal) {
	t.Helper()
	store := local.NewMemStore()
	provider := local.New(local.Config{
		Store:         store,
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        &local.TestMailer{},
		BaseURL:       "http://localhost:3000",
		Clock:         func() time.Time { return time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC) },
	})
	res, err := provider.Signup(context.Background(), domain.SignupInput{
		Email: "alice@example.com", Password: "passw0rd!", OrgName: "AliceCo",
	})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	princ := domain.Principal{
		UserID: res.User.ID, OrgID: res.Org.ID, Role: domain.RoleOwner,
	}
	return provider, princ
}

// TestInviteIssue_HappyPath — owner issues an invite; response carries the
// token_prefix.
func TestInviteIssue_HappyPath(t *testing.T) {
	auth, princ := newInviteAuth(t)
	h := handler.InviteIssue(auth, noopAuditWriter{})
	r := chi.NewRouter()
	r.Post("/v1/orgs/{id}/invites", h)
	body, _ := json.Marshal(map[string]string{"email": "bob@example.com", "role": "member"})
	req := httptest.NewRequest(http.MethodPost, "/v1/orgs/"+princ.OrgID+"/invites", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "token_prefix") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestInviteIssue_CrossOrgForbidden — issuing to a different org id is
// rejected even from an owner of org A.
func TestInviteIssue_CrossOrgForbidden(t *testing.T) {
	auth, princ := newInviteAuth(t)
	h := handler.InviteIssue(auth, noopAuditWriter{})
	r := chi.NewRouter()
	r.Post("/v1/orgs/{id}/invites", h)
	body, _ := json.Marshal(map[string]string{"email": "x@x.com", "role": "member"})
	req := httptest.NewRequest(http.MethodPost, "/v1/orgs/other-org/invites", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestInviteIssue_BadJSONIs400 — malformed body returns 400.
func TestInviteIssue_BadJSONIs400(t *testing.T) {
	auth, princ := newInviteAuth(t)
	h := handler.InviteIssue(auth, noopAuditWriter{})
	r := chi.NewRouter()
	r.Post("/v1/orgs/{id}/invites", h)
	body := bytes.NewReader([]byte(`{not-json`))
	req := httptest.NewRequest(http.MethodPost, "/v1/orgs/"+princ.OrgID+"/invites", body)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestInviteIssue_InvalidRoleIs400 — role must be admin or member.
func TestInviteIssue_InvalidRoleIs400(t *testing.T) {
	auth, princ := newInviteAuth(t)
	h := handler.InviteIssue(auth, noopAuditWriter{})
	r := chi.NewRouter()
	r.Post("/v1/orgs/{id}/invites", h)
	body, _ := json.Marshal(map[string]string{"email": "x@x.com", "role": "owner"}) // owner not allowed via invite
	req := httptest.NewRequest(http.MethodPost, "/v1/orgs/"+princ.OrgID+"/invites", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestInviteList_HappyPath — list returns the invites for the principal's
// org.
func TestInviteList_HappyPath(t *testing.T) {
	auth, princ := newInviteAuth(t)
	// Issue one first.
	_, err := auth.IssueInvite(context.Background(), princ, "bob@example.com", domain.RoleMember)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	h := handler.InviteList(auth)
	r := chi.NewRouter()
	r.Get("/v1/orgs/{id}/invites", h)
	req := httptest.NewRequest(http.MethodGet, "/v1/orgs/"+princ.OrgID+"/invites", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bob@example.com") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestInviteList_CrossOrgForbidden — listing another org returns 403.
func TestInviteList_CrossOrgForbidden(t *testing.T) {
	auth, princ := newInviteAuth(t)
	h := handler.InviteList(auth)
	r := chi.NewRouter()
	r.Get("/v1/orgs/{id}/invites", h)
	req := httptest.NewRequest(http.MethodGet, "/v1/orgs/other-org/invites", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestInviteGet_PublicShape — the public endpoint returns org info for a
// valid token. (Uses the IssueInvite return token verbatim, not the hash.)
func TestInviteGet_PublicShape(t *testing.T) {
	auth, princ := newInviteAuth(t)
	token, err := auth.IssueInvite(context.Background(), princ, "carol@example.com", domain.RoleMember)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	h := handler.InviteGet(auth)
	r := chi.NewRouter()
	r.Get("/v1/invites/{token}", h)
	req := httptest.NewRequest(http.MethodGet, "/v1/invites/"+token, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "AliceCo") {
		t.Fatalf("body missing org name: %s", rec.Body.String())
	}
}

// TestInviteGet_InvalidTokenIs404 — unknown tokens return 404.
func TestInviteGet_InvalidTokenIs404(t *testing.T) {
	auth, _ := newInviteAuth(t)
	h := handler.InviteGet(auth)
	r := chi.NewRouter()
	r.Get("/v1/invites/{token}", h)
	req := httptest.NewRequest(http.MethodGet, "/v1/invites/not-a-real-token", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestInviteRevoke_CrossOrgForbidden — revoke gates on org match.
func TestInviteRevoke_CrossOrgForbidden(t *testing.T) {
	auth, princ := newInviteAuth(t)
	h := handler.InviteRevoke(auth, noopAuditWriter{})
	r := chi.NewRouter()
	r.Delete("/v1/orgs/{id}/invites/{token_hash}", h)
	req := httptest.NewRequest(http.MethodDelete, "/v1/orgs/other-org/invites/deadbeef", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestInviteClaim_InvalidTokenIs400 — claiming an invalid token returns 400.
func TestInviteClaim_InvalidTokenIs400(t *testing.T) {
	auth, _ := newInviteAuth(t)
	h := handler.InviteClaim(auth, config.Config{AppEnv: "dev"})
	r := chi.NewRouter()
	r.Post("/v1/invites/{token}/claim", h)
	body := bytes.NewReader([]byte(`{"password": "new-pass"}`))
	req := httptest.NewRequest(http.MethodPost, "/v1/invites/not-a-real-token/claim", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}
