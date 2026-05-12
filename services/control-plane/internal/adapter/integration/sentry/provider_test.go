package sentry

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/keyvault"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type repoEntry struct {
	c      domain.Connection
	secret []byte
}

// fakeRepo is an in-memory stand-in for repo.IntegrationsRepo.
type fakeRepo struct {
	connections map[string]repoEntry
}

func (r *fakeRepo) Upsert(_ context.Context, orgID string, c domain.Connection, secret []byte) error {
	if r.connections == nil {
		r.connections = map[string]repoEntry{}
	}
	r.connections[orgID] = repoEntry{c: c, secret: secret}
	return nil
}

func (r *fakeRepo) Get(_ context.Context, orgID string, _ domain.IntegrationProvider) (domain.Connection, []byte, error) {
	e, ok := r.connections[orgID]
	if !ok {
		return domain.Connection{}, nil, domain.ErrNotFound
	}
	return e.c, e.secret, nil
}

func (r *fakeRepo) Delete(_ context.Context, orgID string, _ domain.IntegrationProvider) error {
	delete(r.connections, orgID)
	return nil
}

// fakeSink captures Insert calls so tests can assert on them.
type fakeSink struct {
	inserts []domain.RawIncident
}

func (s *fakeSink) Insert(_ context.Context, _ string, r domain.RawIncident) error {
	s.inserts = append(s.inserts, r)
	return nil
}

func newTest(t *testing.T) (*Provider, *fakeRepo, *fakeSink) {
	t.Helper()
	repo := &fakeRepo{connections: map[string]repoEntry{}}
	sink := &fakeSink{}
	kv, err := keyvault.NewLocal(make([]byte, 32))
	if err != nil {
		t.Fatalf("keyvault: %v", err)
	}
	p := New(repo, kv, sink)
	return p, repo, sink
}

func TestSentry_HandleWebhook_InsertsIncident(t *testing.T) {
	p, repo, sink := newTest(t)
	secret := []byte("dev-sentry-secret-32")
	// Pre-seed connection so verify path finds the secret.
	enc, err := p.kv.Encrypt(context.Background(), secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	repo.connections["org-1"] = repoEntry{
		c:      domain.Connection{Provider: domain.IntegrationSentry, Status: domain.StatusConnected},
		secret: enc,
	}

	body := []byte(`{"id":"e1","level":"error","title":"T","environment":"prod","tags":[["service","api"]]}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	if err := p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"Sentry-Hook-Signature": sig}, body); err != nil {
		t.Fatalf("webhook: %v", err)
	}
	if len(sink.inserts) != 1 {
		t.Fatalf("want 1 insert, got %d", len(sink.inserts))
	}
	if sink.inserts[0].SourceEventID != "e1" {
		t.Errorf("event id: %s", sink.inserts[0].SourceEventID)
	}
	if sink.inserts[0].Service != "api" {
		t.Errorf("service tag: %s", sink.inserts[0].Service)
	}
}

func TestSentry_HandleWebhook_RejectsBadHMAC(t *testing.T) {
	p, repo, _ := newTest(t)
	enc, err := p.kv.Encrypt(context.Background(), []byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	repo.connections["org-1"] = repoEntry{secret: enc}
	err = p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"Sentry-Hook-Signature": "bad"}, []byte(`{}`))
	if err == nil {
		t.Fatal("want HMAC error")
	}
}
