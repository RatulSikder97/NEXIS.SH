package domain

import (
	"context"
	"time"
)

// IntegrationProvider names the upstream system. Use the constants below; do
// not invent new providers without a corresponding adapter under
// internal/adapter/integration/<name>/.
type IntegrationProvider string

const (
	IntegrationGitHub    IntegrationProvider = "github"
	IntegrationSentry    IntegrationProvider = "sentry"
	IntegrationArgoCD    IntegrationProvider = "argocd"
	IntegrationSlack     IntegrationProvider = "slack"
	IntegrationDatadog   IntegrationProvider = "datadog"
	IntegrationPagerDuty IntegrationProvider = "pagerduty"
)

// IntegrationStatus mirrors the CHECK constraint on integrations.status.
type IntegrationStatus string

const (
	StatusConnected    IntegrationStatus = "connected"
	StatusPending      IntegrationStatus = "pending"
	StatusError        IntegrationStatus = "error"
	StatusDisconnected IntegrationStatus = "disconnected"
)

// Connection is the tenant-visible view of one integrations row. The encrypted
// secret bytea is NEVER included here — it stays inside the repo + KeyVault
// boundary.
type Connection struct {
	Provider       IntegrationProvider
	Status         IntegrationStatus
	InstallationID string
	Metadata       map[string]any
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Integration is the port each provider adapter (github/sentry/argocd)
// implements. Handlers call Connect/Disconnect/Status from authenticated
// routes; HandleWebhook is called from the unauthenticated webhook router
// after dispatching on the URL's provider segment.
type Integration interface {
	Name() IntegrationProvider
	Connect(ctx context.Context, p Principal, config map[string]any) (Connection, error)
	Disconnect(ctx context.Context, p Principal) error
	Status(ctx context.Context, p Principal) (Connection, error)
	HandleWebhook(ctx context.Context, orgID string, headers map[string]string, body []byte) error
}

// IntegrationRegistry is the composite port exposed by the integration
// factory. Routers depend on this interface, not on the concrete Registry, so
// tests can substitute a fake.
type IntegrationRegistry interface {
	Get(p IntegrationProvider) (Integration, bool)
	List(ctx context.Context, p Principal) ([]Connection, error)
}

// IncidentSink is the port the Sentry adapter (and future event sources) call
// to persist a raw event to incidents_raw. Phase 4 will add a normaliser
// usecase reading from this table; for Phase 3 we only land the row.
type IncidentSink interface {
	Insert(ctx context.Context, orgID string, raw RawIncident) error
}

// RawIncident is the provider-agnostic shape we persist to incidents_raw.
// Payload is the original JSON event for replay/forensics.
type RawIncident struct {
	Source        string
	SourceEventID string
	Title         string
	Level         string
	Service       string
	Environment   string
	Payload       map[string]any
}
