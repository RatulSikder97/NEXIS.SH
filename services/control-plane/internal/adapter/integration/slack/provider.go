// Package slack implements the Slack IntegrationProvider. Phase 6 introduced
// outbound-only webhook delivery; the "real-integrations" work plan promotes
// Slack to a full OAuth v2 install + interactivity flow:
//
//   - Connect supports three flavours:
//
//   - {webhook_url, channel_name} — legacy incoming-webhook flow (kept for
//     existing tenants who connected before OAuth landed). Stored secret
//     is the encrypted webhook URL.
//
//   - {code, state}               — OAuth v2 callback. We exchange the code
//     for a bot token via oauth.v2.access and store the encrypted bot
//     token + team metadata.
//
//   - {bot_token}                 — manual entry (typically used by ops
//     during incident response). We validate with auth.test before
//     persisting.
//
//   - Status probes the stored credential with auth.test (bot-token flows) or
//     a no-op for the webhook flow, surfacing team_id/team_name/bot_user_id
//     in metadata + a latency_ms field for the UI health pill.
//
//   - SendBlock continues to be the Approval Gate's notifier hook: for the
//     bot-token flow it posts via chat.postMessage to the configured channel;
//     for the legacy webhook flow it falls back to the incoming-webhook URL.
//
//   - DMUserByEmail is the new high-severity escalation path: resolve email
//     → user id → IM channel → chat.postMessage with a Block Kit approval
//     prompt. Requires the bot-token flow.
//
// Storage layout: the encrypted "secret" column holds either the webhook URL
// (legacy) or the bot token (OAuth/manual). The Metadata map carries
// {auth_mode: "webhook"|"bot", channel_name, channel_id, team_id, team_name,
// bot_user_id} so Status + SendBlock can branch without re-decrypting.
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

// Repo is the slice of repo.IntegrationsRepo this adapter needs. Defined here
// so tests can substitute a fake without depending on pgx.
type Repo interface {
	Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
	Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
	Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

// HTTPDoer is the minimal *http.Client surface the legacy webhook path uses
// (the OAuth + bot-token paths go through *httpx.Client + slack.Client
// instead). Kept around so the existing provider_test.go continues to compile
// without a behaviour change.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Provider is the Slack Integration adapter.
type Provider struct {
	repo   Repo
	kv     domain.KeyVault
	client *Client     // typed Slack Web API client; nil disables OAuth/bot-token flows
	oauth  OAuthConfig // Slack-app OAuth credentials; zero values disable ExchangeCode
	legacy HTTPDoer    // legacy webhook-URL HTTP client; used only when client == nil
}

// New constructs a Provider with a legacy 10-second *http.Client. The Slack
// Web API client is left nil — callers who need OAuth/bot-token flows must
// use NewWithClient + NewWithOAuth instead.
//
// Kept for backwards compatibility with the existing factory wire.
func New(r Repo, kv domain.KeyVault) *Provider {
	return &Provider{
		repo:   r,
		kv:     kv,
		legacy: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewWithClient lets tests inject a recording HTTPDoer for the legacy webhook
// path. The Web API client is left nil.
func NewWithClient(r Repo, kv domain.KeyVault, c HTTPDoer) *Provider {
	return &Provider{repo: r, kv: kv, legacy: c}
}

// NewWithOAuth wires the typed Slack Web API client and OAuth credentials.
// This is the production constructor for the OAuth v2 install flow; the
// legacy webhook HTTPDoer is set to a default *http.Client so existing
// webhook tenants continue to work unchanged.
//
// Either client or legacy may be nil; passing nil for one disables the
// corresponding code path. Tests typically pass both — the legacy doer is
// kept as the recording stub, and the Web API client points at an
// httptest.Server via NewClientWithBaseURL.
func NewWithOAuth(r Repo, kv domain.KeyVault, client *Client, oauth OAuthConfig) *Provider {
	return &Provider{
		repo:   r,
		kv:     kv,
		client: client,
		oauth:  oauth,
		legacy: &http.Client{Timeout: 10 * time.Second},
	}
}

// SetLegacyHTTPClient is the constructor-shim tests use to wire both the
// typed client and a recording legacy doer in one Provider instance. Real
// callers should not depend on it.
func (p *Provider) SetLegacyHTTPClient(c HTTPDoer) { p.legacy = c }

// Name returns the canonical provider identifier.
func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationSlack }

// Connect routes the supplied config to one of three install flavours:
//
//  1. code + state present  → OAuth v2 callback (ExchangeCode → bot token).
//  2. bot_token present     → manual bot-token entry (auth.test validation).
//  3. webhook_url present   → legacy incoming-webhook flow.
//
// Exactly one MUST match; the function returns an error if the config
// matches none. The encrypted secret persisted is either the bot token (1+2)
// or the webhook URL (3). Metadata always carries an "auth_mode" key so the
// downstream Status/SendBlock paths can branch without re-decrypting.
func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	if code, _ := cfg["code"].(string); code != "" {
		return p.connectOAuth(ctx, princ, cfg)
	}
	if token, _ := cfg["bot_token"].(string); token != "" {
		return p.connectBotToken(ctx, princ, cfg)
	}
	if url, _ := cfg["webhook_url"].(string); url != "" {
		return p.connectWebhook(ctx, princ, cfg)
	}
	// Error string deliberately mentions webhook_url first so the historical
	// "webhook_url required" assertion in provider_test.go keeps matching;
	// the broader list of accepted keys is on the same line for operators.
	return domain.Connection{}, errors.New("slack: webhook_url required (or one of {code, bot_token})")
}

// connectOAuth handles the OAuth v2 callback path. state is consumed by the
// HTTP handler (against a signed cookie) before this is called — we don't
// verify state here, that's the transport layer's job.
func (p *Provider) connectOAuth(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	code, _ := cfg["code"].(string)
	if p.client == nil {
		return domain.Connection{}, errors.New("slack: oauth client not configured")
	}
	res, err := p.ExchangeCode(ctx, code)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("slack: oauth exchange: %w", err)
	}
	channelName, _ := cfg["channel_name"].(string)
	channelID, _ := cfg["channel_id"].(string)

	encTok, err := p.kv.Encrypt(ctx, []byte(res.BotToken))
	if err != nil {
		return domain.Connection{}, fmt.Errorf("slack: encrypt bot token: %w", err)
	}
	c := domain.Connection{
		Provider:       domain.IntegrationSlack,
		Status:         domain.StatusConnected,
		InstallationID: fmt.Sprintf("slack-%s", res.TeamID),
		Metadata: map[string]any{
			"auth_mode":    "bot",
			"channel_name": channelName,
			"channel_id":   channelID,
			"team_id":      res.TeamID,
			"team_name":    res.TeamName,
			"bot_user_id":  res.BotUserID,
			"scope":        res.Scope,
		},
	}
	if err := p.repo.Upsert(ctx, princ.OrgID, c, encTok); err != nil {
		return domain.Connection{}, err
	}
	return c, nil
}

// connectBotToken handles the manual-entry flow. We auth.test the supplied
// token before persisting so a typo fails Connect rather than silently
// breaking the next notifier fire.
func (p *Provider) connectBotToken(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	token, _ := cfg["bot_token"].(string)
	if p.client == nil {
		return domain.Connection{}, errors.New("slack: web api client not configured")
	}
	info, err := p.client.AuthTest(ctx, token)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("slack: validate bot token: %w", err)
	}
	channelName, _ := cfg["channel_name"].(string)
	channelID, _ := cfg["channel_id"].(string)

	encTok, err := p.kv.Encrypt(ctx, []byte(token))
	if err != nil {
		return domain.Connection{}, fmt.Errorf("slack: encrypt bot token: %w", err)
	}
	c := domain.Connection{
		Provider:       domain.IntegrationSlack,
		Status:         domain.StatusConnected,
		InstallationID: fmt.Sprintf("slack-%s", info.TeamID),
		Metadata: map[string]any{
			"auth_mode":    "bot",
			"channel_name": channelName,
			"channel_id":   channelID,
			"team_id":      info.TeamID,
			"team_name":    info.TeamName,
			"bot_user_id":  info.BotUserID,
		},
	}
	if err := p.repo.Upsert(ctx, princ.OrgID, c, encTok); err != nil {
		return domain.Connection{}, err
	}
	return c, nil
}

// connectWebhook is the legacy incoming-webhook flow. Kept for backwards
// compatibility with existing tenants and existing tests; the encrypted
// secret is the webhook URL, and Metadata carries auth_mode="webhook" so
// SendBlock falls back to direct POST.
func (p *Provider) connectWebhook(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
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
	handle := fmt.Sprintf("slack-%s", shortOrg(princ.OrgID))
	c := domain.Connection{
		Provider:       domain.IntegrationSlack,
		Status:         domain.StatusConnected,
		InstallationID: handle,
		Metadata: map[string]any{
			"auth_mode":    "webhook",
			"channel_name": channel,
		},
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

// Status returns the current Connection. For bot-token flows it probes
// auth.test and surfaces latency_ms + team metadata on the Connection's
// metadata; for legacy webhook flows it returns the stored row unchanged
// (the webhook URL is not probeable — Slack 200s on any POST).
//
// A probe failure does NOT remove the row — instead the Connection is
// returned with Status=domain.StatusError + LastError populated, mirroring
// the GitHub/Sentry adapters' degraded-but-not-disconnected semantics.
func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
	c, secret, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationSlack)
	if err != nil {
		return c, err
	}
	mode, _ := c.Metadata["auth_mode"].(string)
	if mode != "bot" {
		// Legacy webhook tenants: nothing to probe; return what we have.
		return c, nil
	}
	if p.client == nil || p.kv == nil {
		return c, nil
	}
	tokenB, err := p.kv.Decrypt(ctx, secret)
	if err != nil {
		c.Status = domain.StatusError
		c.LastError = fmt.Sprintf("decrypt bot token: %v", err)
		return c, nil
	}
	start := time.Now()
	info, err := p.client.AuthTest(ctx, string(tokenB))
	latency := time.Since(start)
	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	c.Metadata["latency_ms"] = latency.Milliseconds()
	if err != nil {
		c.Status = domain.StatusError
		c.LastError = err.Error()
		return c, nil
	}
	c.Metadata["team_id"] = info.TeamID
	c.Metadata["team_name"] = info.TeamName
	c.Metadata["bot_user_id"] = info.BotUserID
	c.Status = domain.StatusConnected
	c.LastError = ""
	return c, nil
}

// HandleWebhook intentionally rejects: Slack outbound integrations don't have
// an incoming webhook in Phase 6. Future phases can add slash-command
// handling here; interactivity (button clicks) is dispatched through the
// dedicated /v1/integrations/slack/interactivity endpoint, not through this
// catch-all.
func (p *Provider) HandleWebhook(_ context.Context, _ string, _ map[string]string, _ []byte) error {
	return errors.New("slack: incoming webhooks not supported in phase 6")
}

// SendBlock posts a Block Kit message to the org's stored Slack target. For
// bot-token tenants the message goes through chat.postMessage to the
// configured channel_id (or channel_name fallback); for legacy webhook
// tenants the message is POSTed to the encrypted webhook URL.
//
// Returns the HTTP status code on success (200 from Slack); on error returns
// the surfaced status (or 0 for non-HTTP errors).
//
// Used by internal/adapter/notifier/slack.go from the multi-fanout notifier.
func (p *Provider) SendBlock(ctx context.Context, orgID string, blocks []map[string]any) (int, error) {
	c, secret, err := p.repo.Get(ctx, orgID, domain.IntegrationSlack)
	if err != nil {
		return 0, err
	}
	if len(secret) == 0 {
		return 0, errors.New("slack: no credential on file")
	}
	mode, _ := c.Metadata["auth_mode"].(string)
	plain, err := p.kv.Decrypt(ctx, secret)
	if err != nil {
		return 0, fmt.Errorf("slack: decrypt: %w", err)
	}

	if mode == "bot" {
		if p.client == nil {
			return 0, errors.New("slack: web api client not configured")
		}
		channel, _ := c.Metadata["channel_id"].(string)
		if channel == "" {
			channel, _ = c.Metadata["channel_name"].(string)
		}
		if channel == "" {
			return 0, errors.New("slack: channel_id or channel_name required for bot mode")
		}
		if _, err := p.client.PostMessage(ctx, string(plain), channel, blocks); err != nil {
			return slackErrStatus(err), err
		}
		return http.StatusOK, nil
	}

	// Legacy: plain is the webhook URL.
	return p.postRaw(ctx, string(plain), map[string]any{"blocks": blocks})
}

// DMUserByEmail posts a Block Kit message to a user identified by their
// workspace email. The flow is:
//
//  1. users.lookupByEmail → resolves email to a Slack user id.
//  2. conversations.open  → returns the IM channel id for that user.
//  3. chat.postMessage    → delivers the blocks to that IM channel.
//
// Returns the message timestamp from step 3 on success. The Approval Gate
// uses this for high-severity human-required incidents — the approver's
// e-mail is read from PagerDuty's on-call API.
//
// REQUIRED scopes on the bot token: users:read.email, im:write, chat:write.
// Missing any of these surfaces as a typed *APIError from the underlying
// client call.
func (p *Provider) DMUserByEmail(ctx context.Context, princ domain.Principal, email string, blocks []map[string]any) (string, error) {
	if email == "" {
		return "", errors.New("slack: email required")
	}
	if p.client == nil {
		return "", errors.New("slack: web api client not configured")
	}
	_, secret, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationSlack)
	if err != nil {
		return "", err
	}
	if len(secret) == 0 {
		return "", errors.New("slack: no credential on file")
	}
	tokenB, err := p.kv.Decrypt(ctx, secret)
	if err != nil {
		return "", fmt.Errorf("slack: decrypt: %w", err)
	}
	token := string(tokenB)

	userID, err := p.client.LookupUserByEmail(ctx, token, email)
	if err != nil {
		return "", fmt.Errorf("slack: lookup user %s: %w", email, err)
	}
	channelID, err := p.client.OpenIM(ctx, token, userID)
	if err != nil {
		return "", fmt.Errorf("slack: open im for %s: %w", userID, err)
	}
	ts, err := p.client.PostMessage(ctx, token, channelID, blocks)
	if err != nil {
		return "", fmt.Errorf("slack: dm %s: %w", userID, err)
	}
	return ts, nil
}

// postRaw is the legacy POST helper for the incoming-webhook flow. Returns
// the response status integer + any error. 2xx is success; everything else
// surfaces as "http <code>".
func (p *Provider) postRaw(ctx context.Context, url string, body map[string]any) (int, error) {
	if p.legacy == nil {
		return 0, errors.New("slack: legacy http client not configured")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("slack: marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("slack: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.legacy.Do(req)
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

// slackErrStatus extracts the HTTP status from a Slack client error, falling
// back to 0 for non-HTTP errors (network failure, ctx cancel). Used by
// SendBlock to keep its (status, error) return shape symmetrical regardless
// of which code path the error came from.
func slackErrStatus(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
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
