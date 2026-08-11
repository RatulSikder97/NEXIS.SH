// Package sentry implements the Sentry IntegrationProvider.
//
// Phase 3 originally only stored a per-tenant webhook secret and HMAC-verified
// inbound events. The Stage-0 rewrite (Task 3 of the real-integrations plan)
// upgrades the adapter to:
//
//  1. Validate the supplied auth_token against Sentry's REST API at Connect
//     time so we never persist an unusable secret.
//  2. Persist a structured secret payload — auth_token + project_slug +
//     organization_slug + client_secret — sealed through the KeyVault.
//  3. Surface per-project event counts via Status() for the UI.
//  4. Pull a 5-minute backfill window via the BackfillRecent / cron path so
//     incidents land even when Sentry's webhook fan-out is degraded.
//  5. Promote inbound `issue.created` webhook bodies to the IncidentSink
//     with a stable fingerprint = Sentry issue id.
//
// Secret-at-rest contract: every byte of state that could leak a Sentry
// credential lives in `secret` (the encrypted blob) and the metadata map only
// holds non-secret slugs.
package sentry

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// repoIfc is the subset of repo.IntegrationsRepo this adapter needs. Defined
// here so provider_test.go can pass a fake without depending on pgx.
type repoIfc interface {
	Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
	Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
	Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

// Repo retains the public Phase-3 alias so consumers outside this package
// (factory.go) keep compiling. Eventually we can drop it.
type Repo = repoIfc

// Config tunes the provider. Zero value picks production defaults.
type Config struct {
	BaseURL string // "https://sentry.io" by default — set to self-hosted base when needed
}

// secretBlob is the structured secret persisted via KeyVault. JSON-encoded
// before encryption so future fields (refresh tokens, scopes) can be added
// without a schema migration.
type secretBlob struct {
	AuthToken    string `json:"auth_token"`
	ProjectSlug  string `json:"project_slug"`
	OrgSlug      string `json:"organization_slug"`
	ClientSecret string `json:"client_secret"` // HMAC secret for inbound webhook signature
}

// Provider is the Sentry Integration adapter. Wiring is split across the
// constructor (New) and an optional SetClient call — production wires the
// REST client at boot, but tests / dev paths that never call Sentry can
// leave it nil and the methods that need the client return a clear error.
type Provider struct {
	repo repoIfc
	kv   domain.KeyVault
	sink domain.IncidentSink

	mu     sync.RWMutex
	client *Client
	cfg    Config

	// dedupe is the in-memory fingerprint ring used by the backfill cron.
	// Bounded at 1000 entries per process; cron loops are idempotent within
	// that window. Cleared on restart, which is fine — Sentry's issue ids
	// only change when an issue is re-grouped, and the worst case is a
	// duplicate row on the first tick after a restart.
	dedupe *fingerprintLRU
}

// New builds a Provider with no REST client wired. The provider remains
// usable for the HMAC-only webhook path (HandleWebhook) but Connect /
// Status / BackfillRecent return an error until SetClient is called.
//
// sink is the IncidentsRepo (or test fake) that persists raw events on each
// verified webhook OR each backfill emit.
func New(repo repoIfc, kv domain.KeyVault, sink domain.IncidentSink) *Provider {
	return &Provider{
		repo:   repo,
		kv:     kv,
		sink:   sink,
		dedupe: newFingerprintLRU(1000),
	}
}

// SetClient attaches the REST client + config. Called from cmd/server/main.go
// once the integration registry is built. Safe to call once; replacing a
// live client is allowed for tests.
func (p *Provider) SetClient(c *Client, cfg Config) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.client = c
	p.cfg = cfg
}

// getClient returns the wired client + a typed error when it is missing.
// Methods that depend on REST access call this so the error message points
// at the configuration omission rather than panicking on nil.
func (p *Provider) getClient() (*Client, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.client == nil {
		return nil, errors.New("sentry: REST client not wired — set SENTRY_BASE_URL and call SetClient")
	}
	return p.client, nil
}

// Name returns the canonical provider identifier.
func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationSentry }

// Connect persists a Sentry connection after validating the supplied
// credentials against Sentry's API. Config keys (all strings, all required
// except client_secret):
//
//   - auth_token         — the Sentry auth token (org-scoped or higher)
//   - organization_slug  — the Sentry org slug the token belongs to
//   - project_slug       — the project this tenant wants to monitor
//   - client_secret      — HMAC secret for the inbound webhook (optional;
//     defaults to auth_token when omitted, which matches Sentry's "use
//     the integration's secret" guidance)
//
// On success the encrypted blob carries every field above plus the slugs;
// metadata stores only non-secret slugs.
func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	authToken, _ := cfg["auth_token"].(string)
	orgSlug, _ := cfg["organization_slug"].(string)
	projectSlug, _ := cfg["project_slug"].(string)
	clientSecret, _ := cfg["client_secret"].(string)

	if authToken == "" {
		return domain.Connection{}, errors.New("sentry: auth_token required")
	}
	if orgSlug == "" {
		return domain.Connection{}, errors.New("sentry: organization_slug required")
	}
	if projectSlug == "" {
		return domain.Connection{}, errors.New("sentry: project_slug required")
	}
	if clientSecret == "" {
		// Match Sentry's default behaviour: the auth token doubles as the
		// HMAC secret unless the operator sets a separate one. We keep
		// these split in storage so a future rotate-secret call can
		// change one without invalidating the other.
		clientSecret = authToken
	}

	client, err := p.getClient()
	if err != nil {
		return domain.Connection{}, err
	}
	if _, err := client.ValidateToken(ctx, authToken, orgSlug); err != nil {
		return domain.Connection{}, fmt.Errorf("sentry: validate token: %w", err)
	}
	if err := client.AssertProjectExists(ctx, authToken, orgSlug, projectSlug); err != nil {
		return domain.Connection{}, err
	}

	raw, err := json.Marshal(secretBlob{
		AuthToken: authToken, ProjectSlug: projectSlug, OrgSlug: orgSlug, ClientSecret: clientSecret,
	})
	if err != nil {
		return domain.Connection{}, fmt.Errorf("sentry: marshal secret: %w", err)
	}
	enc, err := p.kv.Encrypt(ctx, raw)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("sentry: encrypt secret: %w", err)
	}
	c := domain.Connection{
		Provider: domain.IntegrationSentry,
		Status:   domain.StatusConnected,
		Metadata: map[string]any{
			"organization_slug": orgSlug,
			"project_slug":      projectSlug,
		},
	}
	if err := p.repo.Upsert(ctx, princ.OrgID, c, enc); err != nil {
		return domain.Connection{}, err
	}
	return c, nil
}

// Disconnect removes the (org, sentry) integration row.
func (p *Provider) Disconnect(ctx context.Context, princ domain.Principal) error {
	return p.repo.Delete(ctx, princ.OrgID, domain.IntegrationSentry)
}

// Status returns the current Connection enriched with a real
// events_1h gauge from Sentry. When the REST client is not wired or the
// gauge query fails, we still return the persisted Connection so the UI can
// render the rest of the integration state — the error is folded into
// metadata under "stats_error".
func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
	c, enc, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationSentry)
	if err != nil {
		return domain.Connection{}, err
	}
	blob, err := p.decryptBlob(ctx, enc)
	if err != nil {
		// Without the secret we cannot enrich; return the persisted row.
		return c, nil
	}
	client, err := p.getClient()
	if err != nil {
		return c, nil
	}
	events, statsErr := client.ProjectStats(ctx, blob.AuthToken, blob.OrgSlug, blob.ProjectSlug)
	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	c.Metadata["organization_slug"] = blob.OrgSlug
	c.Metadata["project_slug"] = blob.ProjectSlug
	if statsErr != nil {
		c.Metadata["stats_error"] = statsErr.Error()
		return c, nil
	}
	c.Metadata["events_1h"] = events
	c.Metadata["events_checked_at"] = nowFn().UTC().Format("2006-01-02T15:04:05Z")
	return c, nil
}

// HandleWebhook verifies Sentry-Hook-Signature against the per-tenant
// client_secret, decodes the canonical event envelope, and either promotes
// the body to the IncidentSink (for `issue.created`) or no-ops with a debug
// log. The org-level connection MUST exist — there is no defaultSecret
// fallback for Sentry.
//
// Sentry's signature scheme is HMAC-SHA256 over the raw request body, hex
// encoded. Header is `Sentry-Hook-Signature`. The hook resource type is
// carried in `Sentry-Hook-Resource` (e.g. "issue", "event_alert").
func (p *Provider) HandleWebhook(ctx context.Context, orgID string, headers map[string]string, body []byte) error {
	sig := headers["Sentry-Hook-Signature"]
	if sig == "" {
		return errors.New("sentry: missing Sentry-Hook-Signature")
	}

	_, enc, err := p.repo.Get(ctx, orgID, domain.IntegrationSentry)
	if err != nil || enc == nil {
		return fmt.Errorf("sentry: no connection for org=%s", orgID)
	}
	blob, err := p.decryptBlob(ctx, enc)
	if err != nil {
		return fmt.Errorf("sentry: decrypt: %w", err)
	}

	// Two secret modes are in production today:
	//   - Legacy: the encrypted blob is the raw HMAC secret bytes (Phase 3
	//     pre-rewrite). decryptBlob falls back to ClientSecret=string(raw)
	//     when the bytes are not JSON.
	//   - Current: ClientSecret is a field on the structured blob.
	secret := []byte(blob.ClientSecret)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(sig)) {
		return errors.New("sentry: HMAC mismatch")
	}

	resource := headers["Sentry-Hook-Resource"]
	switch resource {
	case "issue":
		return p.handleIssueEvent(ctx, orgID, blob, body)
	default:
		// Any other resource type — ack-only for Phase 3. Future phases can
		// extend this switch (event_alert, metric_alert, ...).
		return p.handleLegacyEvent(ctx, orgID, blob, body)
	}
}

// handleIssueEvent decodes Sentry's "issue.created" envelope and emits a
// RawIncident to the sink. The envelope shape is documented at:
//
//	https://docs.sentry.io/product/integrations/integration-platform/webhooks/issues/
//
// Fingerprint is the Sentry issue id (`data.issue.id`) which is stable
// across event volume — the canonical dedupe key.
//
// blob is the decrypted secret blob — we re-use its OrgSlug / ProjectSlug
// values to fill the RawIncident fingerprint so Sentinel's router can
// resolve a project_id from the row without re-decoding the body.
func (p *Provider) handleIssueEvent(ctx context.Context, orgID string, blob secretBlob, body []byte) error {
	var envelope struct {
		Action string `json:"action"`
		Data   struct {
			Issue struct {
				ID      string `json:"id"`
				Title   string `json:"title"`
				Culprit string `json:"culprit"`
				Level   string `json:"level"`
				Project struct {
					Slug string `json:"slug"`
				} `json:"project"`
				Environment string         `json:"environment"`
				Metadata    map[string]any `json:"metadata"`
			} `json:"issue"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("sentry: decode issue webhook: %w", err)
	}
	issue := envelope.Data.Issue
	if issue.ID == "" {
		// Malformed envelope — fall back to the legacy decoder so we still
		// land *something* in incidents_raw for forensic replay.
		return p.handleLegacyEvent(ctx, orgID, blob, body)
	}
	// Webhook is the source of truth; the backfill cron dedupes against
	// fingerprints it already emitted in its own window.
	p.dedupe.Add(issue.ID)

	// Prefer the slug embedded in the payload (always matches the issue's
	// originating project); fall back to the blob's persisted project slug.
	projectSlug := issue.Project.Slug
	if projectSlug == "" {
		projectSlug = blob.ProjectSlug
	}

	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	return p.sink.Insert(ctx, orgID, domain.RawIncident{
		Source:        "sentry",
		SourceEventID: issue.ID,
		Title:         issue.Title,
		Level:         issue.Level,
		Service:       issue.Project.Slug,
		Environment:   issue.Environment,
		Payload:       payload,
		// Fingerprint fields — feed Sentinel's project router.
		SentryOrganizationSlug: blob.OrgSlug,
		SentryProjectSlug:      projectSlug,
	})
}

// handleLegacyEvent is the pre-rewrite decoder path. Sentry's older
// "raw event" webhooks (and our test fixtures) deliver a flat shape rather
// than the envelope; the original Phase 3 adapter targeted exactly this.
// Keep it around so existing tenants on the older delivery path do not
// regress.
func (p *Provider) handleLegacyEvent(ctx context.Context, orgID string, blob secretBlob, body []byte) error {
	var evt struct {
		ID          string     `json:"id"`
		Level       string     `json:"level"`
		Title       string     `json:"title"`
		Environment string     `json:"environment"`
		Tags        [][]string `json:"tags"`
	}
	if err := json.Unmarshal(body, &evt); err != nil {
		return fmt.Errorf("sentry: decode: %w", err)
	}
	service := ""
	for _, t := range evt.Tags {
		if len(t) >= 2 && t[0] == "service" {
			service = t[1]
			break
		}
	}
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	return p.sink.Insert(ctx, orgID, domain.RawIncident{
		Source:        "sentry",
		SourceEventID: evt.ID,
		Title:         evt.Title,
		Level:         evt.Level,
		Service:       service,
		Environment:   evt.Environment,
		Payload:       payload,
		// Legacy path doesn't carry the project slug in the body — fall
		// back to the persisted blob so Sentinel can still route.
		SentryOrganizationSlug: blob.OrgSlug,
		SentryProjectSlug:      blob.ProjectSlug,
	})
}

// decryptBlob returns the structured secretBlob for a stored connection.
// If the persisted ciphertext predates the JSON-blob format (Phase 3
// pre-rewrite, where the raw HMAC secret was stored unwrapped) we fall back
// to treating the entire decrypted byte slice as ClientSecret so legacy
// connections keep verifying webhooks without a forced re-Connect.
func (p *Provider) decryptBlob(ctx context.Context, enc []byte) (secretBlob, error) {
	raw, err := p.kv.Decrypt(ctx, enc)
	if err != nil {
		return secretBlob{}, err
	}
	var blob secretBlob
	if err := json.Unmarshal(raw, &blob); err != nil || blob.ClientSecret == "" && blob.AuthToken == "" {
		// Legacy raw-secret path.
		return secretBlob{ClientSecret: string(raw)}, nil
	}
	return blob, nil
}

// nowFn is a package-level injection seam for tests that need to assert on
// the timestamp embedded in Status() metadata. Default to time.Now via a
// var so tests can swap it out without exporting a setter.
var nowFn = defaultNow

// compile-time conformance check
var _ domain.Integration = (*Provider)(nil)
