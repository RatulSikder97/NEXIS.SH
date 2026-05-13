// Package sentry — provider-level tests.
//
// The provider wires together the repo, KeyVault, REST client, and incident
// sink. These tests cover the full Connect → Status → HandleWebhook →
// BackfillRecent loop using fakes for the repo + sink and an httptest.Server
// for the REST surface.
package sentry

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/keyvault"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type repoEntry struct {
	c      domain.Connection
	secret []byte
}

// fakeRepo is an in-memory stand-in for repo.IntegrationsRepo.
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

// fakeSink captures Insert calls so tests can assert on them. Mutex-guarded
// so concurrent backfill goroutines do not race the slice.
type fakeSink struct {
	mu      sync.Mutex
	inserts []domain.RawIncident
	err     error // returned from Insert; nil = success
}

func (s *fakeSink) Insert(_ context.Context, _ string, r domain.RawIncident) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.inserts = append(s.inserts, r)
	return nil
}

func (s *fakeSink) snapshot() []domain.RawIncident {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.RawIncident, len(s.inserts))
	copy(out, s.inserts)
	return out
}

// newTest builds a Provider with an in-memory repo + sink + a LocalKeyVault.
// No REST client wired — tests that need one call provider.SetClient.
func newTest(t *testing.T) (*Provider, *fakeRepo, *fakeSink, *keyvault.LocalKeyVault) {
	t.Helper()
	repo := &fakeRepo{connections: map[string]repoEntry{}}
	sink := &fakeSink{}
	kv, err := keyvault.NewLocal(make([]byte, 32))
	if err != nil {
		t.Fatalf("keyvault: %v", err)
	}
	p := New(repo, kv, sink)
	return p, repo, sink, kv
}

// wireFakeSentry stands up an httptest.Server with the supplied handler and
// attaches the corresponding REST client to the provider. Returns the
// server so tests can read URL / close it.
func wireFakeSentry(t *testing.T, p *Provider, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	hx := httpx.New(srv.Client(), httpx.Config{
		RatePerSec:       1000,
		Burst:            1000,
		MaxAttempts:      2,
		BreakerThreshold: 100,
	})
	p.SetClient(NewClient(srv.URL, hx), Config{BaseURL: srv.URL})
	return srv
}

// seedConnection writes a legacy raw-secret connection so tests for the
// HMAC verification path can stay close to Phase 3's original shape
// (TestSentry_HandleWebhook_InsertsIncident, the legacy compat suite).
func seedConnection(t *testing.T, p *Provider, repo *fakeRepo, orgID string, secret []byte) {
	t.Helper()
	enc, err := p.kv.Encrypt(context.Background(), secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	repo.connections[orgID] = repoEntry{
		c: domain.Connection{
			Provider: domain.IntegrationSentry,
			Status:   domain.StatusConnected,
		},
		secret: enc,
	}
}

// ----- legacy compatibility tests (pre-rewrite Phase 3 behaviour) -----

func TestSentry_HandleWebhook_InsertsIncident(t *testing.T) {
	p, repo, sink, _ := newTest(t)
	secret := []byte("dev-sentry-secret-32")
	seedConnection(t, p, repo, "org-1", secret)

	body := []byte(`{"id":"e1","level":"error","title":"T","environment":"prod","tags":[["service","api"]]}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	if err := p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"Sentry-Hook-Signature": sig}, body); err != nil {
		t.Fatalf("webhook: %v", err)
	}
	got := sink.snapshot()
	if len(got) != 1 {
		t.Fatalf("want 1 insert, got %d", len(got))
	}
	if got[0].SourceEventID != "e1" {
		t.Errorf("event id: %s", got[0].SourceEventID)
	}
	if got[0].Service != "api" {
		t.Errorf("service tag: %s", got[0].Service)
	}
}

func TestSentry_HandleWebhook_RejectsBadHMAC(t *testing.T) {
	p, repo, _, _ := newTest(t)
	seedConnection(t, p, repo, "org-1", []byte("secret"))

	err := p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"Sentry-Hook-Signature": "bad"}, []byte(`{}`))
	if err == nil {
		t.Fatal("want HMAC error")
	}
}

// ----- new behaviour -----

func TestConnect_ValidatesTokenAndProject(t *testing.T) {
	p, _, _, _ := newTest(t)

	var calls []string
	wireFakeSentry(t, p, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if !strings.Contains(r.URL.Path, "/api/0/organizations/acme/projects/") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]ProjectRef{{Slug: "api"}, {Slug: "web"}})
	})

	c, err := p.Connect(context.Background(),
		domain.Principal{OrgID: "org-1"},
		map[string]any{
			"auth_token":        "tok-1",
			"organization_slug": "acme",
			"project_slug":      "api",
			"client_secret":     "hmac-secret",
		},
	)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if c.Status != domain.StatusConnected {
		t.Errorf("status = %s", c.Status)
	}
	if c.Metadata["organization_slug"] != "acme" {
		t.Errorf("metadata = %v", c.Metadata)
	}
	if c.Metadata["project_slug"] != "api" {
		t.Errorf("metadata = %v", c.Metadata)
	}
	// ValidateToken called once; AssertProjectExists shares the same call.
	if len(calls) == 0 {
		t.Fatal("no calls made to Sentry")
	}
}

func TestConnect_RejectsBadToken(t *testing.T) {
	p, _, _, _ := newTest(t)
	wireFakeSentry(t, p, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := p.Connect(context.Background(),
		domain.Principal{OrgID: "org-1"},
		map[string]any{
			"auth_token":        "bad",
			"organization_slug": "acme",
			"project_slug":      "api",
		},
	)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "validate token") {
		t.Errorf("error = %v", err)
	}
}

func TestConnect_RejectsUnknownProject(t *testing.T) {
	p, _, _, _ := newTest(t)
	wireFakeSentry(t, p, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]ProjectRef{{Slug: "web"}})
	})

	_, err := p.Connect(context.Background(),
		domain.Principal{OrgID: "org-1"},
		map[string]any{
			"auth_token":        "tok",
			"organization_slug": "acme",
			"project_slug":      "billing",
		},
	)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "billing") {
		t.Errorf("error = %v", err)
	}
}

func TestConnect_RequiresFields(t *testing.T) {
	p, _, _, _ := newTest(t)
	// no client wired — first guard hit should be the missing-field check.
	_, err := p.Connect(context.Background(), domain.Principal{OrgID: "o"}, map[string]any{})
	if err == nil {
		t.Fatal("want error on missing auth_token")
	}
}

func TestHandleWebhook_IssueEvent_PublishesIncident(t *testing.T) {
	p, repo, sink, _ := newTest(t)

	// Seed a structured (post-rewrite) connection — Connect would normally
	// build this, but we want to test HandleWebhook in isolation.
	blob := secretBlob{
		AuthToken: "tok", OrgSlug: "acme", ProjectSlug: "api", ClientSecret: "hmac-secret",
	}
	raw, _ := json.Marshal(blob)
	enc, err := p.kv.Encrypt(context.Background(), raw)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	repo.connections["org-1"] = repoEntry{
		c:      domain.Connection{Provider: domain.IntegrationSentry, Status: domain.StatusConnected},
		secret: enc,
	}

	body := []byte(`{
		"action":"created",
		"data":{
			"issue":{
				"id":"sentry-issue-42",
				"title":"NPE in checkout",
				"culprit":"checkout/handler.go",
				"level":"error",
				"environment":"prod",
				"project":{"slug":"api"},
				"metadata":{"value":"nil pointer"}
			}
		}
	}`)
	mac := hmac.New(sha256.New, []byte("hmac-secret"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	err = p.HandleWebhook(context.Background(), "org-1", map[string]string{
		"Sentry-Hook-Signature": sig,
		"Sentry-Hook-Resource":  "issue",
	}, body)
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	inserts := sink.snapshot()
	if len(inserts) != 1 {
		t.Fatalf("want 1 insert, got %d", len(inserts))
	}
	got := inserts[0]
	if got.Source != "sentry" {
		t.Errorf("source = %q", got.Source)
	}
	if got.SourceEventID != "sentry-issue-42" {
		t.Errorf("fingerprint = %q", got.SourceEventID)
	}
	if got.Title != "NPE in checkout" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Level != "error" {
		t.Errorf("level = %q", got.Level)
	}
	if got.Service != "api" {
		t.Errorf("service = %q", got.Service)
	}
	if got.Environment != "prod" {
		t.Errorf("env = %q", got.Environment)
	}
	// Fingerprint should now be in the dedupe set so a follow-up backfill
	// will skip it.
	if !p.dedupe.Contains("sentry-issue-42") {
		t.Errorf("dedupe did not record fingerprint")
	}
}

func TestHandleWebhook_UnknownResource_FallsBackToLegacyDecode(t *testing.T) {
	p, repo, sink, _ := newTest(t)
	blob, _ := json.Marshal(secretBlob{ClientSecret: "secret"})
	enc, err := p.kv.Encrypt(context.Background(), blob)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	repo.connections["org-1"] = repoEntry{
		c:      domain.Connection{Provider: domain.IntegrationSentry, Status: domain.StatusConnected},
		secret: enc,
	}

	body := []byte(`{"id":"e9","level":"warning","title":"T","environment":"stg","tags":[["service","worker"]]}`)
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	err = p.HandleWebhook(context.Background(), "org-1", map[string]string{
		"Sentry-Hook-Signature": sig,
		"Sentry-Hook-Resource":  "event_alert", // unhandled — falls back to legacy
	}, body)
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	inserts := sink.snapshot()
	if len(inserts) != 1 {
		t.Fatalf("want 1 insert, got %d", len(inserts))
	}
	if inserts[0].SourceEventID != "e9" {
		t.Errorf("fingerprint = %q", inserts[0].SourceEventID)
	}
}

func TestStatus_EnrichesWithEventCount(t *testing.T) {
	p, repo, _, _ := newTest(t)

	blob, _ := json.Marshal(secretBlob{
		AuthToken: "tok", OrgSlug: "acme", ProjectSlug: "api", ClientSecret: "hmac",
	})
	enc, err := p.kv.Encrypt(context.Background(), blob)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	repo.connections["org-1"] = repoEntry{
		c: domain.Connection{
			Provider: domain.IntegrationSentry,
			Status:   domain.StatusConnected,
			Metadata: map[string]any{},
		},
		secret: enc,
	}

	wireFakeSentry(t, p, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/stats/") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[[1,7],[2,3]]`))
	})

	c, err := p.Status(context.Background(), domain.Principal{OrgID: "org-1"})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if c.Metadata["events_1h"] != 10 {
		t.Errorf("events_1h = %v", c.Metadata["events_1h"])
	}
	if _, ok := c.Metadata["events_checked_at"].(string); !ok {
		t.Errorf("missing events_checked_at: %v", c.Metadata)
	}
}

func TestStatus_StatsErrorFoldsIntoMetadata(t *testing.T) {
	p, repo, _, _ := newTest(t)
	blob, _ := json.Marshal(secretBlob{
		AuthToken: "tok", OrgSlug: "acme", ProjectSlug: "api", ClientSecret: "hmac",
	})
	enc, _ := p.kv.Encrypt(context.Background(), blob)
	repo.connections["org-1"] = repoEntry{
		c:      domain.Connection{Provider: domain.IntegrationSentry, Status: domain.StatusConnected},
		secret: enc,
	}

	wireFakeSentry(t, p, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	c, err := p.Status(context.Background(), domain.Principal{OrgID: "org-1"})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if _, ok := c.Metadata["stats_error"]; !ok {
		t.Errorf("expected stats_error in metadata, got %v", c.Metadata)
	}
	if _, ok := c.Metadata["events_1h"]; ok {
		t.Errorf("events_1h should be absent on error, got %v", c.Metadata)
	}
}

func TestBackfill_DedupesByFingerprint(t *testing.T) {
	p, repo, sink, _ := newTest(t)

	blob, _ := json.Marshal(secretBlob{
		AuthToken: "tok", OrgSlug: "acme", ProjectSlug: "api", ClientSecret: "hmac",
	})
	enc, _ := p.kv.Encrypt(context.Background(), blob)
	repo.connections["org-1"] = repoEntry{
		c:      domain.Connection{Provider: domain.IntegrationSentry, Status: domain.StatusConnected},
		secret: enc,
	}

	now := time.Now()
	issuesJSON := fmt.Sprintf(`[
		{"id":"i-1","title":"E1","level":"error","lastSeen":%q},
		{"id":"i-2","title":"E2","level":"error","lastSeen":%q}
	]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	wireFakeSentry(t, p, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(issuesJSON))
	})

	// First run — both fingerprints land in the sink.
	got1, err := p.BackfillRecent(context.Background(), domain.Principal{OrgID: "org-1"}, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("BackfillRecent #1: %v", err)
	}
	if got1 != 2 {
		t.Fatalf("first run emitted %d, want 2", got1)
	}
	// Second run — same fingerprints, sink should remain untouched.
	got2, err := p.BackfillRecent(context.Background(), domain.Principal{OrgID: "org-1"}, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("BackfillRecent #2: %v", err)
	}
	if got2 != 0 {
		t.Errorf("second run emitted %d, want 0", got2)
	}
	if total := len(sink.snapshot()); total != 2 {
		t.Errorf("sink length = %d, want 2", total)
	}
}

func TestBackfill_RollsBackOnSinkError(t *testing.T) {
	p, repo, sink, _ := newTest(t)
	blob, _ := json.Marshal(secretBlob{
		AuthToken: "tok", OrgSlug: "acme", ProjectSlug: "api", ClientSecret: "hmac",
	})
	enc, _ := p.kv.Encrypt(context.Background(), blob)
	repo.connections["org-1"] = repoEntry{
		c:      domain.Connection{Provider: domain.IntegrationSentry, Status: domain.StatusConnected},
		secret: enc,
	}

	now := time.Now()
	wireFakeSentry(t, p, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `[{"id":"i-1","title":"E","level":"error","lastSeen":%q}]`,
			now.Format(time.RFC3339Nano))
	})

	sink.err = fmt.Errorf("sink down")
	if _, err := p.BackfillRecent(context.Background(), domain.Principal{OrgID: "org-1"}, now.Add(-time.Minute)); err == nil {
		t.Fatal("want sink error")
	}
	// Failed fingerprint must NOT live in the LRU.
	if p.dedupe.Contains("i-1") {
		t.Errorf("dedupe retained fingerprint after sink failure")
	}
	// Recover and retry — should succeed now.
	sink.err = nil
	n, err := p.BackfillRecent(context.Background(), domain.Principal{OrgID: "org-1"}, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if n != 1 {
		t.Errorf("retry emitted %d, want 1", n)
	}
}

func TestBackfill_NoConnection(t *testing.T) {
	p, _, _, _ := newTest(t)
	wireFakeSentry(t, p, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})

	_, err := p.BackfillRecent(context.Background(), domain.Principal{OrgID: "org-missing"}, time.Now().Add(-time.Minute))
	if err == nil {
		t.Fatal("want error for unknown org")
	}
}

func TestStartBackfillCron_TicksAndStops(t *testing.T) {
	p, repo, sink, _ := newTest(t)
	blob, _ := json.Marshal(secretBlob{
		AuthToken: "tok", OrgSlug: "acme", ProjectSlug: "api", ClientSecret: "hmac",
	})
	enc, _ := p.kv.Encrypt(context.Background(), blob)
	repo.connections["org-1"] = repoEntry{
		c:      domain.Connection{Provider: domain.IntegrationSentry, Status: domain.StatusConnected},
		secret: enc,
	}

	now := time.Now()
	wireFakeSentry(t, p, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `[{"id":"cron-1","title":"C","level":"error","lastSeen":%q}]`,
			now.Format(time.RFC3339Nano))
	})

	calls := 0
	listOrgs := func() ([]string, error) {
		calls++
		return []string{"org-1"}, nil
	}

	// Drive the cron at 10ms cadence so the test runs in < 100ms.
	stop := p.startBackfillCronWith(context.Background(), listOrgs, 10*time.Millisecond, time.Hour)
	defer stop()

	// Poll until we see at least one emit OR a 1s safety budget runs out.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(sink.snapshot()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	stop()

	if len(sink.snapshot()) == 0 {
		t.Fatalf("cron did not emit (calls=%d)", calls)
	}
}

// ----- LRU unit -----

func TestFingerprintLRU_FIFO(t *testing.T) {
	l := newFingerprintLRU(3)
	for _, fp := range []string{"a", "b", "c"} {
		if !l.AddIfAbsent(fp) {
			t.Fatalf("first add of %q reported duplicate", fp)
		}
	}
	if l.AddIfAbsent("a") {
		t.Fatal("second add of a should be a no-op")
	}
	if !l.AddIfAbsent("d") {
		t.Fatal("d should be new")
	}
	// "a" was the oldest before we added d → should be evicted.
	if l.Contains("a") {
		t.Error("a should have been evicted")
	}
	if !l.Contains("b") || !l.Contains("c") || !l.Contains("d") {
		t.Error("b/c/d should remain")
	}
}

func TestFingerprintLRU_Drop(t *testing.T) {
	l := newFingerprintLRU(3)
	l.AddIfAbsent("a")
	l.Drop("a")
	if l.Contains("a") {
		t.Error("a should be dropped")
	}
	// Adding back should report as new again.
	if !l.AddIfAbsent("a") {
		t.Error("a should be re-addable after Drop")
	}
}
