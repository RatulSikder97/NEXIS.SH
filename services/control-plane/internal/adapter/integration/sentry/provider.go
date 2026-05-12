// Package sentry implements the Sentry IntegrationProvider. Webhooks land here
// after Sentry-Hook-Signature verification and are persisted to incidents_raw
// via the injected IncidentSink. Phase 4's normaliser will fan these out into
// the canonical incidents table.
package sentry

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Repo is the subset of repo.IntegrationsRepo this adapter needs.
type Repo interface {
	Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
	Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
	Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

// Provider is the Sentry Integration adapter. Unlike GitHub there is no
// defaultSecret fallback — Sentry requires a per-tenant webhook_secret at
// Connect time.
type Provider struct {
	repo Repo
	kv   domain.KeyVault
	sink domain.IncidentSink
}

// New builds a Provider. sink is the IncidentsRepo (or test fake) that
// persists raw events on each verified webhook.
func New(repo Repo, kv domain.KeyVault, sink domain.IncidentSink) *Provider {
	return &Provider{repo: repo, kv: kv, sink: sink}
}

// Name returns the canonical provider identifier.
func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationSentry }

// Connect upserts a Sentry connection. Config must contain "webhook_secret";
// optional "dsn" goes into metadata for the UI.
func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
	dsn, _ := cfg["dsn"].(string)
	secret, _ := cfg["webhook_secret"].(string)
	if secret == "" {
		return domain.Connection{}, errors.New("sentry: webhook_secret required")
	}
	enc, err := p.kv.Encrypt(ctx, []byte(secret))
	if err != nil {
		return domain.Connection{}, err
	}
	c := domain.Connection{
		Provider: domain.IntegrationSentry,
		Status:   domain.StatusConnected,
		Metadata: map[string]any{"dsn": dsn},
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

// Status returns the current Connection for the caller's org.
func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
	c, _, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationSentry)
	return c, err
}

// HandleWebhook verifies Sentry-Hook-Signature against the per-tenant secret,
// decodes the canonical Sentry event shape, extracts the "service" tag, and
// hands the raw payload off to the IncidentSink. The org-level connection MUST
// exist — there is no defaultSecret fallback for Sentry.
func (p *Provider) HandleWebhook(ctx context.Context, orgID string, headers map[string]string, body []byte) error {
	sig := headers["Sentry-Hook-Signature"]
	if sig == "" {
		return errors.New("sentry: missing Sentry-Hook-Signature")
	}

	_, enc, err := p.repo.Get(ctx, orgID, domain.IntegrationSentry)
	if err != nil || enc == nil {
		return fmt.Errorf("sentry: no connection for org=%s", orgID)
	}
	secret, err := p.kv.Decrypt(ctx, enc)
	if err != nil {
		return fmt.Errorf("sentry: decrypt: %w", err)
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(sig)) {
		return errors.New("sentry: HMAC mismatch")
	}

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
	})
}

// compile-time conformance check
var _ domain.Integration = (*Provider)(nil)
