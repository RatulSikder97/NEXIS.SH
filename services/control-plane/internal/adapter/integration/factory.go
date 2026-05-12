// Package integration wires the three provider adapters (github/sentry/argocd)
// into a single Registry exposed to the HTTP layer. The Registry is the only
// adapter-level type the transport package depends on; routers stay agnostic
// to the concrete provider implementations.
package integration

import (
	"context"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/argocd"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/github"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/sentry"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Registry is the composite holding every provider Integration. Implements
// domain.IntegrationRegistry.
type Registry struct {
	providers map[domain.IntegrationProvider]domain.Integration
	repo      *repo.IntegrationsRepo
}

// Deps bundles every dependency NewRegistry needs. Constructed in
// cmd/server/main.go.
type Deps struct {
	Repo                *repo.IntegrationsRepo
	KV                  domain.KeyVault
	IncidentSink        domain.IncidentSink
	GitHubDefaultSecret []byte
}

// NewRegistry constructs the three Phase 3 adapters and registers them under
// their canonical IntegrationProvider names.
func NewRegistry(d Deps) *Registry {
	return &Registry{
		providers: map[domain.IntegrationProvider]domain.Integration{
			domain.IntegrationGitHub: github.New(d.Repo, d.KV, d.GitHubDefaultSecret),
			domain.IntegrationSentry: sentry.New(d.Repo, d.KV, d.IncidentSink),
			domain.IntegrationArgoCD: argocd.New(d.Repo, d.KV),
		},
		repo: d.Repo,
	}
}

// Get returns the Integration for the supplied provider, or false if no
// adapter is registered.
func (r *Registry) Get(p domain.IntegrationProvider) (domain.Integration, bool) {
	v, ok := r.providers[p]
	return v, ok
}

// List returns the caller's org's Connections via the underlying repo.
func (r *Registry) List(ctx context.Context, princ domain.Principal) ([]domain.Connection, error) {
	return r.repo.List(ctx, princ.OrgID)
}

// compile-time conformance check
var _ domain.IntegrationRegistry = (*Registry)(nil)
