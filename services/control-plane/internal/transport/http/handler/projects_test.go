package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	"github.com/nexis-eco/nexis/services/control-plane/internal/usecase"
)

// TestProjects_CRUDRoundTrip exercises every projects endpoint end-to-end
// against an in-memory auth provider + fake ProjectsService. The fake mirrors
// the storage semantics (workspace + slug uniqueness, archive, partial
// update) so the handler-side wire shapes can be asserted without standing
// up a real database.
func TestProjects_CRUDRoundTrip(t *testing.T) {
	cookie, srv, _, fake := setupProjectsTestServer(t, "owner")

	// CREATE
	createBody := map[string]any{
		"name":        "Orders API",
		"description": "primary orders service",
		"environment": "prod",
		"selectors": map[string]any{
			"github_repo":              "acme/orders-api",
			"github_installation_id":   12345,
			"github_default_branch":    "main",
			"sentry_organization_slug": "acme",
			"sentry_project_slug":      "orders-api-prod",
			"datadog_service_tag":      "service:orders-api",
		},
		"slo": map[string]any{
			"availability_target": 0.9995,
			"latency_p95_ms":      250,
			"error_rate_pct":      0.5,
		},
	}
	created := doRequest[dto.ProjectResp](t, srv, cookie, http.MethodPost, "/v1/workspaces/ws-1/projects", createBody, http.StatusCreated)
	if created.ID == "" || created.Slug != "orders-api" {
		t.Fatalf("create response wrong: %+v", created)
	}
	if created.Selectors.DatadogServiceTag != "service:orders-api" {
		t.Fatalf("selectors did not round-trip: %+v", created.Selectors)
	}
	if created.RecoveryPolicy.MediumCountdownSeconds != 120 || !created.RecoveryPolicy.RollbackOnSLOBreach {
		t.Fatalf("default policy missing: %+v", created.RecoveryPolicy)
	}

	// GET
	got := doRequest[dto.ProjectResp](t, srv, cookie, http.MethodGet, "/v1/projects/"+created.ID, nil, http.StatusOK)
	if got.ID != created.ID {
		t.Fatalf("get round-trip: id=%s want=%s", got.ID, created.ID)
	}

	// PATCH (name only — slug must stay)
	newName := "Orders v2"
	patchBody := map[string]any{"name": newName}
	patched := doRequest[dto.ProjectResp](t, srv, cookie, http.MethodPatch, "/v1/projects/"+created.ID, patchBody, http.StatusOK)
	if patched.Name != newName {
		t.Fatalf("patch name: %q", patched.Name)
	}
	if patched.Slug != created.Slug {
		t.Fatalf("slug changed on patch: was %q now %q", created.Slug, patched.Slug)
	}

	// PUT /recovery-policy
	newPolicy := dto.RecoveryPolicy{
		AutoMergeLowSeverity:    true,
		AutoMergeMediumSeverity: false,
		MediumCountdownSeconds:  300,
		KillSwitchEnabled:       false,
		ApproverUserIDs:         []string{},
		MaxConcurrentRecoveries: 2,
		RollbackOnSLOBreach:     true,
	}
	updatedPolicy := doRequest[dto.RecoveryPolicy](t, srv, cookie, http.MethodPut, "/v1/projects/"+created.ID+"/recovery-policy", newPolicy, http.StatusOK)
	if updatedPolicy.MediumCountdownSeconds != 300 || !updatedPolicy.AutoMergeLowSeverity {
		t.Fatalf("policy did not persist: %+v", updatedPolicy)
	}

	// GET policy after PUT — should match.
	gotPolicy := doRequest[dto.RecoveryPolicy](t, srv, cookie, http.MethodGet, "/v1/projects/"+created.ID+"/recovery-policy", nil, http.StatusOK)
	if gotPolicy.MediumCountdownSeconds != 300 {
		t.Fatalf("policy GET after PUT: %+v", gotPolicy)
	}

	// DELETE → 204
	req := httptest.NewRequest(http.MethodDelete, "/v1/projects/"+created.ID, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// LIST after delete — archived row must be absent.
	list := doRequest[[]dto.ProjectResp](t, srv, cookie, http.MethodGet, "/v1/workspaces/ws-1/projects", nil, http.StatusOK)
	for _, p := range list {
		if p.ID == created.ID {
			t.Fatalf("archived project leaked into list: %+v", p)
		}
	}

	_ = fake // silence linter; fake is exercised via the wire path
}

// TestProjects_MemberForbidden — members can read but cannot create.
func TestProjects_MemberForbidden(t *testing.T) {
	cookie, srv, _, _ := setupProjectsTestServer(t, "member")

	req := httptest.NewRequest(http.MethodPost, "/v1/workspaces/ws-1/projects",
		bytes.NewReader(mustJSON(t, map[string]any{
			"name":        "Member Attempt",
			"environment": "prod",
		})))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member create: status=%d body=%s want=403", rec.Code, rec.Body.String())
	}
}

// TestProjects_CrossTenant404 — a second org cannot Get a project owned by
// the first org. The handler hides foreign rows behind a uniform 404.
func TestProjects_CrossTenant404(t *testing.T) {
	cookieA, srv, _, _ := setupProjectsTestServer(t, "owner")

	// Org A creates a project.
	created := doRequest[dto.ProjectResp](t, srv, cookieA, http.MethodPost, "/v1/workspaces/ws-1/projects", map[string]any{
		"name":        "Tenant A",
		"environment": "prod",
	}, http.StatusCreated)

	// Build a SECOND signed-in identity in the same server instance. We
	// reuse the signup path through the auth provider so the cookie is
	// genuine; the auth provider is in-memory so the round-trip is fast.
	cookieB := signupAndLogin(t, srv, "b@example.com", "OrgB")

	// Org B's GET on Org A's project must return 404.
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+created.ID, nil)
	req.AddCookie(cookieB)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant get: status=%d body=%s want=404", rec.Code, rec.Body.String())
	}
}

// --- test harness -----------------------------------------------------------

// setupProjectsTestServer spins up an httpserver with an in-memory auth
// provider + a fake ProjectsService, signs up a single principal, promotes
// them to the requested role, and returns the session cookie + server. The
// role argument is one of "owner" / "admin" / "member".
func setupProjectsTestServer(t *testing.T, role string) (*http.Cookie, http.Handler, *local.MemStore, *fakeProjectsService) {
	t.Helper()
	store := local.NewMemStore()
	provider := local.New(local.Config{
		Store:         store,
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        &local.TestMailer{},
		BaseURL:       "http://localhost:3000",
	})

	fake := newFakeProjectsService()
	srv := httpserver.New(
		config.Config{AppEnv: "test", AppBaseURL: "http://localhost:3000"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpserver.Deps{
			Auth:     provider,
			Projects: fake,
		},
	)

	cookie := signupAndLogin(t, srv, "alice@example.com", "AliceCo")
	if role != "owner" {
		// Demote in the MemStore. The provider issued an owner role on
		// signup; tests for "admin"/"member" need to override.
		demoteMembership(t, store, "alice@example.com", role)
		// Re-issue cookie so the claim carries the new role.
		cookie = loginExistingUser(t, srv, "alice@example.com", "passw0rd!")
	}
	return cookie, srv, store, fake
}

// signupAndLogin posts /v1/auth/signup with a fresh email + org name and
// returns the session cookie set on the response.
func signupAndLogin(t *testing.T, srv http.Handler, email, orgName string) *http.Cookie {
	t.Helper()
	body := mustJSON(t, map[string]string{
		"email":    email,
		"password": "passw0rd!",
		"org_name": orgName + "-" + time.Now().UTC().Format("150405.000000"),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup: status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "nexis_session" {
			return c
		}
	}
	t.Fatalf("signup: no session cookie")
	return nil
}

// loginExistingUser re-issues a session cookie for an already-created user.
func loginExistingUser(t *testing.T, srv http.Handler, email, password string) *http.Cookie {
	t.Helper()
	body := mustJSON(t, map[string]string{"email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "nexis_session" {
			return c
		}
	}
	t.Fatalf("login: no session cookie")
	return nil
}

// demoteMembership flips the user's org_member role to "admin" or "member"
// in the in-memory store. The MemStore exposes CreateMembership as an upsert
// (the map assignment overwrites), so reissuing with a new role is the
// cleanest path to a non-owner principal in tests.
func demoteMembership(t *testing.T, store *local.MemStore, email, role string) {
	t.Helper()
	ctx := context.Background()
	user, err := store.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("lookup user %q: %v", email, err)
	}
	orgID, _, err := store.GetMembership(ctx, user.ID)
	if err != nil {
		t.Fatalf("lookup membership %q: %v", email, err)
	}
	if err := store.CreateMembership(ctx, orgID, user.ID, domain.Role(role)); err != nil {
		t.Fatalf("upsert membership %q: %v", email, err)
	}
}

// doRequest is the typed JSON request helper used by the round-trip test.
// Marshals the body, fires the request, asserts the status, and decodes the
// response into T.
func doRequest[T any](t *testing.T, srv http.Handler, cookie *http.Cookie, method, path string, body any, wantStatus int) T {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(mustJSON(t, body))
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	var out T
	if rec.Body.Len() == 0 {
		return out
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s %s: %v body=%s", method, path, err, rec.Body.String())
	}
	return out
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// fakeProjectsService implements handler.ProjectsService. The fake preserves
// the parts of usecase.ProjectsService semantics the handler tests need:
//
//   - slug auto-derived from the name
//   - workspace + slug uniqueness
//   - principal-scoped Get / Update / Archive (cross-tenant → ErrNotFound)
//
// The full usecase has more (audit, cap, GitHub binding); those are exercised
// in projects_test.go under internal/usecase. Here we just want the wire
// shape + RBAC plumbing to work.
type fakeProjectsService struct {
	mu      sync.Mutex
	byID    map[string]domain.Project
	bySlug  map[string]string // workspace_id||slug → id
	idCount int
}

func newFakeProjectsService() *fakeProjectsService {
	return &fakeProjectsService{
		byID:   map[string]domain.Project{},
		bySlug: map[string]string{},
	}
}

func (f *fakeProjectsService) Create(_ context.Context, princ domain.Principal, in usecase.CreateProjectInput) (domain.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if in.Name == "" {
		return domain.Project{}, errors.New("name required")
	}
	slug := fakeSlugify(in.Name)
	if slug == "" {
		slug = "project"
	}
	for i := 0; i < 99; i++ {
		candidate := slug
		if i > 0 {
			candidate = slug + "-" + itoa(i+1)
		}
		key := in.WorkspaceID + "||" + candidate
		if _, taken := f.bySlug[key]; !taken {
			slug = candidate
			break
		}
		if i == 98 {
			return domain.Project{}, errors.New("slug allocation failed")
		}
	}

	f.idCount++
	id := "00000000-0000-0000-0000-" + leftpad(itoa(f.idCount), 12)
	policy := domain.DefaultRecoveryPolicy()
	if in.Policy != nil {
		policy = *in.Policy
	}
	p := domain.Project{
		ID:          id,
		OrgID:       princ.OrgID,
		WorkspaceID: in.WorkspaceID,
		Name:        in.Name,
		Slug:        slug,
		Description: in.Description,
		Environment: in.Environment,
		OwnerUserID: princ.UserID,
		Selectors:   in.Selectors,
		Policy:      policy,
		SLO:         in.SLO,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	f.byID[id] = p
	f.bySlug[in.WorkspaceID+"||"+slug] = id
	return p, nil
}

func (f *fakeProjectsService) Get(_ context.Context, princ domain.Principal, projectID string) (domain.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[projectID]
	if !ok || p.OrgID != princ.OrgID {
		return domain.Project{}, domain.ErrNotFound
	}
	return p, nil
}

func (f *fakeProjectsService) List(_ context.Context, princ domain.Principal, workspaceID string) ([]domain.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.Project{}
	for _, p := range f.byID {
		if p.OrgID == princ.OrgID && p.WorkspaceID == workspaceID && p.ArchivedAt == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeProjectsService) Update(_ context.Context, princ domain.Principal, projectID string, in usecase.UpdateProjectInput) (domain.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[projectID]
	if !ok || p.OrgID != princ.OrgID {
		return domain.Project{}, domain.ErrNotFound
	}
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	if in.Environment != nil {
		p.Environment = *in.Environment
	}
	if in.OwnerUserID != nil {
		p.OwnerUserID = *in.OwnerUserID
	}
	if in.Selectors != nil {
		p.Selectors = *in.Selectors
	}
	if in.SLO != nil {
		p.SLO = *in.SLO
	}
	p.UpdatedAt = time.Now().UTC()
	f.byID[projectID] = p
	return p, nil
}

func (f *fakeProjectsService) Archive(_ context.Context, princ domain.Principal, projectID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[projectID]
	if !ok || p.OrgID != princ.OrgID {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	p.ArchivedAt = &now
	f.byID[projectID] = p
	return nil
}

func (f *fakeProjectsService) UpdatePolicy(_ context.Context, princ domain.Principal, projectID string, policy domain.RecoveryPolicy) (domain.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[projectID]
	if !ok || p.OrgID != princ.OrgID {
		return domain.Project{}, domain.ErrNotFound
	}
	p.Policy = policy
	p.UpdatedAt = time.Now().UTC()
	f.byID[projectID] = p
	return p, nil
}

// fakeSlugify mirrors usecase.slugify (we can't import the unexported one).
func fakeSlugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, c := range s {
		switch {
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			b.WriteRune(c)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// itoa avoids strconv import in the fake.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func leftpad(s string, width int) string {
	for len(s) < width {
		s = "0" + s
	}
	return s
}
