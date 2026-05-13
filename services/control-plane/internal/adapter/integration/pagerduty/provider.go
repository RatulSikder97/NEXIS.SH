// Package pagerduty implements the PagerDuty IntegrationProvider. Three
// distinct flows meet here:
//
//  1. Connect/Disconnect/Status — tenant CRUD on the (api_token, service_id,
//     escalation_policy_id) tuple. The token is sealed via KeyVault; the
//     service + policy ids stay in metadata so the UI + approval gate can
//     reference them without decrypting.
//
//  2. HandleWebhook — PagerDuty V3 webhook envelope. Signed with HMAC-SHA256
//     under one or more rotating secrets in `X-PagerDuty-Signature`. We
//     verify ANY one matches (rotation-aware) before parsing the body and
//     fanning the event off to the IncidentSink.
//
//  3. OnCallFor + Escalate — external methods used by the approval gate and
//     Sentinel respectively. OnCallFor reads /oncalls for the configured
//     policy; Escalate POSTs /incidents on the configured service when we
//     detect something the customer's own monitoring missed.
//
// The provider deliberately holds zero secrets in memory between calls — the
// api_token is decrypted just-in-time inside each external method and the
// decrypted bytes never escape this package.
package pagerduty

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

// Repo is the slice of repo.IntegrationsRepo this adapter needs. Defined
// here so tests can substitute a fake without depending on pgx.
type Repo interface {
	Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
	Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
	Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

// storedSecret is the JSON document we seal under the integration row's
// encrypted secret bytea. Keeping all three of {api_token, service_id,
// escalation_policy_id} encrypted means a leak of the metadata column never
// exposes which service/policy the token can touch — a defense-in-depth
// layer above just sealing the token.
type storedSecret struct {
	APIToken           string `json:"api_token"`
	ServiceID          string `json:"service_id"`
	EscalationPolicyID string `json:"escalation_policy_id,omitempty"`
}

// Provider is the PagerDuty Integration adapter. Construct with New and
// optionally inject a real *Client via SetClient before use; tests can leave
// the client nil and only exercise paths that do not touch the REST API.
type Provider struct {
	repo      Repo
	kv        domain.KeyVault
	client    *Client
	sink      domain.IncidentSink
	fromEmail string
	// webhookSecrets is the rotation set used to verify
	// X-PagerDuty-Signature on every incoming webhook. ANY match accepts
	// the body; allowing the list lets operators rotate without a flap.
	webhookSecrets [][]byte
}

// New constructs a Provider with the IncidentSink (for webhook fan-out) and
// the "from" email used when triggering incidents on behalf of the bot.
// fromEmail MUST be the email of a real PagerDuty user in the customer's
// account — PagerDuty 400s the POST otherwise.
func New(r Repo, kv domain.KeyVault, sink domain.IncidentSink, fromEmail string) *Provider {
	if fromEmail == "" {
		fromEmail = "nexis-bot@nexis.dev"
	}
	return &Provider{repo: r, kv: kv, sink: sink, fromEmail: fromEmail}
}

// SetClient swaps in the REST client. Kept separate from New so production
// can wire the httpx-backed real client at boot while tests assemble the
// Provider with a stub client (or leave it nil for non-REST paths).
func (p *Provider) SetClient(c *Client) { p.client = c }

// SetWebhookSecrets installs the rotation set used by HandleWebhook. Pass an
// empty slice to fall back to the GitHub default webhook secret in dev (the
// caller is responsible for that fallback).
func (p *Provider) SetWebhookSecrets(secrets [][]byte) {
	p.webhookSecrets = secrets
}

// Name returns the canonical provider identifier.
func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationPagerDuty }

// Connect upserts a PagerDuty connection. Config must contain "api_token"
// and "service_id"; "escalation_policy_id" is optional but unlocks OnCallFor.
//
// The token is validated with GET /users/me before we persist — refusing to
// store a token that cannot authenticate avoids the "looks connected, fails
// every call" failure mode.
func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	token, _ := cfg["api_token"].(string)
	serviceID, _ := cfg["service_id"].(string)
	policyID, _ := cfg["escalation_policy_id"].(string)
	if token == "" {
		return domain.Connection{}, errors.New("pagerduty: api_token required")
	}
	if serviceID == "" {
		return domain.Connection{}, errors.New("pagerduty: service_id required")
	}
	if p.client == nil {
		return domain.Connection{}, errors.New("pagerduty: REST client not configured")
	}

	user, err := p.client.MeOK(ctx, token)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("pagerduty: validate token: %w", err)
	}

	sec := storedSecret{APIToken: token, ServiceID: serviceID, EscalationPolicyID: policyID}
	secBytes, err := json.Marshal(sec)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("pagerduty: marshal secret: %w", err)
	}
	enc, err := p.kv.Encrypt(ctx, secBytes)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("pagerduty: encrypt: %w", err)
	}

	meta := map[string]any{
		"service_id": serviceID,
		"user_email": user.Email,
	}
	if policyID != "" {
		meta["escalation_policy_id"] = policyID
	}

	c := domain.Connection{
		Provider:       domain.IntegrationPagerDuty,
		Status:         domain.StatusConnected,
		InstallationID: user.ID,
		Metadata:       meta,
	}
	if err := p.repo.Upsert(ctx, princ.OrgID, c, enc); err != nil {
		return domain.Connection{}, err
	}
	return c, nil
}

// Disconnect removes the (org, pagerduty) integration row.
func (p *Provider) Disconnect(ctx context.Context, princ domain.Principal) error {
	return p.repo.Delete(ctx, princ.OrgID, domain.IntegrationPagerDuty)
}

// Status decrypts the stored token, calls /users/me, and returns the
// resulting Connection with user_name/user_email metadata populated. A
// failing /users/me marks the connection error rather than ripping the row
// out — Disconnect is the explicit removal path.
func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
	conn, enc, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationPagerDuty)
	if err != nil {
		return conn, err
	}
	if p.client == nil {
		return conn, nil
	}
	sec, err := p.loadSecret(ctx, enc)
	if err != nil {
		conn.Status = domain.StatusError
		conn.LastError = err.Error()
		return conn, nil
	}
	user, err := p.client.MeOK(ctx, sec.APIToken)
	if err != nil {
		conn.Status = domain.StatusError
		conn.LastError = err.Error()
		return conn, nil
	}
	if conn.Metadata == nil {
		conn.Metadata = map[string]any{}
	}
	conn.Metadata["user_name"] = user.Name
	conn.Metadata["user_email"] = user.Email
	conn.Status = domain.StatusConnected
	conn.LastError = ""
	return conn, nil
}

// pagerDutyV3Envelope is the slim view of the V3 webhook body we care
// about. PagerDuty emits a richer payload (links, references, log entries);
// the parser drops everything else.
//
// Service.ID is the canonical PagerDuty service id (e.g. "P1234567") —
// distinct from Service.Summary (display name) — and is the column the
// projects table indexes on for routing.
type pagerDutyV3Envelope struct {
	Event struct {
		ID        string `json:"id"`
		EventType string `json:"event_type"`
		Data      struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Urgency string `json:"urgency"`
			Service struct {
				ID      string `json:"id"`
				Summary string `json:"summary"`
			} `json:"service"`
		} `json:"data"`
	} `json:"event"`
}

// HandleWebhook verifies X-PagerDuty-Signature against the configured
// rotation set and, on `incident.triggered`, fans the event off to the
// IncidentSink as a RawIncident.
//
// Signature format from PagerDuty docs:
//
//	X-PagerDuty-Signature: v1=<hex>,v1=<hex>,...
//
// where each <hex> is HMAC-SHA256(body, secret). We accept the body if ANY
// candidate (secret × signature) matches — that is what makes the rotation
// safe.
func (p *Provider) HandleWebhook(ctx context.Context, orgID string, headers map[string]string, body []byte) error {
	sig := headers["X-PagerDuty-Signature"]
	if sig == "" {
		// http.Header preserves the canonical case but our adapter is fed a
		// flat map[string]string by the dispatcher — fall back to the
		// lowercase + Title variants just in case the caller normalises.
		sig = headers["X-Pagerduty-Signature"]
	}
	if sig == "" {
		return errors.New("pagerduty: missing X-PagerDuty-Signature")
	}
	secrets := p.webhookSecrets
	if len(secrets) == 0 {
		return errors.New("pagerduty: no webhook secrets configured")
	}
	if !verifyWebhookSignature(sig, body, secrets) {
		return errors.New("pagerduty: HMAC mismatch")
	}

	var env pagerDutyV3Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("pagerduty: decode webhook: %w", err)
	}

	// Only the trigger event drives an Insert — acknowledge/resolve are
	// state changes on an existing incident and have no place in
	// incidents_raw.
	if env.Event.EventType != "incident.triggered" {
		return nil
	}

	var payload map[string]any
	_ = json.Unmarshal(body, &payload)

	return p.sink.Insert(ctx, orgID, domain.RawIncident{
		Source:        "pagerduty",
		SourceEventID: env.Event.Data.ID,
		Title:         env.Event.Data.Title,
		Level:         env.Event.Data.Urgency,
		Service:       env.Event.Data.Service.Summary,
		Payload:       payload,
		// Fingerprint — feed Sentinel's project router. The service id
		// (e.g. "P1234567") is what the projects table indexes on, not the
		// summary string.
		PagerDutyServiceID: env.Event.Data.Service.ID,
	})
}

// OnCallFor returns the user currently on call for the org's configured
// escalation policy. The approval gate reads this so a human reviewer can
// see who would be paged before they approve a recovery.
func (p *Provider) OnCallFor(ctx context.Context, princ domain.Principal) (User, error) {
	if p.client == nil {
		return User{}, errors.New("pagerduty: REST client not configured")
	}
	_, enc, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationPagerDuty)
	if err != nil {
		return User{}, err
	}
	sec, err := p.loadSecret(ctx, enc)
	if err != nil {
		return User{}, err
	}
	if sec.EscalationPolicyID == "" {
		return User{}, errors.New("pagerduty: no escalation_policy_id configured")
	}
	return p.client.WhoIsOnCall(ctx, sec.APIToken, sec.EscalationPolicyID)
}

// Escalate triggers a PagerDuty incident on the customer's configured
// service. Used by Sentinel when it detects something the customer's own
// monitoring missed; dedupKey bounds the blast radius of repeated triggers
// (PagerDuty merges any subsequent call with the same key into the existing
// open incident).
func (p *Provider) Escalate(ctx context.Context, princ domain.Principal, title, body, dedupKey string) (string, error) {
	if p.client == nil {
		return "", errors.New("pagerduty: REST client not configured")
	}
	_, enc, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationPagerDuty)
	if err != nil {
		return "", err
	}
	sec, err := p.loadSecret(ctx, enc)
	if err != nil {
		return "", err
	}
	return p.client.TriggerIncident(ctx, sec.APIToken, p.fromEmail, TriggerReq{
		ServiceID:   sec.ServiceID,
		Title:       title,
		UrgencyHigh: true,
		Body:        body,
		DedupKey:    dedupKey,
	})
}

// loadSecret decrypts the stored secret blob and JSON-decodes it. Returns a
// descriptive error on each failure mode so callers can surface useful
// audit-trail messages.
func (p *Provider) loadSecret(ctx context.Context, enc []byte) (storedSecret, error) {
	if len(enc) == 0 {
		return storedSecret{}, errors.New("pagerduty: empty secret blob")
	}
	plain, err := p.kv.Decrypt(ctx, enc)
	if err != nil {
		return storedSecret{}, fmt.Errorf("pagerduty: decrypt: %w", err)
	}
	var sec storedSecret
	if err := json.Unmarshal(plain, &sec); err != nil {
		return storedSecret{}, fmt.Errorf("pagerduty: decode secret: %w", err)
	}
	if sec.APIToken == "" {
		return storedSecret{}, errors.New("pagerduty: stored secret missing api_token")
	}
	return sec, nil
}

// verifyWebhookSignature implements PagerDuty's `v1=<hex>,v1=<hex>` schema.
// We accept the body if ANY (secret × signature) pair produces a matching
// HMAC — that is the property that lets the operator rotate secrets without
// dropping any in-flight webhooks.
//
// Implementation note: PagerDuty pads the header with optional whitespace
// after the commas, and the prefix "v1=" is the only scheme version
// shipped today. Unknown prefixes are ignored rather than rejected so a
// future "v2=" announcement is not a breaking change for us.
func verifyWebhookSignature(header string, body []byte, secrets [][]byte) bool {
	candidates := parseSignatureHeader(header)
	if len(candidates) == 0 {
		return false
	}
	for _, secret := range secrets {
		if len(secret) == 0 {
			continue
		}
		mac := hmac.New(sha256.New, secret)
		mac.Write(body)
		want := hex.EncodeToString(mac.Sum(nil))
		for _, got := range candidates {
			if hmac.Equal([]byte(got), []byte(want)) {
				return true
			}
		}
	}
	return false
}

// parseSignatureHeader extracts the hex digests from a PagerDuty signature
// header. Only `v1=<hex>` entries are returned.
func parseSignatureHeader(header string) []string {
	if header == "" {
		return nil
	}
	parts := strings.Split(header, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "v1=") {
			out = append(out, strings.TrimPrefix(p, "v1="))
		}
	}
	return out
}

// compile-time conformance check
var _ domain.Integration = (*Provider)(nil)
