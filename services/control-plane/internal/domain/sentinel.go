package domain

import (
	"context"
	"time"
)

// IncidentTrigger is what the Sentinel detector emits per evaluated rule hit.
// The detector goroutine consumes []IncidentTrigger from rules.Apply and
// hands them to WorkflowService.Start one by one.
//
// SourceEventID is filled on per-row triggers (rule="fatal_level") with the
// upstream natural key — used by the multi-source dedupe layer to collapse
// duplicates inside a 60-second window. Rate-spike triggers leave it empty
// because they correspond to an aggregate count, not a single event.
//
// IncidentRawID is the FK to incidents_raw — Sentinel writes back project_id
// onto that row once the router resolves a match, so downstream queries
// (timeline, dashboards) can scope by project without re-running the match.
//
// ProjectID is stamped by the sentinel.Router after a successful fingerprint
// match against the projects table. Empty when no project owns this trigger's
// upstream selectors — the workflow still runs, just falls back to the legacy
// fixture path.
//
// The fingerprint fields (Source + Sentry/Datadog/PagerDuty/GitHub identifiers)
// are filled by the rules layer from the matching IncidentRow before the
// trigger enters the router. They are the inputs to ProjectMatcher.
type IncidentTrigger struct {
	OrgID         string
	WorkspaceID   string
	IncidentID    string
	IncidentRawID string
	SourceEventID string
	Rule          string // 'fatal_level' | 'error_rate_spike'
	DetectedAt    time.Time
	ReceivedAt    time.Time

	// ProjectID is filled by the sentinel.Router post-fingerprint match.
	// Empty when no project owns the trigger.
	ProjectID string

	// Incident content, copied off the incidents_raw row. Without these the
	// recovery workflow starts with an incident_id and nothing to reason
	// about, so every LLM agent returns an empty structure and the run
	// degrades to stub output. The demo path embeds a fixture for the same
	// reason; the autonomous path has the real row and must pass it on.
	Title       string
	Service     string
	Environment string
	Stacktrace  string
	Logs        string

	// Fingerprint fields. Populated from the incidents_raw row by the rules
	// layer (or by adapters' webhook paths for direct fan-out). The router
	// reads these into a domain.IncidentFingerprint and asks ProjectMatcher
	// to resolve a project_id.
	Source                 string
	SentryOrganizationSlug string
	SentryProjectSlug      string
	DatadogServiceTag      string
	PagerDutyServiceID     string
	GitHubRepo             string
}

// IncidentRow is the minimal projection the Sentinel rules + downstream
// activities need. The full row in incidents_raw has more columns; the
// stacktrace + logs strings are extracted from raw_payload by the repo.
//
// SourceEventID is the per-source natural key (Sentry event_id, Datadog
// alert_id, PagerDuty incident.id). The Sentinel dedupe layer
// (sentinel/multisource.go) groups by (org_id, source_event_id) inside a
// 60-second window so a single root incident observed by multiple sources
// only spawns one recovery workflow.
type IncidentRow struct {
	ID            string
	OrgID         string
	Source        string // 'sentry'|'datadog'|'pagerduty'
	SourceEventID string
	Level         string // 'fatal'|'error'|'warning'|'info'
	Title         string
	Service       string
	Environment   string
	Stacktrace    string
	Logs          string
	ReceivedAt    time.Time

	// Fingerprint fields persisted on incidents_raw by each adapter's
	// HandleWebhook path. Sentinel's router uses these to resolve a project
	// without re-decoding the raw payload. All optional — older rows
	// pre-dating the projects-routing feature have these as empty strings.
	SentryOrganizationSlug string
	SentryProjectSlug      string
	DatadogServiceTag      string
	PagerDutyServiceID     string
	GitHubRepo             string
}

// IncidentsReader is the read-only port the Sentinel goroutine depends on.
// Implementations live in internal/adapter/repo/incidents_repo.go.
type IncidentsReader interface {
	// PollFatalSince returns rows where (source='sentry', level='fatal',
	// received_at > since). System-job path — caller bypasses RLS via the
	// admin pool.
	PollFatalSince(ctx context.Context, orgID string, since time.Time) ([]IncidentRow, error)

	// CountRecent returns the count of rows within the lookback window
	// [now-window, now]. Used by the error-rate-spike rule.
	CountRecent(ctx context.Context, orgID string, window time.Duration) (int, error)

	// MaxReceivedAt is called at detector startup to initialize last_seen_at.
	MaxReceivedAt(ctx context.Context, orgID string) (time.Time, error)
}
