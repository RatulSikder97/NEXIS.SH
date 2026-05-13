// Package slack implements the Slack IntegrationProvider. Phase 6 — Slack
// joins (github, sentry, argocd) as the 4th external integration. Unlike
// GitHub/Sentry which receive webhooks, Slack is outbound-only: the adapter
// owns a webhook URL stored encrypted at rest and POSTs Block Kit messages
// when the notifier fires.
//
// The webhook URL is sealed via KeyVault (same envelope encryption used by
// Phase 3 adapters) and NEVER returned in metadata or audit rows — only the
// channel name is surfaced to the UI.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Repo is the slice of repo.IntegrationsRepo this adapter needs. Defined
// here so tests can substitute a fake without depending on pgx.
type Repo interface {
	Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
	Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
	Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

// HTTPDoer is the minimal *http.Client surface this adapter uses. Tests
// substitute a recording roundtripper without pulling in net/http/httptest
// globally.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Provider is the Slack Integration adapter.
type Provider struct {
	repo   Repo
	kv     domain.KeyVault
	client HTTPDoer
}

// New constructs a Provider with a 10-second HTTP timeout. The KeyVault is
// the same instance every other adapter uses (a process-local AES-GCM
// implementation in dev; cloud KMS in prod).
func New(r Repo, kv domain.KeyVault) *Provider {
	return &Provider{
		repo:   r,
		kv:     kv,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewWithClient lets tests inject a recording HTTPDoer. Keep it unexported
// to the package level — production wires it via New above.
func NewWithClient(r Repo, kv domain.KeyVault, c HTTPDoer) *Provider {
	return &Provider{repo: r, kv: kv, client: c}
}

// Name returns the canonical provider identifier.
func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationSlack }

// Connect upserts a Slack connection row. Config must contain "webhook_url";
// optional "channel_name" goes into metadata for the UI. The probe POST is
// a minimal "Nexis connected" message — if Slack returns 4xx/5xx we fail
// the Connect rather than persist a broken connection.
//
// SECURITY: the webhook URL is encrypted before persistence. It is NEVER
// returned in the Connection or audit metadata — the metadata map only
// holds the channel name.
func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	url, _ := cfg["webhook_url"].(string)
	if url == "" {
		return domain.Connection{}, errors.New("slack: webhook_url required")
	}
	channel, _ := cfg["channel_name"].(string)

	if _, err := p.postRaw(ctx, url, map[string]any{"text": "Nexis connected"}); err != nil {
		return domain.Connection{}, fmt.Errorf("slack: probe failed: %w", err)
	}

	encURL, err := p.kv.Encrypt(ctx, []byte(url))
	if err != nil {
		return domain.Connection{}, fmt.Errorf("slack: encrypt webhook: %w", err)
	}

	// InstallationID is a non-secret ledger handle — keep it deterministic
	// so the integrations.installation_id column has something readable for
	// ops without leaking the webhook URL. Use a short prefix of the org id
	// so the value stays globally unique without exposing the org uuid.
	handle := fmt.Sprintf("slack-%s", shortOrg(princ.OrgID))
	c := domain.Connection{
		Provider:       domain.IntegrationSlack,
		Status:         domain.StatusConnected,
		InstallationID: handle,
		Metadata:       map[string]any{"channel_name": channel},
	}
	if err := p.repo.Upsert(ctx, princ.OrgID, c, encURL); err != nil {
		return domain.Connection{}, err
	}
	return c, nil
}

// Disconnect removes the (org, slack) integration row.
func (p *Provider) Disconnect(ctx context.Context, princ domain.Principal) error {
	return p.repo.Delete(ctx, princ.OrgID, domain.IntegrationSlack)
}

// Status returns the current Connection (no secret bytes). Returns
// domain.ErrNotFound if no row matches.
func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
	c, _, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationSlack)
	return c, err
}

// HandleWebhook intentionally rejects: Slack outbound integrations don't
// have an incoming webhook in Phase 6. Future phases can add slash-command
// handling here.
func (p *Provider) HandleWebhook(_ context.Context, _ string, _ map[string]string, _ []byte) error {
	return errors.New("slack: incoming webhooks not supported in phase 6")
}

// SendBlock posts a Block Kit message to the org's configured webhook URL.
// Decrypts the stored ciphertext through KeyVault — the plaintext URL is
// alive only for the duration of the HTTP call. Returns the HTTP status
// code and (on non-2xx) a descriptive error.
//
// Used by internal/adapter/notifier/slack.go from the multi-fanout notifier.
func (p *Provider) SendBlock(ctx context.Context, orgID string, blocks []map[string]any) (int, error) {
	_, encURL, err := p.repo.Get(ctx, orgID, domain.IntegrationSlack)
	if err != nil {
		return 0, err
	}
	if len(encURL) == 0 {
		return 0, errors.New("slack: no webhook URL on file")
	}
	urlB, err := p.kv.Decrypt(ctx, encURL)
	if err != nil {
		return 0, fmt.Errorf("slack: decrypt webhook: %w", err)
	}
	return p.postRaw(ctx, string(urlB), map[string]any{"blocks": blocks})
}

// postRaw is the shared HTTP POST helper. Returns the response status
// integer + any error. 2xx is success; everything else surfaces as an
// "http <code>" error string.
func (p *Provider) postRaw(ctx context.Context, url string, body map[string]any) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("slack: marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("slack: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("slack: do: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 != 2 {
		return resp.StatusCode, fmt.Errorf("slack: http %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// shortOrg returns the first 8 chars of an org id (or the full string if
// shorter). Used to build a non-secret ledger handle for installation_id.
func shortOrg(orgID string) string {
	if len(orgID) > 8 {
		return orgID[:8]
	}
	return orgID
}

// compile-time conformance check
var _ domain.Integration = (*Provider)(nil)
