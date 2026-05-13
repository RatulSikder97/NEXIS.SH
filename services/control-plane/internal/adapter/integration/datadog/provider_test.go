package datadog

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/keyvault"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeRepo is the same minimal in-memory IntegrationsRepo used by the
// sibling adapter tests. Keyed by orgID (the adapter only ever stores one
// row per org per provider).
type fakeRepo struct {
	rows map[string]*entry
}

type entry struct {
	c      domain.Connection
	secret []byte
}

func (r *fakeRepo) Upsert(_ context.Context, orgID string, c domain.Connection, secret []byte) error {
	if r.rows == nil {
		r.rows = map[string]*entry{}
	}
	r.rows[orgID] = &entry{c: c, secret: secret}
	return nil
}

func (r *fakeRepo) Get(_ context.Context, orgID string, _ domain.IntegrationProvider) (domain.Connection, []byte, error) {
	e, ok := r.rows[orgID]
	if !ok {
		return domain.Connection{}, nil, domain.ErrNotFound
	}
	return e.c, e.secret, nil
}

func (r *fakeRepo) Delete(_ context.Context, orgID string, _ domain.IntegrationProvider) error {
	delete(r.rows, orgID)
	return nil
}

// fakeSink captures Insert calls so tests can assert on them.
type fakeSink struct {
	inserts []domain.RawIncident
	orgs    []string
}

func (s *fakeSink) Insert(_ context.Context, orgID string, r domain.RawIncident) error {
	s.inserts = append(s.inserts, r)
	s.orgs = append(s.orgs, orgID)
	return nil
}

func newKV(t *testing.T) domain.KeyVault {
	t.Helper()
	kv, err := keyvault.NewLocal(make([]byte, 32))
	if err != nil {
		t.Fatalf("keyvault: %v", err)
	}
	return kv
}

func TestConnect_ValidatesCreds(t *testing.T) {
	var calls int
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":true}`))
	}))
	t.Cleanup(srv.Close)

	repo := &fakeRepo{}
	sink := &fakeSink{}
	p := New(repo, newKV(t), sink)
	p.SetClient(newTestClient(t, srv, SiteUS1))

	c, err := p.Connect(context.Background(), domain.Principal{OrgID: "org-1"}, map[string]any{
		"api_key": "api-k",
		"app_key": "app-k",
		"site":    SiteUS1,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if calls == 0 {
		t.Fatal("validate not called")
	}
	if gotPath != "/api/v1/validate" {
		t.Errorf("validate path = %q, want /api/v1/validate", gotPath)
	}
	if c.Status != domain.StatusConnected {
		t.Errorf("status = %s, want connected", c.Status)
	}
	if c.Metadata["site"] != SiteUS1 {
		t.Errorf("metadata site = %v, want %s", c.Metadata["site"], SiteUS1)
	}
	// Secret persisted + encrypted.
	row, ok := repo.rows["org-1"]
	if !ok {
		t.Fatal("connection not persisted")
	}
	if len(row.secret) == 0 {
		t.Fatal("secret bytes empty")
	}
	// And the encrypted blob must not contain the plaintext keys.
	if containsPlain(row.secret, "api-k") || containsPlain(row.secret, "app-k") {
		t.Fatalf("plaintext keys leaked into stored ciphertext: %q", row.secret)
	}
}

func TestConnect_RejectsInvalidCreds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":["Forbidden"]}`))
	}))
	t.Cleanup(srv.Close)

	repo := &fakeRepo{}
	p := New(repo, newKV(t), &fakeSink{})
	p.SetClient(newTestClient(t, srv, SiteUS1))

	_, err := p.Connect(context.Background(), domain.Principal{OrgID: "org-1"}, map[string]any{
		"api_key": "bad", "app_key": "bad",
	})
	if err == nil {
		t.Fatal("want error for 401")
	}
	if _, persisted := repo.rows["org-1"]; persisted {
		t.Error("connection persisted on auth failure")
	}
}

func TestConnect_RequiresKeys(t *testing.T) {
	p := New(&fakeRepo{}, newKV(t), &fakeSink{})
	// No client set — but we should fail validation before reaching it.
	_, err := p.Connect(context.Background(), domain.Principal{OrgID: "org-1"}, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing keys")
	}
}

func TestHandleWebhook_PublishesIncidentDetected(t *testing.T) {
	repo := &fakeRepo{}
	sink := &fakeSink{}
	p := New(repo, newKV(t), sink)
	// No signing secret on the connection, and no process-wide secret — we
	// should still accept + insert.
	body := []byte(`{
		"alert_id":"alert-42",
		"event_type":"alert.triggered",
		"title":"High error rate",
		"priority":"P1",
		"alert_status":"alerting",
		"tags":"service:checkout,env:prod"
	}`)

	if err := p.HandleWebhook(context.Background(), "org-1", map[string]string{}, body); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if len(sink.inserts) != 1 {
		t.Fatalf("inserts = %d, want 1", len(sink.inserts))
	}
	got := sink.inserts[0]
	if got.Source != "datadog" {
		t.Errorf("source = %s, want datadog", got.Source)
	}
	if got.SourceEventID != "alert-42" {
		t.Errorf("source event id = %s, want alert-42", got.SourceEventID)
	}
	if got.Title != "High error rate" {
		t.Errorf("title = %s", got.Title)
	}
	if got.Level != "P1" {
		t.Errorf("level = %s", got.Level)
	}
	if got.Service != "checkout" {
		t.Errorf("service tag = %s, want checkout", got.Service)
	}
	if got.Environment != "prod" {
		t.Errorf("env tag = %s, want prod", got.Environment)
	}
	if sink.orgs[0] != "org-1" {
		t.Errorf("org = %s, want org-1", sink.orgs[0])
	}
}

func TestHandleWebhook_RejectsInvalidHMAC(t *testing.T) {
	repo := &fakeRepo{}
	sink := &fakeSink{}
	kv := newKV(t)
	p := New(repo, kv, sink)

	// Seed a connection with a webhook_secret set inside the blob.
	const secret = "shared-webhook-secret"
	if err := seedConnection(context.Background(), p, "org-1", SiteUS1, "api-k", "app-k", secret); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"alert_id":"a1","title":"T","priority":"P1"}`)
	// Bad signature — random hex of correct length.
	bad := "v0=" + hex.EncodeToString(make([]byte, 32))
	err := p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"X-Datadog-Signature": bad}, body)
	if err == nil {
		t.Fatal("want HMAC mismatch error")
	}
	if len(sink.inserts) != 0 {
		t.Fatalf("sink received %d inserts on bad sig, want 0", len(sink.inserts))
	}

	// Now a correct signature must pass through.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	good := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if err := p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"X-Datadog-Signature": good}, body); err != nil {
		t.Fatalf("good HMAC: %v", err)
	}
	if len(sink.inserts) != 1 {
		t.Fatalf("inserts after good HMAC = %d, want 1", len(sink.inserts))
	}
}

func TestHandleWebhook_RejectsMissingSignatureWhenSecretSet(t *testing.T) {
	repo := &fakeRepo{}
	sink := &fakeSink{}
	p := New(repo, newKV(t), sink)

	if err := seedConnection(context.Background(), p, "org-1", SiteUS1, "k", "k", "shh"); err != nil {
		t.Fatal(err)
	}

	err := p.HandleWebhook(context.Background(), "org-1", map[string]string{}, []byte(`{}`))
	if err == nil {
		t.Fatal("want error when signature header missing but secret on file")
	}
}

func TestStatus_PopulatesMonitorCounts(t *testing.T) {
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/validate":
			_, _ = w.Write([]byte(`{"valid":true}`))
		case "/api/v1/monitor":
			_, _ = w.Write([]byte(`[{"overall_state":"Alert"},{"overall_state":"OK"},{"overall_state":"OK"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	repo := &fakeRepo{}
	p := New(repo, newKV(t), &fakeSink{})
	p.SetClient(newTestClient(t, srv, SiteUS1))

	_, err := p.Connect(context.Background(), domain.Principal{OrgID: "org-1"}, map[string]any{
		"api_key": "k", "app_key": "k", "site": SiteUS1,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	c, err := p.Status(context.Background(), domain.Principal{OrgID: "org-1"})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if c.Metadata["monitors_alerting"] != 1 {
		t.Errorf("monitors_alerting = %v, want 1", c.Metadata["monitors_alerting"])
	}
	if c.Metadata["monitors_ok"] != 2 {
		t.Errorf("monitors_ok = %v, want 2", c.Metadata["monitors_ok"])
	}
	if c.Metadata["site"] != SiteUS1 {
		t.Errorf("site = %v, want %s", c.Metadata["site"], SiteUS1)
	}
	if calls["/api/v1/monitor"] == 0 {
		t.Error("monitor endpoint not called")
	}
}

func TestHandleWebhook_ProcessWideSigningSecretPreferred(t *testing.T) {
	repo := &fakeRepo{}
	sink := &fakeSink{}
	kv := newKV(t)
	// Per-tenant secret says "alpha" but the env-wired global says "beta".
	// Datadog sends a body signed with "beta" so global must win.
	p := NewWithSigningSecret(repo, kv, sink, []byte("beta"))

	if err := seedConnection(context.Background(), p, "org-1", SiteUS1, "k", "k", "alpha"); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"alert_id":"a1","title":"T","priority":"P1"}`)
	mac := hmac.New(sha256.New, []byte("beta"))
	mac.Write(body)
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	if err := p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"X-Datadog-Signature": sig}, body); err != nil {
		t.Fatalf("HandleWebhook with global secret: %v", err)
	}
	if len(sink.inserts) != 1 {
		t.Fatalf("inserts = %d, want 1", len(sink.inserts))
	}
}

func TestConnect_UnknownSiteRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"valid":true}`))
	}))
	t.Cleanup(srv.Close)

	p := New(&fakeRepo{}, newKV(t), &fakeSink{})
	p.SetClient(newTestClient(t, srv, SiteUS1))

	_, err := p.Connect(context.Background(), domain.Principal{OrgID: "org-1"}, map[string]any{
		"api_key": "k", "app_key": "k", "site": "evil.example.com",
	})
	if err == nil {
		t.Fatal("want error for unknown site")
	}
}

// seedConnection persists a connection row directly through the Provider's
// repo + KV so signing tests can run without invoking the HTTP validate
// path. Mirrors what Connect would do but skips the network.
func seedConnection(ctx context.Context, p *Provider, orgID, site, apiKey, appKey, webhookSecret string) error {
	blob := secretBlob{APIKey: apiKey, AppKey: appKey, Site: site, WebhookSecret: webhookSecret}
	raw, err := json.Marshal(blob)
	if err != nil {
		return err
	}
	enc, err := p.kv.Encrypt(ctx, raw)
	if err != nil {
		return err
	}
	return p.repo.Upsert(ctx, orgID, domain.Connection{
		Provider: domain.IntegrationDatadog,
		Status:   domain.StatusConnected,
		Metadata: map[string]any{"site": site},
	}, enc)
}

// containsPlain returns true when needle appears as a substring of haystack.
// We don't use bytes.Contains directly so we can avoid the byte/string juggle
// in the assertion above.
func containsPlain(haystack []byte, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}

// Sanity: confirm we're using the real httpx.Client through the test
// transport. Failure here usually means a future refactor broke the
// rewrite path.
func TestNewClient_UsesSiteForBaseURL(t *testing.T) {
	c := NewClient(SiteEU1, httpx.New(nil, httpx.Config{}))
	if got := c.baseURL(); got != "https://api.datadoghq.eu" {
		t.Errorf("baseURL = %q, want EU1", got)
	}
}
