// Package integration wires the six provider adapters (github/sentry/argocd/
// slack/datadog/pagerduty) into a single Registry exposed to the HTTP layer.
// The Registry is the only adapter-level type the transport package depends
// on; routers stay agnostic to the concrete provider implementations.
package integration

import (
	"context"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/argocd"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/datadog"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/github"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/pagerduty"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/sentry"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/slack"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Registry is the composite holding every provider Integration. Implements
// domain.IntegrationRegistry.
type Registry struct {
	providers map[domain.IntegrationProvider]domain.Integration
	repo      *repo.IntegrationsRepo
	kv        domain.KeyVault

	// Direct typed handles for callers that need the adapter-specific surface
	// beyond the domain.Integration port (e.g. ArgoCD sync, Slack DM,
	// PagerDuty escalate). Set by NewRegistry; never nil after init.
	GitHub    *github.Provider
	Sentry    *sentry.Provider
	ArgoCD    *argocd.Provider
	Slack     *slack.Provider
	Datadog   *datadog.Provider
	PagerDuty *pagerduty.Provider
}

// Deps bundles every dependency NewRegistry needs. Constructed in
// cmd/server/main.go.
type Deps struct {
	Repo                *repo.IntegrationsRepo
	KV                  domain.KeyVault
	IncidentSink        domain.IncidentSink
	GitHubDefaultSecret []byte
	PagerDutyFromEmail  string
	DatadogSigningSecret []byte
}

// NewRegistry constructs the six adapters and registers them under their
// canonical IntegrationProvider names. Real-API config (App credentials,
// OAuth secrets, etc.) is wired post-construction via SetConfig/SetClient
// calls in cmd/server/main.go so this factory stays config-free.
func NewRegistry(d Deps) *Registry {
	fromEmail := d.PagerDutyFromEmail
	if fromEmail == "" {
		fromEmail = "nexis-bot@nexis.dev"
	}

	gh := github.New(d.Repo, d.KV, d.GitHubDefaultSecret)
	se := sentry.New(d.Repo, d.KV, d.IncidentSink)
	ac := argocd.New(d.Repo, d.KV)
	sl := slack.New(d.Repo, d.KV)
	dd := datadog.NewWithSigningSecret(d.Repo, d.KV, d.IncidentSink, d.DatadogSigningSecret)
	pd := pagerduty.New(d.Repo, d.KV, d.IncidentSink, fromEmail)

	return &Registry{
		providers: map[domain.IntegrationProvider]domain.Integration{
			domain.IntegrationGitHub:    gh,
			domain.IntegrationSentry:    se,
			domain.IntegrationArgoCD:    ac,
			domain.IntegrationSlack:     sl,
			domain.IntegrationDatadog:   dd,
			domain.IntegrationPagerDuty: pd,
		},
		repo:      d.Repo,
		kv:        d.KV,
		GitHub:    gh,
		Sentry:    se,
		ArgoCD:    ac,
		Slack:     sl,
		Datadog:   dd,
		PagerDuty: pd,
	}
}

// Get returns the Integration for the supplied provider, or false if no
// adapter is registered.
func (r *Registry) Get(p domain.IntegrationProvider) (domain.Integration, bool) {
	v, ok := r.providers[p]
	return v, ok
}

// SetProvider replaces the adapter for a given provider. Used by main.go to
// swap in OAuth-aware variants after construction (e.g. slack.NewWithOAuth).
func (r *Registry) SetProvider(p domain.IntegrationProvider, impl domain.Integration) {
	r.providers[p] = impl
}

// List returns the caller's org's Connections via the underlying repo.
func (r *Registry) List(ctx context.Context, princ domain.Principal) ([]domain.Connection, error) {
	return r.repo.List(ctx, princ.OrgID)
}

// compile-time conformance check
var _ domain.IntegrationRegistry = (*Registry)(nil)
