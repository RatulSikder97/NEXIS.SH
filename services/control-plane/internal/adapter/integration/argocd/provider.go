// Package argocd implements the ArgoCD IntegrationProvider — token-only,
// no webhooks in Phase 3. We store a server_url + bearer token under the
// KeyVault so the gitops service can pull deployment state in later phases.
package argocd

import (
	"context"
	"errors"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Repo is the subset of repo.IntegrationsRepo this adapter needs.
type Repo interface {
	Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
	Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
	Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

// Provider is the ArgoCD Integration adapter.
type Provider struct {
	repo Repo
	kv   domain.KeyVault
}

// New builds a Provider.
func New(repo Repo, kv domain.KeyVault) *Provider { return &Provider{repo: repo, kv: kv} }

// Name returns the canonical provider identifier.
func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationArgoCD }

// Connect upserts an ArgoCD connection. Both server_url and token are
// required — there is no mock path here.
func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	serverURL, _ := cfg["server_url"].(string)
	token, _ := cfg["token"].(string)
	if serverURL == "" || token == "" {
		return domain.Connection{}, errors.New("argocd: server_url and token required")
	}
	enc, err := p.kv.Encrypt(ctx, []byte(token))
	if err != nil {
		return domain.Connection{}, err
	}
	c := domain.Connection{
		Provider: domain.IntegrationArgoCD,
		Status:   domain.StatusConnected,
		Metadata: map[string]any{"server_url": serverURL},
	}
	if err := p.repo.Upsert(ctx, princ.OrgID, c, enc); err != nil {
		return domain.Connection{}, err
	}
	return c, nil
}

// Disconnect removes the (org, argocd) integration row.
func (p *Provider) Disconnect(ctx context.Context, princ domain.Principal) error {
	return p.repo.Delete(ctx, princ.OrgID, domain.IntegrationArgoCD)
}

// Status returns the current Connection for the caller's org.
func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
	c, _, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationArgoCD)
	return c, err
}

// HandleWebhook is a hard error — ArgoCD does not deliver webhooks to us in
// Phase 3. The gitops service polls instead.
func (p *Provider) HandleWebhook(_ context.Context, _ string, _ map[string]string, _ []byte) error {
	return errors.New("argocd: no webhooks in Phase 3")
}

// compile-time conformance check
var _ domain.Integration = (*Provider)(nil)
