// Package github implements the GitHub IntegrationProvider. The adapter is
// dual-mode by design:
//
//   - Stub mode (the original Phase 3 shape): when SetConfig is never called
//     the Provider behaves like the prior dev/mock implementation —
//     installation_id is accepted verbatim, no upstream calls are made, and
//     webhooks are HMAC-verified against defaultSecret. This keeps the
//     existing unit tests and local-dev "fake-install" flow working.
//
//   - App mode (Phase 6 onwards): once main.go calls SetConfig with a real
//     *Client + AppCreds + webhook secret, the Provider validates installation
//     IDs by minting tokens, surfaces a repo preview on Status, and dispatches
//     installation.deleted / pull_request.closed webhooks. Real connections
//     persist a sha256 of the most recent installation token so log forensics
//     can correlate audit rows with token rotations.
package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

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

// Config is the full set of App-mode parameters. Pass to SetConfig from
// main.go; pass an empty Config (or never call SetConfig) to stay in stub
// mode. Fields are deliberately exported so wiring code can build the struct
// literal without going through helpers.
type Config struct {
	AppID             int64
	PrivateKeyPEM     []byte
	AppSlug           string     // for install-redirect URL surfaced via Status metadata
	WebhookSecret     []byte     // HMAC-SHA256 key for X-Hub-Signature-256
	AppBaseURL        string     // OAuth callback redirect_uri base
	InstallationCache RedisCache // optional; nil = re-mint on every call
}

// Provider is the GitHub Integration adapter. Stays usable with zero
// configuration so existing tests and local-dev paths do not break; SetConfig
// upgrades it into App mode.
type Provider struct {
	repo Repo
	kv   domain.KeyVault
	sink domain.IncidentSink

	// defaultSecret backs the stub HMAC path when no per-tenant secret has
	// been encrypted into the repo yet. Production paths always have a per-
	// tenant secret, so this is only exercised during local-dev mock installs.
	defaultSecret []byte

	// App-mode fields are nil-valued in stub mode. The hasAppMode() helper
	// keeps the dispatch readable.
	cfg    Config
	client *Client
	cache  *installationTokenCache
	log    *slog.Logger
	now    func() time.Time
}

// New builds a Provider in stub mode. The signature is identical to the
// previous Phase 3 constructor so factory.go does not change. Call SetConfig
// from main.go to enable App mode.
func New(repo Repo, kv domain.KeyVault, defaultSecret []byte) *Provider {
	return &Provider{
		repo:          repo,
		kv:            kv,
		defaultSecret: defaultSecret,
		log:           slog.Default(),
		now:           time.Now,
	}
}

// SetConfig wires the real App credentials + cache. After this call returns
// the Provider operates in App mode: Connect validates installation IDs by
// minting a token, Status mints a fresh one and lists repos, and webhooks
// surface installation.deleted / pull_request.closed.
//
// Passing a nil client reverts to stub mode (used by tests to undo a previous
// SetConfig).
func (p *Provider) SetConfig(cfg Config, client *Client) {
	p.cfg = cfg
	p.client = client
	p.cache = newInstallationTokenCache(cfg.InstallationCache)
}

// SetIncidentSink is an optional injection used to emit audit events on
// pull_request.closed/merged webhooks. nil is allowed; the adapter degrades
// to a debug-log "noop" path. This is wired separately from SetConfig so
// callers that only want the App-mode auth changes do not have to plumb an
// IncidentSink they may not have.
func (p *Provider) SetIncidentSink(sink domain.IncidentSink) { p.sink = sink }

// hasAppMode reports whether SetConfig has installed real credentials. It is
// the single point of truth for stub-vs-App branching, so flipping the mode
// is a one-line change rather than a scattered conditional.
func (p *Provider) hasAppMode() bool { return p.client != nil && p.cfg.AppID > 0 }

// Name returns the canonical provider identifier.
func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationGitHub }

// Connect persists the tenant's installation. In App mode we first exchange
// the installation_id for an access token — that round-trip is the only way
// to prove the App is installed on the target org. The token's sha256 hash
// goes into metadata.last_token_sha256 so subsequent audit rows can be
// correlated with token rotations.
func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	installID, _ := cfg["installation_id"].(string)
	if installID == "" {
		return domain.Connection{}, errors.New("github: installation_id required")
	}
	secret, _ := cfg["webhook_secret"].(string)
	if secret == "" {
		secret = string(p.defaultSecret)
	}

	meta := map[string]any{}
	if scopes, ok := cfg["scopes"].([]any); ok {
		meta["scopes"] = scopes
	}

	if p.hasAppMode() {
		id, err := strconv.ParseInt(installID, 10, 64)
		if err != nil {
			return domain.Connection{}, fmt.Errorf("github: installation_id not numeric: %w", err)
		}
		tok, expiresAt, err := p.client.ExchangeInstallationToken(ctx, id)
		if err != nil {
			return domain.Connection{}, fmt.Errorf("github: validate installation: %w", err)
		}
		// Hash the token rather than storing it — the App private key can
		// always mint a fresh one, so the hash is only forensic.
		sum := sha256.Sum256([]byte(tok))
		meta["last_token_sha256"] = hex.EncodeToString(sum[:])
		meta["last_token_minted_at"] = p.now().UTC().Format(time.RFC3339)
		if p.cfg.AppSlug != "" {
			meta["app_slug"] = p.cfg.AppSlug
		}
		// Pre-warm the cache so a Status poll right after Connect skips the
		// mint round-trip.
		p.cache.Set(ctx, id, tok, expiresAt, p.now())
	}

	encSecret, err := p.kv.Encrypt(ctx, []byte(secret))
	if err != nil {
		return domain.Connection{}, fmt.Errorf("github: encrypt secret: %w", err)
	}
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

// Status returns the current Connection for the caller's org. In App mode we
// also mint (or read-through cache) an installation token and call
// /installation/repositories so the UI can show a repo preview + latency.
// Failures during the upstream call decorate LastError but do not flip
// Status — the connection itself is still configured.
func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
	c, _, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationGitHub)
	if err != nil {
		return c, err
	}
	if !p.hasAppMode() || c.InstallationID == "" {
		return c, nil
	}
	id, parseErr := strconv.ParseInt(c.InstallationID, 10, 64)
	if parseErr != nil {
		// Legacy / mock installation IDs (e.g. "inst-123") cannot be turned
		// into the integer GitHub expects. Surface as a soft error so the UI
		// shows the row but flags it as needing reconnect.
		c.LastError = "installation_id not numeric"
		return c, nil
	}

	start := p.now()
	tok, ok := p.cache.Get(ctx, id)
	if !ok {
		var expiresAt time.Time
		tok, expiresAt, err = p.client.ExchangeInstallationToken(ctx, id)
		if err != nil {
			c.LastError = err.Error()
			return c, nil
		}
		p.cache.Set(ctx, id, tok, expiresAt, p.now())
	}
	repos, err := p.client.ListInstallationRepos(ctx, tok)
	latency := p.now().Sub(start)

	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	c.Metadata["latency_ms"] = latency.Milliseconds()

	if err != nil {
		c.LastError = err.Error()
		return c, nil
	}
	preview := repos
	if len(preview) > 5 {
		preview = preview[:5]
	}
	c.Metadata["repo_count"] = len(repos)
	c.Metadata["repos"] = preview
	return c, nil
}

// MintInstallationToken returns a short-lived installation access token for
// the supplied installation id, reading through the Redis-backed cache the
// same way Status/ListReposForOrg do. It is the handler/activity-facing
// entry point for callers that already hold a project's installation id
// (deploy preflight, deploy-engine dispatch) and therefore don't need the
// org → connection lookup.
//
// The raw token is returned to the caller and NEVER logged here — callers
// needing forensics hash it first, matching Connect's last_token_sha256
// pattern.
func (p *Provider) MintInstallationToken(ctx context.Context, installationID int64) (string, error) {
	if !p.hasAppMode() {
		return "", errors.New("github: app credentials not configured")
	}
	if installationID <= 0 {
		return "", errors.New("github: installation id must be positive")
	}
	if tok, ok := p.cache.Get(ctx, installationID); ok {
		return tok, nil
	}
	tok, expiresAt, err := p.client.ExchangeInstallationToken(ctx, installationID)
	if err != nil {
		return "", fmt.Errorf("github: mint installation token: %w", err)
	}
	p.cache.Set(ctx, installationID, tok, expiresAt, p.now())
	return tok, nil
}

// GetFileContents reads one file from a repo the installation can see —
// mint (or reuse cached) token, then hit the contents API. ref may be empty
// for the default branch. A missing file surfaces as *APIError{Status: 404}
// via the client, letting callers treat "no Dockerfile" as data, not error.
func (p *Provider) GetFileContents(ctx context.Context, installationID int64, owner, repo, path, ref string) ([]byte, error) {
	tok, err := p.MintInstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	return p.client.GetFileContents(ctx, tok, owner, repo, path, ref)
}

// ListReposForOrg mints (or reuses cached) an installation token for the
// caller's org and returns the full repository projection. Powers
// GET /v1/integrations/github/repos for the project-wizard dropdown.
func (p *Provider) ListReposForOrg(ctx context.Context, princ domain.Principal) ([]RepoDetail, error) {
	c, _, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationGitHub)
	if err != nil {
		return nil, err
	}
	if !p.hasAppMode() {
		return nil, errors.New("github: app credentials not configured")
	}
	if c.InstallationID == "" {
		return nil, errors.New("github: not installed for this org")
	}
	id, err := strconv.ParseInt(c.InstallationID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("github: installation_id not numeric: %w", err)
	}
	tok, ok := p.cache.Get(ctx, id)
	if !ok {
		var expiresAt time.Time
		tok, expiresAt, err = p.client.ExchangeInstallationToken(ctx, id)
		if err != nil {
			return nil, err
		}
		p.cache.Set(ctx, id, tok, expiresAt, p.now())
	}
	return p.client.ListInstallationReposDetailed(ctx, tok)
}

// HandleWebhook verifies the X-Hub-Signature-256 header against the per-tenant
// secret (or defaultSecret if no row exists yet) and dispatches a few key
// events:
//
//   - installation.deleted → mark the connection disconnected via repo.Delete.
//   - pull_request.closed with merged=true → emit an audit event via the
//     incident sink so the gitops layer can correlate the merge with whatever
//     run originally opened the PR.
//   - anything else → ack with a debug log so operators can spot unexpected
//     payloads.
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
		// No per-tenant row yet — fall back to the App-mode webhook secret
		// (when set) and finally the legacy defaultSecret for the dev mock
		// path. The App-mode key takes precedence because it is the secret
		// configured in the GitHub App UI; defaultSecret is dev-only.
		switch {
		case p.hasAppMode() && len(p.cfg.WebhookSecret) > 0:
			secret = p.cfg.WebhookSecret
		default:
			secret = p.defaultSecret
		}
	}

	want := hmac.New(sha256.New, secret)
	want.Write(body)
	gotHex := strings.TrimPrefix(sig, "sha256=")
	if !hmac.Equal([]byte(hex.EncodeToString(want.Sum(nil))), []byte(gotHex)) {
		return errors.New("github: HMAC mismatch")
	}

	event := headers["X-GitHub-Event"]
	if event == "" {
		event = headers["X-Github-Event"]
	}
	switch event {
	case "installation":
		return p.handleInstallation(ctx, orgID, body)
	case "pull_request":
		return p.handlePullRequest(ctx, orgID, body)
	default:
		p.log.Debug("github webhook: noop", "event", event, "org", orgID)
		return nil
	}
}

// installationEvent is the slim view we read out of the installation webhook
// body. GitHub fills in many more fields; we only care about the action so we
// can branch on installation.deleted.
type installationEvent struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// handleInstallation routes installation.* webhooks. For now we only act on
// 'deleted'; other actions (created, suspended, unsuspended) are debug-logged
// so operators can spot them but not silently swallow.
func (p *Provider) handleInstallation(ctx context.Context, orgID string, body []byte) error {
	var ev installationEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return fmt.Errorf("github: parse installation event: %w", err)
	}
	if ev.Action != "deleted" {
		p.log.Debug("github webhook: installation noop", "action", ev.Action, "org", orgID)
		return nil
	}
	// Delete the connection row. We intentionally do not 404 if the row was
	// already gone — installation.deleted may race with a manual Disconnect.
	if err := p.repo.Delete(ctx, orgID, domain.IntegrationGitHub); err != nil {
		return fmt.Errorf("github: delete on installation.deleted: %w", err)
	}
	p.log.Info("github webhook: installation.deleted",
		"org", orgID, "installation_id", ev.Installation.ID)
	return nil
}

// pullRequestEvent is the slice of the pull_request webhook body we consume:
// the action + the merged flag + a few PR fields for the audit row. PR bodies
// are large (~50KB on big repos) so we only decode what we need.
type pullRequestEvent struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Merged  bool   `json:"merged"`
		HTMLURL string `json:"html_url"`
		Title   string `json:"title"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// handlePullRequest routes pull_request.* webhooks. We only act on the
// 'closed' action with merged=true — that is the signal that an auto-PR
// landed and the gitops layer should mark the run complete.
func (p *Provider) handlePullRequest(ctx context.Context, orgID string, body []byte) error {
	var ev pullRequestEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return fmt.Errorf("github: parse pull_request event: %w", err)
	}
	if ev.Action != "closed" || !ev.PullRequest.Merged {
		p.log.Debug("github webhook: pull_request noop",
			"action", ev.Action, "merged", ev.PullRequest.Merged, "org", orgID)
		return nil
	}
	if p.sink == nil {
		// Best-effort log if no sink is wired (e.g. tests that didn't call
		// SetIncidentSink). The merge has still happened upstream; we just
		// have nowhere to durably record it.
		p.log.Info("github webhook: pull_request.merged (no sink)",
			"org", orgID, "url", ev.PullRequest.HTMLURL, "title", ev.PullRequest.Title)
		return nil
	}
	raw := domain.RawIncident{
		Source:        "github",
		SourceEventID: fmt.Sprintf("pr-%s-%d", ev.Repository.FullName, ev.Number),
		Title:         "pull_request.merged: " + ev.PullRequest.Title,
		Level:         "info",
		Service:       ev.Repository.FullName,
		Environment:   "",
		Payload: map[string]any{
			"action":   ev.Action,
			"merged":   ev.PullRequest.Merged,
			"html_url": ev.PullRequest.HTMLURL,
			"number":   ev.Number,
		},
		// Fingerprint — Sentinel's router keys projects by github_repo
		// ("owner/repo"); repository.full_name already matches.
		GitHubRepo: ev.Repository.FullName,
	}
	if err := p.sink.Insert(ctx, orgID, raw); err != nil {
		// Surface but keep best-effort posture: the webhook return value
		// drives the HTTP 200 → GitHub stops retrying. We prefer to drop
		// duplicate audit rows over re-delivery storms.
		p.log.Warn("github webhook: audit insert failed", "err", err, "org", orgID)
	}
	return nil
}

// compile-time conformance check
var _ domain.Integration = (*Provider)(nil)
