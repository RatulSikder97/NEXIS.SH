package github

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/keyvault"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeRepo is an in-memory stand-in for repo.IntegrationsRepo — keeps the unit
// tests free of Postgres.
type fakeRepo struct {
	rows         map[string]*entry
	upsertCalled bool
	deleted      int32
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
	atomic.AddInt32(&r.deleted, 1)
	delete(r.rows, orgID)
	return nil
}

func (r *fakeRepo) deleteCount() int { return int(atomic.LoadInt32(&r.deleted)) }

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

// genTestPEM is the same helper as client_test.go but inlined so each file is
// self-contained. We generate per-test rather than per-package to keep
// goroutine-level isolation clean.
func genTestPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

// withAppMode wires a Provider into App mode and points it at the supplied
// test server so Connect/Status actually exchange tokens.
func withAppMode(t *testing.T, p *Provider, srv *httptest.Server, webhookSecret []byte) {
	t.Helper()
	pemBytes := genTestPEM(t)
	httpc := httpx.New(srv.Client(), httpx.Config{
		RatePerSec:  1000,
		Burst:       1000,
		MaxAttempts: 1,
	})
	client, err := NewClientWithBaseURL(AppCreds{AppID: 1, PrivateKeyPEM: pemBytes}, httpc, srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	p.SetConfig(Config{
		AppID:         1,
		PrivateKeyPEM: pemBytes,
		AppSlug:       "nexis-test",
		WebhookSecret: webhookSecret,
		AppBaseURL:    "http://localhost",
	}, client)
}

func TestGitHub_Connect_ValidatesInstallationToken(t *testing.T) {
	p, repo := newTest(t)
	const installID = 4242
	tokenWant := "ghs_inst_token"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/access_tokens") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      tokenWant,
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	t.Cleanup(srv.Close)

	withAppMode(t, p, srv, []byte("webhook-secret"))

	princ := domain.Principal{OrgID: "org-1", UserID: "user-1", Role: domain.RoleOwner}
	c, err := p.Connect(context.Background(), princ, map[string]any{
		"installation_id": fmt.Sprintf("%d", installID),
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if c.Status != domain.StatusConnected {
		t.Errorf("status = %s", c.Status)
	}
	if !repo.upsertCalled {
		t.Error("repo upsert not called")
	}
	// Verify token sha256 landed in metadata — we never store the raw token
	// but its hash is the audit-correlation hook.
	sum := sha256.Sum256([]byte(tokenWant))
	gotHash, _ := c.Metadata["last_token_sha256"].(string)
	if gotHash != hex.EncodeToString(sum[:]) {
		t.Errorf("metadata.last_token_sha256 = %q, want %q",
			gotHash, hex.EncodeToString(sum[:]))
	}
	if _, ok := c.Metadata["last_token_minted_at"]; !ok {
		t.Error("metadata.last_token_minted_at missing")
	}
}

func TestGitHub_HandleWebhook_VerifiesHMACSignature(t *testing.T) {
	// This test exercises the App-mode webhook path: a real WebhookSecret is
	// configured via SetConfig, and the installation.deleted event triggers a
	// repo.Delete. The bad-sig branch is already covered by
	// TestGitHub_HandleWebhook_RejectsBadSignature against the legacy stub.
	p, repo := newTest(t)
	// Use a non-issuing server — Connect is not exercised here; we only need
	// the SetConfig path so WebhookSecret takes effect.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	const wh = "app-webhook-secret-32byte"
	withAppMode(t, p, srv, []byte(wh))

	body := []byte(`{"action":"deleted","installation":{"id":4242}}`)

	// Wrong signature must reject (no repo Delete should run).
	if err := p.HandleWebhook(context.Background(), "org-1",
		map[string]string{
			"X-Hub-Signature-256": "sha256=deadbeef",
			"X-GitHub-Event":      "installation",
		}, body); err == nil {
		t.Fatal("want HMAC mismatch error")
	}
	if repo.deleteCount() != 0 {
		t.Fatalf("delete count after bad sig = %d, want 0", repo.deleteCount())
	}

	// Correct signature must accept AND dispatch installation.deleted.
	mac := hmac.New(sha256.New, []byte(wh))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if err := p.HandleWebhook(context.Background(), "org-1",
		map[string]string{
			"X-Hub-Signature-256": sig,
			"X-GitHub-Event":      "installation",
		}, body); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if repo.deleteCount() != 1 {
		t.Errorf("delete count after good sig = %d, want 1", repo.deleteCount())
	}
}
