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
//
// Fingerprint fields on RawIncident are folded into raw_payload under a
// canonical "_fingerprint" key so the Sentinel router can recover them later
// without each adapter reinventing the wire shape. The fingerprint copy is
// purely additive — existing raw_payload keys are preserved untouched.
func (r *IncidentsRepo) Insert(ctx context.Context, orgID string, raw domain.RawIncident) error {
	q := db.FromCtx(ctx, r.pool)
	payload := raw.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	// Fold the fingerprint into the payload under a stable key so a future
	// schema-less reader (Phase 7 metric pipeline) can recover the routing
	// inputs without a join. Per-field empty checks keep the payload tidy
	// when an adapter has nothing to contribute.
	fp := map[string]any{}
	if raw.SentryOrganizationSlug != "" {
		fp["sentry_organization_slug"] = raw.SentryOrganizationSlug
	}
	if raw.SentryProjectSlug != "" {
		fp["sentry_project_slug"] = raw.SentryProjectSlug
	}
	if raw.DatadogServiceTag != "" {
		fp["datadog_service_tag"] = raw.DatadogServiceTag
	}
	if raw.PagerDutyServiceID != "" {
		fp["pagerduty_service_id"] = raw.PagerDutyServiceID
	}
	if raw.GitHubRepo != "" {
		fp["github_repo"] = raw.GitHubRepo
	}
	if len(fp) > 0 {
		payload["_fingerprint"] = fp
	}
	payloadJSON, err := json.Marshal(payload)
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

// InsertAdmin is the system-side twin of Insert: same idempotent write, but
// on the admin pool because the caller (the QA continuous-loop cron) runs on
// context.Background with no principal — the RLS-aware app pool would reject
// the row for lack of a bound org GUC. Reuses Insert's payload/fingerprint
// folding by temporarily narrowing the repo to the admin pool.
func (r *IncidentsRepo) InsertAdmin(ctx context.Context, orgID string, raw domain.RawIncident) error {
	if r.adminPool == nil {
		return fmt.Errorf("IncidentsRepo.InsertAdmin: %w", domain.ErrUnknown)
	}
	adminView := &IncidentsRepo{pool: r.adminPool, adminPool: r.adminPool}
	return adminView.Insert(ctx, orgID, raw)
}

// UpdateProjectID stamps incidents_raw.project_id once Sentinel's router
// resolves a project for the row. Runs on the admin pool because the call
// site is the Sentinel goroutine (no principal in ctx), and the column is a
// system-write that should not be filtered by RLS.
//
// Idempotent: re-stamping with the same value is a no-op. A nil/empty
// projectID is rejected as a misuse rather than silently writing NULL — the
// router only calls this on a successful match.
func (r *IncidentsRepo) UpdateProjectID(ctx context.Context, incidentRawID, projectID string) error {
	if incidentRawID == "" {
		return fmt.Errorf("IncidentsRepo.UpdateProjectID: empty incident id")
	}
	if projectID == "" {
		return fmt.Errorf("IncidentsRepo.UpdateProjectID: empty project id")
	}
	if r.adminPool == nil {
		return fmt.Errorf("IncidentsRepo.UpdateProjectID: %w", domain.ErrUnknown)
	}
	_, err := r.adminPool.Exec(ctx, `
		UPDATE incidents_raw SET project_id = $2::uuid
		WHERE id = $1::uuid
	`, incidentRawID, projectID)
	if err != nil {
		return fmt.Errorf("IncidentsRepo.UpdateProjectID: %w", err)
	}
	return nil
}

// compile-time conformance check
var _ domain.IncidentSink = (*IncidentsRepo)(nil)
var _ domain.IncidentsReader = (*IncidentsRepo)(nil)

// PollFatalSince returns rows with level='fatal' whose received_at is after
// `since`, capped at 50 rows per call to bound a single Sentinel tick.
// Runs on the admin pool — this is a system-job path with no principal in ctx.
//
// Multi-source widening (Task 8): the source filter is now
// IN ('sentry','datadog','pagerduty','schema_drift') — every adapter that
// emits via IncidentSink.Insert. The level=='fatal' criterion still applies;
// the Datadog adapter maps Priority=='P1' to 'fatal' (per its alert→level
// table) and PagerDuty maps Severity=='critical' to 'fatal', so a P1 alert
// or a PagerDuty critical lands in the same tick that a Sentry fatal does.
// 'schema_drift' (Phase 9) is the Data Engineer drift checker's synthetic
// source — always level='fatal', deduped by its drift-fingerprint
// source_event_id. 'deploy_engine' is the preview-deploy failure path: the
// deployments handler inserts a fatal row (stacktrace = error + build log,
// logs = container log) whose GitHubRepo fingerprint routes it back to the
// owning project, so a broken build enters the exact same recovery pipeline
// as any other incident. The detector's dedupe layer
// (sentinel/multisource.go) collapses double-fires when multiple sources
// observe the same root incident inside the dedupe window.
//
// The query projects title / service / environment from the dedicated columns
// and pulls stacktrace / logs out of raw_payload. Phase 6's fixture incidents
// place stacktrace under the same path so the same query works for both real
// Sentry events and the scripted fixture-pump path.
func (r *IncidentsRepo) PollFatalSince(ctx context.Context, orgID string, since time.Time) ([]domain.IncidentRow, error) {
	if r.adminPool == nil {
		return nil, fmt.Errorf("IncidentsRepo.PollFatalSince: %w", domain.ErrUnknown)
	}
	rows, err := r.adminPool.Query(ctx, `
		SELECT id, org_id, source,
		       COALESCE(source_event_id, '') AS source_event_id,
		       COALESCE(level, '') AS level,
		       COALESCE(title, '') AS title,
		       COALESCE(service, '') AS service,
		       COALESCE(environment, '') AS environment,
		       COALESCE(raw_payload->>'stacktrace', '') AS stacktrace,
		       COALESCE(raw_payload->>'logs', '') AS logs,
		       received_at,
		       COALESCE(raw_payload->'_fingerprint'->>'sentry_organization_slug', '') AS sentry_org_slug,
		       COALESCE(raw_payload->'_fingerprint'->>'sentry_project_slug', '')      AS sentry_project_slug,
		       COALESCE(raw_payload->'_fingerprint'->>'datadog_service_tag', '')       AS datadog_service_tag,
		       COALESCE(raw_payload->'_fingerprint'->>'pagerduty_service_id', '')      AS pagerduty_service_id,
		       COALESCE(raw_payload->'_fingerprint'->>'github_repo', '')               AS github_repo
		FROM incidents_raw
		WHERE org_id=$1 AND source IN ('sentry','datadog','pagerduty','schema_drift','qa_loop','deploy_engine')
		      AND level='fatal' AND received_at > $2
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
		if err := rows.Scan(&x.ID, &x.OrgID, &x.Source, &x.SourceEventID,
			&x.Level, &x.Title, &x.Service, &x.Environment,
			&x.Stacktrace, &x.Logs, &x.ReceivedAt,
			&x.SentryOrganizationSlug, &x.SentryProjectSlug,
			&x.DatadogServiceTag, &x.PagerDutyServiceID, &x.GitHubRepo,
		); err != nil {
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
