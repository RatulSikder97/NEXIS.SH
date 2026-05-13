package argocd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/keyvault"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// repoEntry mirrors what the production IntegrationsRepo stores per row.
type repoEntry struct {
	c      domain.Connection
	secret []byte
}

// fakeRepo is an in-memory IntegrationsRepo for Provider tests.
type fakeRepo struct {
	mu          sync.Mutex
	connections map[string]repoEntry
}

func (r *fakeRepo) Upsert(_ context.Context, orgID string, c domain.Connection, secret []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.connections == nil {
		r.connections = map[string]repoEntry{}
	}
	r.connections[orgID] = repoEntry{c: c, secret: secret}
	return nil
}

func (r *fakeRepo) Get(_ context.Context, orgID string, _ domain.IntegrationProvider) (domain.Connection, []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.connections[orgID]
	if !ok {
		return domain.Connection{}, nil, domain.ErrNotFound
	}
	return e.c, e.secret, nil
}

func (r *fakeRepo) Delete(_ context.Context, orgID string, _ domain.IntegrationProvider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.connections, orgID)
	return nil
}

// fakeRoutes lets a test answer specific ArgoCD endpoints by path suffix. The
// handler resolves the longest matching suffix so /sync wins over /. This
// keeps each test focused on the call it cares about without re-implementing
// the whole REST surface inline.
type fakeRoutes struct {
	mu     sync.Mutex
	routes map[string]http.HandlerFunc
	calls  map[string]int
}

func newFakeRoutes() *fakeRoutes {
	return &fakeRoutes{
		routes: map[string]http.HandlerFunc{},
		calls:  map[string]int{},
	}
}

func (f *fakeRoutes) On(suffix string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[suffix] = h
}

func (f *fakeRoutes) Calls(suffix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[suffix]
}

func (f *fakeRoutes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	var matched string
	var handler http.HandlerFunc
	for suffix, h := range f.routes {
		if strings.HasSuffix(r.URL.Path, suffix) && len(suffix) > len(matched) {
			matched = suffix
			handler = h
		}
	}
	if matched != "" {
		f.calls[matched]++
	}
	f.mu.Unlock()
	if handler == nil {
		http.NotFound(w, r)
		return
	}
	handler(w, r)
}

// newProviderTest wires a Provider whose ClientBuilder points every call at
// the supplied httptest server. Returns the Provider, the fake routes (for
// per-test handler injection + call counts), and the fake repo.
func newProviderTest(t *testing.T) (*Provider, *fakeRoutes, *fakeRepo, *httptest.Server) {
	t.Helper()
	routes := newFakeRoutes()
	srv := httptest.NewServer(routes)
	t.Cleanup(srv.Close)

	repo := &fakeRepo{}
	kv, err := keyvault.NewLocal(make([]byte, 32))
	if err != nil {
		t.Fatalf("keyvault: %v", err)
	}

	hx := httpx.New(srv.Client(), httpx.Config{
		RatePerSec:  1000,
		Burst:       1000,
		MaxAttempts: 1,
	})

	p := New(repo, kv)
	// Override the builder so every per-call client points at our test
	// server, regardless of what server_url the caller passed in.
	p.SetClientBuilder(func(_, token string) *Client {
		return NewClient(srv.URL, token, hx)
	})
	return p, routes, repo, srv
}

// princ returns a Principal with a stable OrgID for the tests.
func princ() domain.Principal {
	return domain.Principal{
		UserID: "u-1",
		OrgID:  "org-1",
		Role:   domain.RoleAdmin,
	}
}

func appPayload(name, project, syncStatus, health string) string {
	return `{
		"metadata": {"name": "` + name + `"},
		"spec": {
			"project": "` + project + `",
			"source": {"targetRevision": "main"},
			"destination": {"server": "https://kubernetes.default.svc", "namespace": "prod"}
		},
		"status": {
			"sync":   {"status": "` + syncStatus + `", "revision": "abc"},
			"health": {"status": "` + health + `"}
		}
	}`
}

func TestProvider_Connect_RequiresAllFields(t *testing.T) {
	p, _, _, _ := newProviderTest(t)
	_, err := p.Connect(context.Background(), princ(), map[string]any{
		"server_url": "https://argocd.example.com",
		"auth_token": "tok",
		// missing project + app_name
	})
	if err == nil {
		t.Fatal("want error on missing fields")
	}
}

func TestConnect_ValidatesAppExists(t *testing.T) {
	p, routes, repo, _ := newProviderTest(t)
	routes.On("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Version":"v2.10.1"}`))
	})
	// GetApplication 404 → Connect must fail and must NOT persist a row.
	routes.On("/applications/nope", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"app missing"}`))
	})

	_, err := p.Connect(context.Background(), princ(), map[string]any{
		"server_url": "https://argocd.example.com",
		"auth_token": "tok",
		"project":    "demo",
		"app_name":   "nope",
	})
	if err == nil {
		t.Fatal("want error when app does not exist")
	}
	if !strings.Contains(err.Error(), "app lookup") {
		t.Errorf("err = %v, want substring 'app lookup'", err)
	}
	if _, ok := repo.connections["org-1"]; ok {
		t.Fatal("repo wrote a row despite failed validation")
	}
}

func TestConnect_PersistsEnvelopeAndMetadata(t *testing.T) {
	p, routes, repo, _ := newProviderTest(t)
	routes.On("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Version":"v2.10.1"}`))
	})
	routes.On("/applications/nexis-demo", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(appPayload("nexis-demo", "demo", "Synced", "Healthy")))
	})

	conn, err := p.Connect(context.Background(), princ(), map[string]any{
		"server_url": "https://argocd.example.com",
		"auth_token": "tok",
		"project":    "demo",
		"app_name":   "nexis-demo",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if conn.Status != domain.StatusConnected {
		t.Errorf("status = %s, want connected", conn.Status)
	}
	for _, k := range []string{"server_url", "project", "app_name", "version"} {
		if _, ok := conn.Metadata[k]; !ok {
			t.Errorf("metadata missing key %q", k)
		}
	}
	if conn.Metadata["version"] != "v2.10.1" {
		t.Errorf("version = %v, want v2.10.1", conn.Metadata["version"])
	}

	row, ok := repo.connections["org-1"]
	if !ok {
		t.Fatal("repo did not persist")
	}
	if len(row.secret) == 0 {
		t.Fatal("repo persisted empty secret")
	}
}

// connectFixture is a helper used by Status/Sync/Rollback tests to set up a
// pre-connected provider. We reach directly into the repo with a manually
// encrypted envelope so the test does not depend on Connect's success path.
func connectFixture(t *testing.T, p *Provider, repo *fakeRepo) {
	t.Helper()
	env := secretEnvelope{
		ServerURL: "https://argocd.example.com",
		AuthToken: "tok",
		Project:   "demo",
		AppName:   "nexis-demo",
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	enc, err := p.kv.Encrypt(context.Background(), raw)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	repo.connections = map[string]repoEntry{
		"org-1": {
			c: domain.Connection{
				Provider: domain.IntegrationArgoCD,
				Status:   domain.StatusConnected,
				Metadata: map[string]any{"version": "v2.10.1"},
			},
			secret: enc,
		},
	}
}

func TestStatus_ReturnsLiveHealth(t *testing.T) {
	p, routes, repo, _ := newProviderTest(t)
	connectFixture(t, p, repo)
	routes.On("/applications/nexis-demo", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(appPayload("nexis-demo", "demo", "OutOfSync", "Degraded")))
	})

	conn, err := p.Status(context.Background(), princ())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if conn.Status != domain.StatusConnected {
		t.Errorf("status = %s", conn.Status)
	}
	if conn.Metadata["sync_status"] != "OutOfSync" {
		t.Errorf("sync_status = %v", conn.Metadata["sync_status"])
	}
	if conn.Metadata["health_status"] != "Degraded" {
		t.Errorf("health_status = %v", conn.Metadata["health_status"])
	}
}

func TestStatus_PropagatesAPIError(t *testing.T) {
	p, routes, repo, _ := newProviderTest(t)
	connectFixture(t, p, repo)
	routes.On("/applications/nexis-demo", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"token revoked"}`))
	})

	conn, err := p.Status(context.Background(), princ())
	if err != nil {
		t.Fatalf("Status: %v (should not surface error — should encode in connection)", err)
	}
	if conn.Status != domain.StatusError {
		t.Errorf("status = %s, want error", conn.Status)
	}
	if !strings.Contains(conn.LastError, "token revoked") {
		t.Errorf("LastError = %q", conn.LastError)
	}
}

func TestSync_ReturnsOperation(t *testing.T) {
	p, routes, repo, _ := newProviderTest(t)
	connectFixture(t, p, repo)

	var seenRevision string
	routes.On("/sync", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if rev, _ := body["revision"].(string); rev != "" {
			seenRevision = rev
		}
		_, _ = w.Write([]byte(`{
			"status": {
				"sync": {"status": "OutOfSync"},
				"health": {"status": "Progressing"},
				"operationState": {
					"phase": "Running",
					"message": "syncing",
					"startedAt": "2026-05-13T10:00:00Z"
				}
			}
		}`))
	})

	op, err := p.Sync(context.Background(), princ(), "deadbeef")
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if op.Phase != "Running" {
		t.Errorf("phase = %q", op.Phase)
	}
	if seenRevision != "deadbeef" {
		t.Errorf("revision sent = %q, want deadbeef", seenRevision)
	}
	if routes.Calls("/sync") != 1 {
		t.Errorf("sync calls = %d, want 1", routes.Calls("/sync"))
	}
}

func TestRollback_ChoosesPriorRevision(t *testing.T) {
	p, routes, repo, _ := newProviderTest(t)
	connectFixture(t, p, repo)

	// /rollback must be matched BEFORE /applications/nexis-demo because the
	// matcher uses suffix length; both endpoints share the prefix. We register
	// the GET-history handler under the longer "/applications/nexis-demo"
	// suffix and the rollback POST under the more specific "/rollback".
	routes.On("/applications/nexis-demo", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"metadata": {"name": "nexis-demo"},
			"spec": {"project": "demo"},
			"status": {
				"sync": {"status": "Synced"},
				"health": {"status": "Healthy"},
				"history": [
					{"id": 10, "revision": "older",  "deployedAt": "2026-05-13T09:00:00Z"},
					{"id": 11, "revision": "prior",  "deployedAt": "2026-05-13T09:30:00Z"},
					{"id": 12, "revision": "current","deployedAt": "2026-05-13T10:00:00Z"}
				]
			}
		}`))
	})

	var seenID int64
	routes.On("/rollback", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if v, ok := body["id"].(float64); ok {
			seenID = int64(v)
		}
		_, _ = w.Write([]byte(`{
			"phase": "Running",
			"message": "rolling back",
			"startedAt": "2026-05-13T10:05:00Z"
		}`))
	})

	op, err := p.Rollback(context.Background(), princ())
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if op.Phase != "Running" {
		t.Errorf("phase = %q", op.Phase)
	}
	if seenID != 11 {
		t.Errorf("rollback id = %d, want 11 (second-to-last)", seenID)
	}
}

func TestRollback_FailsWithoutHistory(t *testing.T) {
	p, routes, repo, _ := newProviderTest(t)
	connectFixture(t, p, repo)
	// Only one history entry → nothing to roll back to.
	routes.On("/applications/nexis-demo", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"metadata": {"name": "nexis-demo"},
			"spec": {"project": "demo"},
			"status": {
				"sync": {"status": "Synced"},
				"health": {"status": "Healthy"},
				"history": [
					{"id": 1, "revision": "only", "deployedAt": "2026-05-13T10:00:00Z"}
				]
			}
		}`))
	})

	_, err := p.Rollback(context.Background(), princ())
	if err == nil {
		t.Fatal("want error when history has < 2 entries")
	}
	if !strings.Contains(err.Error(), "fewer than 2") {
		t.Errorf("err = %v", err)
	}
}

func TestDisconnect_RemovesRow(t *testing.T) {
	p, _, repo, _ := newProviderTest(t)
	connectFixture(t, p, repo)
	if err := p.Disconnect(context.Background(), princ()); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if _, ok := repo.connections["org-1"]; ok {
		t.Fatal("row not deleted")
	}
}

func TestHandleWebhook_IsNoOp(t *testing.T) {
	p, _, _, _ := newProviderTest(t)
	err := p.HandleWebhook(context.Background(), "org-1", map[string]string{}, []byte(`{}`))
	if err != nil {
		t.Fatalf("HandleWebhook should be a no-op, got %v", err)
	}
}
