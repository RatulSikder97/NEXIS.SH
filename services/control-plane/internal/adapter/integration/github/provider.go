// Package github implements the GitHub IntegrationProvider. Phase 3 covers
// connection management (Connect / Disconnect / Status) and HMAC-verified
// webhook acknowledgement; event-specific routing (PR opened, push, etc.)
// arrives in Phase 4-6 when the runner/gitops services start consuming
// pipeline signals.
package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Repo is the slice of repo.IntegrationsRepo this adapter needs. Defining it
// here (rather than depending on the concrete type) keeps the test fakes
// minimal — see provider_test.go's fakeRepo.
type Repo interface {
	Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
	Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
	Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

// Provider is the GitHub Integration adapter. The webhook secret is stored
// encrypted at rest via the injected KeyVault; defaultSecret is a fallback used
// only on the dev/mock webhook path before a connection row exists.
type Provider struct {
	repo Repo
	kv   domain.KeyVault
	// defaultSecret is used to verify HMACs when no connection row exists yet.
	// In production every org connects before sending events, so this only
	// matters for the local-dev mock-installation flow.
	defaultSecret []byte
}

// New builds a Provider. defaultSecret is the cfg.GitHubDefaultWebhookSecret
// loaded from GITHUB_WEBHOOK_SECRET; pass nil if you want the adapter to
// reject HMACs when no per-tenant secret is configured.
func New(repo Repo, kv domain.KeyVault, defaultSecret []byte) *Provider {
	return &Provider{repo: repo, kv: kv, defaultSecret: defaultSecret}
}

// Name returns the canonical provider identifier.
func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationGitHub }

// Connect upserts a connection row. Config map must contain "installation_id"
// (GitHub App installation id); "webhook_secret" is optional and falls back to
// the default secret for the dev mock path.
func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	installID, _ := cfg["installation_id"].(string)
	if installID == "" {
		return domain.Connection{}, errors.New("github: installation_id required")
	}
	secret, _ := cfg["webhook_secret"].(string)
	if secret == "" {
		secret = string(p.defaultSecret)
	}
	encSecret, err := p.kv.Encrypt(ctx, []byte(secret))
	if err != nil {
		return domain.Connection{}, fmt.Errorf("github: encrypt secret: %w", err)
	}
	scopes, _ := cfg["scopes"].([]any)
	meta := map[string]any{"scopes": scopes}
	c := domain.Connection{
		Provider:       domain.IntegrationGitHub,
		Status:         domain.StatusConnected,
		InstallationID: installID,
		Metadata:       meta,
	}
	if err := p.repo.Upsert(ctx, princ.OrgID, c, encSecret); err != nil {
		return domain.Connection{}, err
	}
	return c, nil
}

// Disconnect removes the (org, github) integration row. Idempotent.
func (p *Provider) Disconnect(ctx context.Context, princ domain.Principal) error {
	return p.repo.Delete(ctx, princ.OrgID, domain.IntegrationGitHub)
}

// Status returns the current Connection for the caller's org. Returns
// domain.ErrNotFound if no connection exists.
func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
	c, _, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationGitHub)
	return c, err
}

// HandleWebhook verifies the X-Hub-Signature-256 header against the per-tenant
// secret (or defaultSecret if no row exists yet) and acks the event. Phase 3
// stops here — Phase 4-6 will branch on X-Github-Event to enqueue pipeline
// work.
func (p *Provider) HandleWebhook(ctx context.Context, orgID string, headers map[string]string, body []byte) error {
	sig := headers["X-Hub-Signature-256"]
	if !strings.HasPrefix(sig, "sha256=") {
		return errors.New("github: missing X-Hub-Signature-256")
	}

	_, encSecret, err := p.repo.Get(ctx, orgID, domain.IntegrationGitHub)
	var secret []byte
	if err == nil && encSecret != nil {
		secret, err = p.kv.Decrypt(ctx, encSecret)
		if err != nil {
			return fmt.Errorf("github: decrypt secret: %w", err)
		}
	} else {
		// Allow default secret in dev (no connection row yet).
		secret = p.defaultSecret
	}

	want := hmac.New(sha256.New, secret)
	want.Write(body)
	gotHex := strings.TrimPrefix(sig, "sha256=")
	if !hmac.Equal([]byte(hex.EncodeToString(want.Sum(nil))), []byte(gotHex)) {
		return errors.New("github: HMAC mismatch")
	}

	// Phase 3: ack only. Event-specific routing lands in Phase 4-6.
	return nil
}

// compile-time conformance check
var _ domain.Integration = (*Provider)(nil)
