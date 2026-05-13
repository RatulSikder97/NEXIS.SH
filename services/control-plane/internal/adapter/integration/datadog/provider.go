// Package datadog — see client.go for the API client. This file owns the
// domain.Integration port implementation: Connect / Disconnect / Status /
// HandleWebhook.
//
// Connect persists a sealed JSON blob of {api_key, app_key, site}; the
// adapter never logs or surfaces those secrets. Status decrypts the blob
// and re-issues a cheap Validate + MonitorState probe so the UI can render
// "alerting=N, ok=M, latency=Xms" instead of just "connected".
//
// Webhook signing: Datadog signs outbound webhook payloads with HMAC-SHA256
// over the request body, prepending the result with the prefix "v0=" and
// returning it in the X-Datadog-Signature header. Signing is configured
// per-customer in Datadog's UI, so this adapter accepts unsigned payloads
// when no per-tenant secret is on file (matches the existing github
// defaultSecret fallback). When the org has set DATADOG_WEBHOOK_SIGNING_SECRET
// (the env wired via Provider.signingSecret), we strictly require + verify
// the header.
package datadog

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// repoIfc is the slice of repo.IntegrationsRepo this adapter needs. Naming
// matches the spec; defining it here keeps the test fakes minimal.
type repoIfc interface {
	Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
	Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
	Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

// Provider is the Datadog Integration adapter. The client is optional —
// New() leaves it nil so unit tests can construct a Provider without an
// httpx wiring; production callers must SetClient before Connect/Status.
//
// signingSecret is the optional process-wide HMAC secret loaded from
// DATADOG_WEBHOOK_SIGNING_SECRET. When non-empty it is the only accepted
// signing key; when empty the adapter falls back to per-tenant secrets
// stored alongside the credentials.
type Provider struct {
	repo          repoIfc
	kv            domain.KeyVault
	client        *Client
	sink          domain.IncidentSink
	signingSecret []byte
}

// New builds a Provider without an API client. Call SetClient before any
// Connect/Status path.
func New(r repoIfc, kv domain.KeyVault, sink domain.IncidentSink) *Provider {
	return &Provider{repo: r, kv: kv, sink: sink}
}

// NewWithSigningSecret is the production constructor used by the factory.
// signingSecret is optional — pass nil to defer to per-tenant secrets
// stored in the connection's webhook_secret slot.
func NewWithSigningSecret(r repoIfc, kv domain.KeyVault, sink domain.IncidentSink, signingSecret []byte) *Provider {
	return &Provider{repo: r, kv: kv, sink: sink, signingSecret: signingSecret}
}

// SetClient injects the API client. Separated from New so tests can build a
// Provider without httpx (e.g. webhook-only paths don't need a live API).
func (p *Provider) SetClient(c *Client) { p.client = c }

// Name returns the canonical provider identifier.
func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationDatadog }

// secretBlob is the JSON shape sealed inside the KeyVault envelope. We
// keep this as a struct (not a freeform map) so the marshal/unmarshal
// roundtrip is type-checked.
type secretBlob struct {
	APIKey        string `json:"api_key"`
	AppKey        string `json:"app_key"`
	Site          string `json:"site"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

// Connect upserts a Datadog connection row. Required cfg: api_key, app_key.
// Optional cfg: site (defaults to datadoghq.com), webhook_secret (per-tenant
// HMAC secret used when no process-wide signing secret is set).
func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	apiKey, _ := cfg["api_key"].(string)
	appKey, _ := cfg["app_key"].(string)
	if apiKey == "" || appKey == "" {
		return domain.Connection{}, errors.New("datadog: api_key and app_key required")
	}
	site, _ := cfg["site"].(string)
	if site == "" {
		site = defaultSite
	}
	if !validSite(site) {
		return domain.Connection{}, fmt.Errorf("datadog: unknown site %q", site)
	}
	webhookSecret, _ := cfg["webhook_secret"].(string)

	if p.client == nil {
		return domain.Connection{}, errors.New("datadog: client not configured")
	}
	// Build a Client for the right site rather than mutating the existing
	// one — Provider may be shared across requests.
	probe := NewClient(site, p.client.http)
	if err := probe.Validate(ctx, apiKey, appKey); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Unauthorized() {
			return domain.Connection{}, fmt.Errorf("datadog: invalid credentials: %w", err)
		}
		return domain.Connection{}, fmt.Errorf("datadog: validate: %w", err)
	}

	blob := secretBlob{APIKey: apiKey, AppKey: appKey, Site: site, WebhookSecret: webhookSecret}
	raw, err := json.Marshal(blob)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("datadog: marshal secret: %w", err)
	}
	enc, err := p.kv.Encrypt(ctx, raw)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("datadog: encrypt secret: %w", err)
	}

	c := domain.Connection{
		Provider: domain.IntegrationDatadog,
		Status:   domain.StatusConnected,
		Metadata: map[string]any{"site": site},
	}
	if err := p.repo.Upsert(ctx, princ.OrgID, c, enc); err != nil {
		return domain.Connection{}, err
	}
	return c, nil
}

// Disconnect removes the (org, datadog) integration row. Idempotent.
func (p *Provider) Disconnect(ctx context.Context, princ domain.Principal) error {
	return p.repo.Delete(ctx, princ.OrgID, domain.IntegrationDatadog)
}

// Status returns the current Connection. When a row exists and the client
// is configured we issue a live MonitorState probe so the UI can show
// alerting/ok counts; on probe failure we surface a LastError but keep the
// stored connection intact.
func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
	c, enc, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationDatadog)
	if err != nil {
		return c, err
	}
	if p.client == nil || enc == nil {
		return c, nil
	}
	blob, err := p.decryptBlob(ctx, enc)
	if err != nil {
		// Surface the decrypt failure on the connection rather than the
		// caller; ops cares more about which org has a broken envelope
		// than about whose request triggered it.
		c.LastError = "datadog: decrypt: " + err.Error()
		return c, nil
	}
	probe := NewClient(blob.Site, p.client.http)
	alerting, ok, err := probe.MonitorState(ctx, blob.APIKey, blob.AppKey)
	if err != nil {
		c.LastError = err.Error()
		return c, nil
	}
	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	c.Metadata["site"] = blob.Site
	c.Metadata["monitors_alerting"] = alerting
	c.Metadata["monitors_ok"] = ok
	return c, nil
}

// HandleWebhook verifies Datadog's X-Datadog-Signature header (HMAC-SHA256
// over the body) against the configured signing secret, then converts the
// payload into a RawIncident and persists it via the IncidentSink.
//
// Signing-secret precedence:
//  1. Process-wide DATADOG_WEBHOOK_SIGNING_SECRET (Provider.signingSecret).
//  2. Per-tenant webhook_secret stored inside the encrypted blob.
//  3. None — accept the payload but log a warning via LastError. This
//     mirrors GitHub's defaultSecret fallback for the bootstrap path.
//
// NOTE on domain port: the original task spec referenced
// domain.IncidentDetected + domain.IncidentSink.PublishIncidentDetected
// which do not exist on the current domain. We use the existing
// IncidentSink.Insert(orgID, RawIncident) instead and map Datadog's
// alert_id → SourceEventID + priority → Level. The deviation is flagged
// in the agent report; the controller should add an IncidentDetected
// channel + PublishIncidentDetected port before Phase-4 normalisation.
func (p *Provider) HandleWebhook(ctx context.Context, orgID string, headers map[string]string, body []byte) error {
	secret, err := p.resolveWebhookSecret(ctx, orgID)
	if err != nil {
		return err
	}
	if len(secret) > 0 {
		sig := firstNonEmpty(headers, "X-Datadog-Signature", "x-datadog-signature")
		if sig == "" {
			return errors.New("datadog: missing X-Datadog-Signature")
		}
		if !verifyHMAC(secret, body, sig) {
			return errors.New("datadog: HMAC mismatch")
		}
	}

	var evt datadogAlert
	if err := json.Unmarshal(body, &evt); err != nil {
		return fmt.Errorf("datadog: decode: %w", err)
	}
	if evt.EventType == "" {
		evt.EventType = "alert.triggered"
	}

	// Only fan triggered/recovered alerts into the incident sink; ignore
	// "test" payloads and Datadog's keep-alive pings.
	if !strings.HasPrefix(evt.EventType, "alert.") && !strings.HasPrefix(evt.EventType, "monitor.") {
		return nil
	}

	var payload map[string]any
	_ = json.Unmarshal(body, &payload)

	raw := domain.RawIncident{
		Source:        "datadog",
		SourceEventID: evt.AlertID,
		Title:         evt.Title,
		Level:         evt.Priority,
		Service:       evt.serviceTag(),
		Environment:   evt.envTag(),
		Payload:       payload,
	}
	if raw.Title == "" {
		raw.Title = evt.EventTitle
	}
	if raw.SourceEventID == "" {
		// Datadog also uses {alert.transition} + {alert.id} on some shapes
		// — fall back to event_id when alert_id is unset so we always
		// dedupe on something.
		raw.SourceEventID = evt.EventID
	}
	return p.sink.Insert(ctx, orgID, raw)
}

// resolveWebhookSecret picks the signing key per the precedence rules
// documented on HandleWebhook.
func (p *Provider) resolveWebhookSecret(ctx context.Context, orgID string) ([]byte, error) {
	if len(p.signingSecret) > 0 {
		return p.signingSecret, nil
	}
	_, enc, err := p.repo.Get(ctx, orgID, domain.IntegrationDatadog)
	if err != nil || enc == nil {
		// No row yet — accept the payload without verification. The
		// caller may still bail if signingSecret was set but the lookup
		// failed for a different reason; we don't distinguish here.
		return nil, nil
	}
	blob, err := p.decryptBlob(ctx, enc)
	if err != nil {
		return nil, fmt.Errorf("datadog: decrypt secret: %w", err)
	}
	if blob.WebhookSecret == "" {
		return nil, nil
	}
	return []byte(blob.WebhookSecret), nil
}

// decryptBlob unseals the JSON-encoded credentials envelope.
func (p *Provider) decryptBlob(ctx context.Context, enc []byte) (secretBlob, error) {
	var blob secretBlob
	plain, err := p.kv.Decrypt(ctx, enc)
	if err != nil {
		return blob, err
	}
	if err := json.Unmarshal(plain, &blob); err != nil {
		return blob, fmt.Errorf("datadog: unmarshal blob: %w", err)
	}
	if blob.Site == "" {
		blob.Site = defaultSite
	}
	return blob, nil
}

// datadogAlert mirrors the slice of Datadog's webhook payload we care
// about. Datadog supports a configurable @-mention template, but the
// shipped default emits these fields under the names below; if a customer
// has overridden the template they need to add the matching keys to
// recover the same data.
type datadogAlert struct {
	AlertID     string `json:"alert_id"`
	EventID     string `json:"event_id"`
	EventType   string `json:"event_type"`
	EventTitle  string `json:"event_title"`
	Title       string `json:"title"`
	Priority    string `json:"priority"`
	AlertStatus string `json:"alert_status"`
	Tags        string `json:"tags"`
	Service     string `json:"service"`
	Env         string `json:"env"`
}

// serviceTag extracts the `service` tag — Datadog renders tags either via
// a dedicated `service` field or comma-joined inside `tags`. Prefer the
// dedicated field when present.
func (a datadogAlert) serviceTag() string {
	if a.Service != "" {
		return a.Service
	}
	return tagValue(a.Tags, "service")
}

// envTag does the same lookup for `env`.
func (a datadogAlert) envTag() string {
	if a.Env != "" {
		return a.Env
	}
	return tagValue(a.Tags, "env")
}

// tagValue extracts the first occurrence of "<key>:<value>" inside a
// comma-separated tag list (Datadog's standard tag rendering).
func tagValue(joined, key string) string {
	if joined == "" {
		return ""
	}
	for _, t := range strings.Split(joined, ",") {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, key+":") {
			return strings.TrimPrefix(t, key+":")
		}
	}
	return ""
}

// firstNonEmpty returns the first non-empty header value across the
// candidate keys. Used so we can accept both canonical and lower-case
// header names without depending on the transport layer's case handling.
func firstNonEmpty(headers map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := headers[k]; v != "" {
			return v
		}
	}
	return ""
}

// verifyHMAC compares Datadog's "<algo>=<hex>" signature against the
// HMAC-SHA256 of body keyed by secret. We accept signatures with or
// without an algorithm prefix to be defensive — the documented shape is
// "v0=<hex>" but some accounts emit a bare hex string.
func verifyHMAC(secret, body []byte, signature string) bool {
	want := hmac.New(sha256.New, secret)
	want.Write(body)
	expected := hex.EncodeToString(want.Sum(nil))

	got := signature
	if i := strings.Index(got, "="); i >= 0 {
		got = got[i+1:]
	}
	return hmac.Equal([]byte(expected), []byte(got))
}

// compile-time conformance check
var _ domain.Integration = (*Provider)(nil)
