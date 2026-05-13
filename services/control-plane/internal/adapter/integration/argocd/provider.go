// Package argocd implements the ArgoCD IntegrationProvider — a real REST
// client that lets the control-plane validate connectivity at Connect time,
// report live sync + health status, and (via Sync/Rollback) drive the
// Approval Gate's post-merge deploy path. ArgoCD does not push webhooks at us
// in this MVP, so HandleWebhook is a soft no-op.
package argocd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Repo is the subset of repo.IntegrationsRepo this adapter needs. Defining it
// here (rather than depending on the concrete type) keeps the test fakes
// minimal — see provider_test.go's fakeRepo.
type Repo interface {
	Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
	Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
	Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

// ClientBuilder constructs a *Client for a given base URL + bearer token.
// Provider owns this seam so tests can inject fakes without spinning up an
// httptest server per case. The default builder uses a shared httpx wrapper
// so the breaker + rate limit + retry policy applies consistently.
type ClientBuilder func(base, token string) *Client

// secretEnvelope is the JSON shape we encrypt and persist under the
// integration row's secret column. Keeping all four fields together keeps
// repo writes atomic — there is no scenario where the token rotates without
// the project + app name being refreshed in lockstep.
type secretEnvelope struct {
	ServerURL string `json:"server_url"`
	AuthToken string `json:"auth_token"`
	Project   string `json:"project"`
	AppName   string `json:"app_name"`
}

// Provider is the ArgoCD Integration adapter.
type Provider struct {
	repo    Repo
	kv      domain.KeyVault
	builder ClientBuilder
}

// New builds a Provider with the default ClientBuilder (an httpx-wrapped REST
// client). Tests call SetClientBuilder to inject fakes.
func New(repo Repo, kv domain.KeyVault) *Provider {
	return &Provider{
		repo:    repo,
		kv:      kv,
		builder: defaultClientBuilder(),
	}
}

// SetClientBuilder overrides the function used to construct per-call clients.
// Intended for tests; production code should leave the default in place.
func (p *Provider) SetClientBuilder(b ClientBuilder) {
	if b == nil {
		return
	}
	p.builder = b
}

// defaultClientBuilder returns the production builder — a shared httpx.Client
// applies its breaker + rate limit across every ArgoCD upstream we touch.
// Each call gets a fresh *Client (cheap; the heavy state lives in httpx),
// which lets the same Provider serve many orgs with different bases without
// the per-host state colliding.
func defaultClientBuilder() ClientBuilder {
	shared := httpx.New(&http.Client{Timeout: 30 * time.Second}, httpx.Config{})
	return func(base, token string) *Client {
		return NewClient(base, token, shared)
	}
}

// Name returns the canonical provider identifier.
func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationArgoCD }

// Connect validates reachability + authz against the ArgoCD server and
// persists the encrypted credential. Required cfg keys: server_url,
// auth_token, project, app_name. Validation:
//
//  1. /api/version succeeds → server reachable + token format accepted.
//  2. /api/v1/applications/{app_name} succeeds → token has read access to the
//     specific application the caller intends to drive.
//
// Both probes must pass before we write to the repo; a half-configured
// connection (where Status would fail on every call) is worse than no row.
func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	env, err := envelopeFromCfg(cfg)
	if err != nil {
		return domain.Connection{}, err
	}

	cli := p.builder(env.ServerURL, env.AuthToken)
	version, err := cli.Version(ctx)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("argocd: version probe: %w", err)
	}
	if _, err := cli.GetApplication(ctx, env.Project, env.AppName); err != nil {
		return domain.Connection{}, fmt.Errorf("argocd: app lookup: %w", err)
	}

	raw, err := json.Marshal(env)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("argocd: marshal envelope: %w", err)
	}
	enc, err := p.kv.Encrypt(ctx, raw)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("argocd: encrypt: %w", err)
	}

	c := domain.Connection{
		Provider: domain.IntegrationArgoCD,
		Status:   domain.StatusConnected,
		Metadata: map[string]any{
			"server_url": env.ServerURL,
			"project":    env.Project,
			"app_name":   env.AppName,
			"version":    version,
		},
	}
	if err := p.repo.Upsert(ctx, princ.OrgID, c, enc); err != nil {
		return domain.Connection{}, err
	}
	return c, nil
}

// Disconnect removes the (org, argocd) integration row. Idempotent.
func (p *Provider) Disconnect(ctx context.Context, princ domain.Principal) error {
	return p.repo.Delete(ctx, princ.OrgID, domain.IntegrationArgoCD)
}

// Status returns the live Connection. We make one round-trip to
// /api/v1/applications/{name} per call — that single request gives us both
// sync status and health status — and surface the result through the
// Connection.Metadata map for the UI's health pill.
//
// If the repo lookup fails (no row), the underlying error is returned
// verbatim; if the API call fails (token revoked, app deleted) we synthesise
// an "error" status so the UI can distinguish "configured but broken" from
// "never configured".
func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
	conn, env, err := p.loadEnvelope(ctx, princ.OrgID)
	if err != nil {
		return domain.Connection{}, err
	}

	start := time.Now()
	cli := p.builder(env.ServerURL, env.AuthToken)
	app, apiErr := cli.GetApplication(ctx, env.Project, env.AppName)
	latency := time.Since(start)

	meta := mergeMeta(conn.Metadata, map[string]any{
		"server_url": env.ServerURL,
		"project":    env.Project,
		"app_name":   env.AppName,
		"latency_ms": latency.Milliseconds(),
	})
	if apiErr != nil {
		meta["last_error"] = apiErr.Error()
		return domain.Connection{
			Provider:  domain.IntegrationArgoCD,
			Status:    domain.StatusError,
			Metadata:  meta,
			LastError: apiErr.Error(),
			CreatedAt: conn.CreatedAt,
			UpdatedAt: conn.UpdatedAt,
		}, nil
	}

	meta["sync_status"] = app.SyncStatus
	meta["health_status"] = app.HealthStatus
	meta["revision"] = app.Revision
	return domain.Connection{
		Provider:  domain.IntegrationArgoCD,
		Status:    domain.StatusConnected,
		Metadata:  meta,
		CreatedAt: conn.CreatedAt,
		UpdatedAt: conn.UpdatedAt,
	}, nil
}

// HandleWebhook is intentionally a no-op. ArgoCD does not push events to the
// control-plane in this MVP — sync/rollback decisions come from our own
// approval gate, not from ArgoCD. Phase 7+ may invert this; until then we ack
// quietly so an accidentally-configured webhook does not 500.
func (p *Provider) HandleWebhook(_ context.Context, _ string, _ map[string]string, _ []byte) error {
	return nil
}

// Sync triggers an ArgoCD sync for the caller's configured application. The
// Approval Gate calls this on a successful merge to push the new revision out
// to the cluster.
//
// revision may be empty — in that case ArgoCD syncs to whatever the
// application's tracking branch resolves to. Callers driving a specific
// post-merge commit should pass the SHA explicitly.
func (p *Provider) Sync(ctx context.Context, princ domain.Principal, revision string) (Operation, error) {
	_, env, err := p.loadEnvelope(ctx, princ.OrgID)
	if err != nil {
		return Operation{}, err
	}
	cli := p.builder(env.ServerURL, env.AuthToken)
	return cli.SyncApp(ctx, env.Project, env.AppName, SyncReq{
		Revision: revision,
		Prune:    false,
		DryRun:   false,
	})
}

// Rollback reverts the configured application to the most recent successful
// deploy *before* the current head. The caller (Approval Gate) invokes this
// when post-deploy SLO probes fail — picking the prior-known-healthy revision
// is the safest default; smarter strategies (last-known-good across N
// failures) are out of scope here.
//
// ArgoCD's history is ordered oldest-first, so the last element is the
// current revision and the second-to-last is the rollback target. Fewer than
// two entries → there is nothing to roll back to.
func (p *Provider) Rollback(ctx context.Context, princ domain.Principal) (Operation, error) {
	_, env, err := p.loadEnvelope(ctx, princ.OrgID)
	if err != nil {
		return Operation{}, err
	}
	cli := p.builder(env.ServerURL, env.AuthToken)
	history, err := cli.ListHistory(ctx, env.Project, env.AppName)
	if err != nil {
		return Operation{}, fmt.Errorf("argocd: list history: %w", err)
	}
	if len(history) < 2 {
		return Operation{}, errors.New("argocd: rollback unavailable: fewer than 2 history entries")
	}
	target := history[len(history)-2]
	return cli.Rollback(ctx, env.Project, env.AppName, target.ID)
}

// envelopeFromCfg validates the four required keys and unpacks them into the
// typed envelope. Defining this as a free function keeps Connect's body
// focused on the side-effect ordering.
func envelopeFromCfg(cfg map[string]any) (secretEnvelope, error) {
	serverURL, _ := cfg["server_url"].(string)
	authToken, _ := cfg["auth_token"].(string)
	project, _ := cfg["project"].(string)
	appName, _ := cfg["app_name"].(string)
	if serverURL == "" || authToken == "" || project == "" || appName == "" {
		return secretEnvelope{}, errors.New("argocd: server_url, auth_token, project, app_name all required")
	}
	return secretEnvelope{
		ServerURL: serverURL,
		AuthToken: authToken,
		Project:   project,
		AppName:   appName,
	}, nil
}

// loadEnvelope is the shared "fetch + decrypt + decode" path used by Status,
// Sync, and Rollback. Returns the persisted Connection (for its CreatedAt /
// UpdatedAt fields) alongside the decoded envelope.
func (p *Provider) loadEnvelope(ctx context.Context, orgID string) (domain.Connection, secretEnvelope, error) {
	conn, enc, err := p.repo.Get(ctx, orgID, domain.IntegrationArgoCD)
	if err != nil {
		return domain.Connection{}, secretEnvelope{}, err
	}
	if enc == nil {
		return domain.Connection{}, secretEnvelope{}, errors.New("argocd: connection has no stored secret")
	}
	raw, err := p.kv.Decrypt(ctx, enc)
	if err != nil {
		return domain.Connection{}, secretEnvelope{}, fmt.Errorf("argocd: decrypt: %w", err)
	}
	var env secretEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return domain.Connection{}, secretEnvelope{}, fmt.Errorf("argocd: decode envelope: %w", err)
	}
	return conn, env, nil
}

// mergeMeta returns a fresh map that combines the persisted metadata with the
// live values. Persisted keys win on duplicate so Connect-time metadata
// (e.g. version) survives across Status calls without us having to re-probe
// every time.
func mergeMeta(base, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range extra {
		out[k] = v
	}
	for k, v := range base {
		out[k] = v
	}
	return out
}

// compile-time conformance check
var _ domain.Integration = (*Provider)(nil)
