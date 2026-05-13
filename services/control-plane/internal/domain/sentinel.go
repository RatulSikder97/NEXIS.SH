package domain

import (
	"context"
	"time"
)

// IncidentTrigger is what the Sentinel detector emits per evaluated rule hit.
// The detector goroutine consumes []IncidentTrigger from rules.Apply and
// hands them to WorkflowService.Start one by one.
type IncidentTrigger struct {
	OrgID       string
	WorkspaceID string
	IncidentID  string
	Rule        string // 'fatal_level' | 'error_rate_spike'
	DetectedAt  time.Time
	ReceivedAt  time.Time
}

// IncidentRow is the minimal projection the Sentinel rules + downstream
// activities need. The full row in incidents_raw has more columns; the
// stacktrace + logs strings are extracted from raw_payload by the repo.
type IncidentRow struct {
	ID          string
	OrgID       string
	Source      string // 'sentry'
	Level       string // 'fatal'|'error'|'warning'|'info'
	Title       string
	Service     string
	Environment string
	Stacktrace  string
	Logs        string
	ReceivedAt  time.Time
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
