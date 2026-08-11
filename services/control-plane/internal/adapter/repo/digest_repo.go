// Package repo — digest_reports adapter (daily admin digest).
//
// DigestRepo is admin-pool-only: every method is called from the DailyDigest
// cron goroutine, which runs on context.Background with no principal, so it
// MUST bypass RLS to sweep every org. The aggregation queries mirror the
// numbers the console dashboard computes client-side from workflow_runs
// (apps/web/components/console/DashboardCharts.tsx) — incidents over time,
// recovery outcomes, MTTR over succeeded runs — plus the approval-decision
// split and per-agent activity counts the widgets derive from the same rows.
package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// DigestRepo aggregates + persists daily digest reports.
type DigestRepo struct {
	adminPool *pgxpool.Pool
}

// NewDigestRepo constructs a DigestRepo on the admin pool.
func NewDigestRepo(admin *pgxpool.Pool) *DigestRepo {
	return &DigestRepo{adminPool: admin}
}

// AdminListActiveOrgs returns every org with any signal (incident received OR
// workflow run started) inside [since, until). Orgs with zero activity get no
// digest — an empty email every day trains admins to ignore the real ones.
func (r *DigestRepo) AdminListActiveOrgs(ctx context.Context, since, until time.Time) ([]string, error) {
	rows, err := r.adminPool.Query(ctx, `
        SELECT org_id::text FROM incidents_raw
        WHERE received_at >= $1 AND received_at < $2
        UNION
        SELECT org_id::text FROM workflow_runs
        WHERE started_at >= $1 AND started_at < $2`,
		since, until,
	)
	if err != nil {
		return nil, fmt.Errorf("digest: list active orgs: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AdminAggregate computes the org's DigestMetrics for [since, until).
func (r *DigestRepo) AdminAggregate(ctx context.Context, orgID string, since, until time.Time) (domain.DigestMetrics, error) {
	var m domain.DigestMetrics

	// Incidents detected + recovery outcomes + MTTR in one round-trip each —
	// the tables are indexed on (org_id, received_at) / (org_id, ...,
	// started_at) so these are cheap range scans.
	if err := r.adminPool.QueryRow(ctx, `
        SELECT count(*) FROM incidents_raw
        WHERE org_id = $1 AND received_at >= $2 AND received_at < $3`,
		orgID, since, until,
	).Scan(&m.IncidentsDetected); err != nil {
		return m, fmt.Errorf("digest: incidents: %w", err)
	}

	if err := r.adminPool.QueryRow(ctx, `
        SELECT count(*),
               count(*) FILTER (WHERE status = 'succeeded'),
               count(*) FILTER (WHERE status IN ('failed', 'timed_out', 'cancelled')),
               COALESCE(avg(duration_ms) FILTER (WHERE status = 'succeeded'), 0)::bigint
        FROM workflow_runs
        WHERE org_id = $1 AND started_at >= $2 AND started_at < $3`,
		orgID, since, until,
	).Scan(&m.RunsStarted, &m.RepairsSucceeded, &m.RepairsFailed, &m.MTTRMs); err != nil {
		return m, fmt.Errorf("digest: runs: %w", err)
	}

	if err := r.adminPool.QueryRow(ctx, `
        SELECT count(*) FILTER (WHERE decision = 'approved'),
               count(*) FILTER (WHERE decision = 'rejected'),
               count(*) FILTER (WHERE decision = 'modified'),
               count(*) FILTER (WHERE decision = 'auto_approved'),
               count(*) FILTER (WHERE decision = 'timeout_rejected'),
               count(*) FILTER (WHERE decision = 'pending')
        FROM approval_decisions
        WHERE org_id = $1
          AND ((decided_at >= $2 AND decided_at < $3)
               OR (decision = 'pending' AND created_at < $3))`,
		orgID, since, until,
	).Scan(&m.Approved, &m.Rejected, &m.Modified, &m.AutoApproved, &m.TimeoutRejected, &m.PendingApprovals); err != nil {
		return m, fmt.Errorf("digest: approvals: %w", err)
	}

	rows, err := r.adminPool.Query(ctx, `
        SELECT agent_role, count(*)
        FROM activity_events
        WHERE org_id = $1 AND ts >= $2 AND ts < $3 AND status = 'succeeded'
        GROUP BY agent_role`,
		orgID, since, until,
	)
	if err != nil {
		return m, fmt.Errorf("digest: agent activity: %w", err)
	}
	defer rows.Close()
	m.AgentActivity = map[string]int{}
	for rows.Next() {
		var role string
		var n int
		if err := rows.Scan(&role, &n); err != nil {
			return m, err
		}
		m.AgentActivity[role] = n
	}
	return m, rows.Err()
}

// AdminInsertReport persists one digest row. Idempotent per (org, UTC day)
// via the digest_reports_org_day_idx unique index — a restarted server that
// re-ticks inside the same day inserts nothing and returns inserted=false so
// the caller can skip the email too.
func (r *DigestRepo) AdminInsertReport(ctx context.Context, rep domain.DigestReport) (inserted bool, err error) {
	metricsJSON, err := json.Marshal(rep.Metrics)
	if err != nil {
		return false, fmt.Errorf("digest: marshal metrics: %w", err)
	}
	insights := rep.Insights
	if insights == nil {
		insights = []string{}
	}
	insightsJSON, err := json.Marshal(insights)
	if err != nil {
		return false, fmt.Errorf("digest: marshal insights: %w", err)
	}
	tag, err := r.adminPool.Exec(ctx, `
        INSERT INTO digest_reports (org_id, period_start, period_end, metrics, insights)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (org_id, ((period_end AT TIME ZONE 'UTC')::date)) DO NOTHING`,
		rep.OrgID, rep.PeriodStart, rep.PeriodEnd, metricsJSON, insightsJSON,
	)
	if err != nil {
		return false, fmt.Errorf("digest: insert report: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// AdminOwnerAdminEmails returns the org's owner + admin member emails —
// the digest recipient list. Satisfies notifier.AdminEmailLister via
// structural typing.
func (r *DigestRepo) AdminOwnerAdminEmails(ctx context.Context, orgID string) ([]string, error) {
	rows, err := r.adminPool.Query(ctx, `
        SELECT u.email
        FROM org_members m
        JOIN users u ON u.id = m.user_id
        WHERE m.org_id = $1 AND m.role IN ('owner', 'admin')
        ORDER BY u.email`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("digest: recipients: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		out = append(out, email)
	}
	return out, rows.Err()
}
