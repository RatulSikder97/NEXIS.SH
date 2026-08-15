// Package webhook implements the vendor-neutral incident intake.
//
// Every other incident source (Sentry, Datadog, PagerDuty) validates its
// credentials against the vendor's API on Connect, so an operator with no
// vendor account has no way to get a single row into incidents_raw — and
// because Sentinel only scans orgs returned by ConnectedIncidentOrgs, a
// deployment without one of those accounts never detects anything at all.
// That is the gap this provider closes: it lets any service post its own
// faults to NEXIS over a signed HTTP call.
//
// Connect mints (or accepts) a 32-byte signing secret and stores it
// encrypted. There is no external validation step, because there is no
// external system — the secret IS the trust boundary.
//
// Webhook contract:
//
//	POST /v1/webhooks/webhook/{org_id}
//	X-Nexis-Signature: sha256=<hex hmac of the raw body>
//	{
//	  "title":       "ZeroDivisionError: division by zero in unit_price",
//	  "service":     "checkout-api",          // optional, defaults "unknown"
//	  "environment": "production",            // optional, defaults "production"
//	  "level":       "fatal",                 // optional, defaults "fatal"
//	  "event_id":    "…",                     // optional, dedupe key
//	  "stacktrace":  "Traceback (most recent call last): …",
//	  "logs":        "ts=… level=error …",
//	  "metadata":    {"root_cause_node": "…", "endpoint": "/checkout"}
//	}
//
// `level: "fatal"` is what makes Sentinel's per-row rule fire immediately;
// anything else only contributes to the EWMA spike baseline.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// SignatureHeader carries the hex-encoded HMAC-SHA256 of the raw request
// body. The `sha256=` prefix is accepted and optional, matching the shape
// GitHub and Stripe use so an operator's muscle memory transfers.
const SignatureHeader = "X-Nexis-Signature"

// secretLen is the byte length of a generated signing secret before hex
// encoding — 32 bytes, the same strength as the OAuth state tokens.
const secretLen = 32

// repoIfc is the storage port. Mirrors the sentry adapter's so the factory
// can hand both the same *repo.IntegrationsRepo.
type repoIfc interface {
	Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
	Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
	Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

// Provider implements domain.Integration for the generic intake.
type Provider struct {
	repo repoIfc
	kv   domain.KeyVault
	sink domain.IncidentSink
}

// New builds the provider. sink is the incidents repo that persists each
// accepted event; a nil sink makes HandleWebhook a validating no-op, which
// is only useful in tests.
func New(r repoIfc, kv domain.KeyVault, sink domain.IncidentSink) *Provider {
	return &Provider{repo: r, kv: kv, sink: sink}
}

func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationWebhook }

// newSecret returns a hex-encoded random signing secret.
func newSecret() (string, error) {
	b := make([]byte, secretLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("webhook: rng: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Connect stores a signing secret for the caller's org. `signing_secret` in
// the config is honoured when present (so an operator can pin a secret their
// service already ships with); otherwise one is generated.
//
// The generated secret is returned in Connection.Metadata under
// "signing_secret" — this is the ONLY time it is readable, exactly like an
// API key. It is never echoed by Status.
func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	secret, _ := cfg["signing_secret"].(string)
	secret = strings.TrimSpace(secret)
	generated := false
	if secret == "" {
		s, err := newSecret()
		if err != nil {
			return domain.Connection{}, err
		}
		secret = s
		generated = true
	}
	if len(secret) < 16 {
		return domain.Connection{}, errors.New("webhook: signing_secret must be at least 16 characters")
	}

	label, _ := cfg["label"].(string)
	if label == "" {
		label = "Incident intake"
	}

	enc, err := p.kv.Encrypt(ctx, []byte(secret))
	if err != nil {
		return domain.Connection{}, fmt.Errorf("webhook: encrypt secret: %w", err)
	}

	conn := domain.Connection{
		Provider:       domain.IntegrationWebhook,
		Status:         domain.StatusConnected,
		InstallationID: princ.OrgID,
		Metadata: map[string]any{
			"label":            label,
			"webhook_path":     "/v1/webhooks/webhook/" + princ.OrgID,
			"signature_header": SignatureHeader,
		},
		UpdatedAt: time.Now().UTC(),
	}
	if err := p.repo.Upsert(ctx, princ.OrgID, conn, enc); err != nil {
		return domain.Connection{}, fmt.Errorf("webhook: persist: %w", err)
	}
	// Surface the secret once, and only when we minted it — re-connecting
	// with an operator-supplied secret must not echo it back.
	if generated {
		conn.Metadata["signing_secret"] = secret
	}
	return conn, nil
}

func (p *Provider) Disconnect(ctx context.Context, princ domain.Principal) error {
	return p.repo.Delete(ctx, princ.OrgID, domain.IntegrationWebhook)
}

func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
	conn, _, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationWebhook)
	if err != nil {
		return domain.Connection{}, err
	}
	if conn.Metadata != nil {
		delete(conn.Metadata, "signing_secret")
	}
	return conn, nil
}

// inboundEvent is the wire format. Everything except `title` is optional so
// a one-line curl is a valid incident.
type inboundEvent struct {
	Title       string         `json:"title"`
	Service     string         `json:"service"`
	Environment string         `json:"environment"`
	Level       string         `json:"level"`
	EventID     string         `json:"event_id"`
	Stacktrace  string         `json:"stacktrace"`
	Logs        string         `json:"logs"`
	Metadata    map[string]any `json:"metadata"`
}

// HandleWebhook verifies the HMAC and lands one row in incidents_raw.
//
// The signature is computed over the exact bytes received; the caller must
// sign the same body it sends. A mismatch returns an error containing "HMAC
// mismatch" so the transport layer maps it to 401 (see handler.isHMACError).
func (p *Provider) HandleWebhook(ctx context.Context, orgID string, headers map[string]string, body []byte) error {
	sig := headers[SignatureHeader]
	if sig == "" {
		// Header lookup in the transport layer is canonicalised, but be
		// forgiving about the exact casing callers send.
		sig = headers["X-NEXIS-SIGNATURE"]
	}
	if sig == "" {
		return errors.New("webhook: missing signature")
	}
	sig = strings.TrimPrefix(strings.TrimSpace(sig), "sha256=")

	_, enc, err := p.repo.Get(ctx, orgID, domain.IntegrationWebhook)
	if err != nil || enc == nil {
		return fmt.Errorf("webhook: no connection for org=%s", orgID)
	}
	secret, err := p.kv.Decrypt(ctx, enc)
	if err != nil {
		return fmt.Errorf("webhook: decrypt: %w", err)
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(strings.ToLower(sig))) {
		return errors.New("webhook: HMAC mismatch")
	}

	var ev inboundEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return fmt.Errorf("webhook: invalid JSON body: %w", err)
	}
	if strings.TrimSpace(ev.Title) == "" {
		return errors.New("webhook: title required")
	}
	if p.sink == nil {
		return nil
	}

	level := strings.ToLower(strings.TrimSpace(ev.Level))
	if level == "" {
		// Default to fatal: a service that bothers to report an incident is
		// reporting a failure, and fatal is what makes Sentinel act on the
		// row rather than only fold it into the spike baseline.
		level = "fatal"
	}
	service := strings.TrimSpace(ev.Service)
	if service == "" {
		service = "unknown"
	}
	env := strings.TrimSpace(ev.Environment)
	if env == "" {
		env = "production"
	}

	payload := map[string]any{
		"title":       ev.Title,
		"service":     service,
		"environment": env,
		"stacktrace":  ev.Stacktrace,
		"logs":        ev.Logs,
	}
	if ev.Metadata != nil {
		payload["metadata"] = ev.Metadata
	}

	return p.sink.Insert(ctx, orgID, domain.RawIncident{
		Source:        string(domain.IntegrationWebhook),
		SourceEventID: ev.EventID,
		Title:         ev.Title,
		Level:         level,
		Service:       service,
		Environment:   env,
		Payload:       payload,
	})
}

// compile-time conformance check
var _ domain.Integration = (*Provider)(nil)
