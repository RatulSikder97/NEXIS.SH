package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/keyvault"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeRepo is an in-memory stand-in for repo.IntegrationsRepo — keeps the unit
// tests free of Postgres.
type fakeRepo struct {
	rows         map[string]*entry
	upsertCalled bool
}

type entry struct {
	c      domain.Connection
	secret []byte
}

func (r *fakeRepo) Upsert(_ context.Context, orgID string, c domain.Connection, secret []byte) error {
	r.upsertCalled = true
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

func newTest(t *testing.T) (*Provider, *fakeRepo) {
	t.Helper()
	repo := &fakeRepo{}
	kv, err := keyvault.NewLocal(make([]byte, 32))
	if err != nil {
		t.Fatalf("keyvault: %v", err)
	}
	p := New(repo, kv, []byte("dev-webhook-secret-32"))
	return p, repo
}

func TestGitHub_HandleWebhook_VerifiesSignature(t *testing.T) {
	p, _ := newTest(t)
	secret := []byte("dev-webhook-secret-32")
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	err := p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"X-Hub-Signature-256": sig, "X-Github-Event": "ping"}, body)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestGitHub_HandleWebhook_RejectsBadSignature(t *testing.T) {
	p, _ := newTest(t)
	err := p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"X-Hub-Signature-256": "sha256=bad"}, []byte(`{}`))
	if err == nil {
		t.Fatal("want HMAC mismatch error")
	}
}

func TestGitHub_Connect_MockInstall(t *testing.T) {
	p, repo := newTest(t)
	princ := domain.Principal{OrgID: "org-1", UserID: "user-1", Role: domain.RoleOwner}
	c, err := p.Connect(context.Background(), princ, map[string]any{
		"installation_id": "inst-123",
		"scopes":          []any{"repo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != domain.StatusConnected {
		t.Errorf("status: %s", c.Status)
	}
	if c.InstallationID != "inst-123" {
		t.Errorf("install id: %s", c.InstallationID)
	}
	if !repo.upsertCalled {
		t.Error("repo upsert not called")
	}
}
