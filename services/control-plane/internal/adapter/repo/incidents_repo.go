package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// IncidentsRepo persists rows in incidents_raw and serves system-job reads of
// the same table.
//
// pool (application, RLS-aware) is the Querier fallback used by webhook-path
// writes that attach a per-request RLS tx via db.FromCtx — the per-org GUC is
// already set, so the policy filters correctly.
//
// adminPool (super-user, RLS-bypass) is used by the Phase 6 Sentinel detector
// goroutine. The detector runs on its own goroutine with context.Background
// and no principal in ctx, so it MUST bypass RLS — otherwise the SELECTs
// match zero rows and no incidents ever fire.
//
// adminPool is optional; when nil the read paths return ErrUnknown so a
// misconfigured cmd/server boot fails loud instead of silently swallowing
// every incident.
type IncidentsRepo struct {
	pool      *pgxpool.Pool // nexis_app — RLS-aware, fallback for db.FromCtx
	adminPool *pgxpool.Pool // nexis super-user — used by Sentinel reads
}

// NewIncidentsRepo builds an IncidentsRepo with the legacy single-pool
// signature. Phase 6 callers should prefer NewIncidentsRepoWithAdmin so the
// Sentinel detector has an RLS-bypass path; this constructor stays for the
// dev/test code that doesn't need it.
func NewIncidentsRepo(pool *pgxpool.Pool) *IncidentsRepo {
	return &IncidentsRepo{pool: pool}
}

// NewIncidentsRepoWithAdmin wires both pools. adminPool may be nil — the
// Sentinel reads will then return ErrUnknown until it's wired in.
func NewIncidentsRepoWithAdmin(pool, adminPool *pgxpool.Pool) *IncidentsRepo {
	if adminPool == nil {
		adminPool = pool
	}
	return &IncidentsRepo{pool: pool, adminPool: adminPool}
}

// Insert lands a single raw incident. The (org_id, source, source_event_id)
// unique index makes Insert idempotent — a retried webhook delivery is a no-op
// rather than a duplicate row.
func (r *IncidentsRepo) Insert(ctx context.Context, orgID string, raw domain.RawIncident) error {
	q := db.FromCtx(ctx, r.pool)
	payloadJSON, err := json.Marshal(raw.Payload)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
        INSERT INTO incidents_raw (org_id, source, source_event_id, title, level, service, environment, raw_payload)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
        ON CONFLICT (org_id, source, source_event_id) DO NOTHING
    `, orgID, raw.Source, raw.SourceEventID, raw.Title, raw.Level, raw.Service, raw.Environment, payloadJSON)
	return err
}

// compile-time conformance check
var _ domain.IncidentSink = (*IncidentsRepo)(nil)
var _ domain.IncidentsReader = (*IncidentsRepo)(nil)

// PollFatalSince returns Sentry rows with level='fatal' whose received_at is
// after `since`, capped at 50 rows per call to bound a single Sentinel tick.
// Runs on the admin pool — this is a system-job path with no principal in ctx.
//
// The query projects title / service / environment from the dedicated columns
// and pulls stacktrace / logs out of raw_payload->'data'->'exception' (Sentry
// envelope shape). Phase 6's fixture incidents place stacktrace under the
// same path so the same query works for both real Sentry events and the
// scripted fixture-pump path.
func (r *IncidentsRepo) PollFatalSince(ctx context.Context, orgID string, since time.Time) ([]domain.IncidentRow, error) {
	if r.adminPool == nil {
		return nil, fmt.Errorf("IncidentsRepo.PollFatalSince: %w", domain.ErrUnknown)
	}
	rows, err := r.adminPool.Query(ctx, `
		SELECT id, org_id, source,
		       COALESCE(level, '') AS level,
		       COALESCE(title, '') AS title,
		       COALESCE(service, '') AS service,
		       COALESCE(environment, '') AS environment,
		       COALESCE(raw_payload->>'stacktrace', '') AS stacktrace,
		       COALESCE(raw_payload->>'logs', '') AS logs,
		       received_at
		FROM incidents_raw
		WHERE org_id=$1 AND source='sentry' AND level='fatal' AND received_at > $2
		ORDER BY received_at ASC
		LIMIT 50
	`, orgID, since)
	if err != nil {
		return nil, fmt.Errorf("IncidentsRepo.PollFatalSince: %w", err)
	}
	defer rows.Close()
	out := []domain.IncidentRow{}
	for rows.Next() {
		var x domain.IncidentRow
		if err := rows.Scan(&x.ID, &x.OrgID, &x.Source, &x.Level, &x.Title,
			&x.Service, &x.Environment, &x.Stacktrace, &x.Logs, &x.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// CountRecent returns the count of incidents_raw rows received in the window
// [now-window, now]. Used by the Sentinel error-rate-spike rule. Bypasses RLS
// via the admin pool — the detector goroutine has no principal in ctx.
func (r *IncidentsRepo) CountRecent(ctx context.Context, orgID string, window time.Duration) (int, error) {
	if r.adminPool == nil {
		return 0, fmt.Errorf("IncidentsRepo.CountRecent: %w", domain.ErrUnknown)
	}
	// Postgres `make_interval` takes an integer seconds — safer than building
	// a text interval from a Go string.
	secs := int(window.Seconds())
	if secs < 1 {
		secs = 1
	}
	var n int
	err := r.adminPool.QueryRow(ctx, `
		SELECT count(*) FROM incidents_raw
		WHERE org_id=$1 AND received_at > now() - make_interval(secs => $2)
	`, orgID, secs).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("IncidentsRepo.CountRecent: %w", err)
	}
	return n, nil
}

// MaxReceivedAt is called at Sentinel detector startup to initialise
// last_seen_at — without it the goroutine would replay every historical fatal
// row on every server boot. Returns the unix epoch when no rows exist.
func (r *IncidentsRepo) MaxReceivedAt(ctx context.Context, orgID string) (time.Time, error) {
	if r.adminPool == nil {
		return time.Time{}, fmt.Errorf("IncidentsRepo.MaxReceivedAt: %w", domain.ErrUnknown)
	}
	var ts time.Time
	err := r.adminPool.QueryRow(ctx, `
		SELECT COALESCE(MAX(received_at), '1970-01-01'::timestamptz)
		FROM incidents_raw WHERE org_id=$1
	`, orgID).Scan(&ts)
	if err != nil {
		return time.Time{}, fmt.Errorf("IncidentsRepo.MaxReceivedAt: %w", err)
	}
	return ts, nil
}
