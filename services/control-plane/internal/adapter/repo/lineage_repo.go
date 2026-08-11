package repo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// LineageRepo persists OpenLineage RunEvents in lineage_events (migration
// 0028).
//
// Dual-pool — same shape as WebhookDeliveriesRepo:
//
//   - pool (nexis_app, RLS-aware) is the db.FromCtx fallback for any future
//     request-path writes that run under a per-request RLS tx.
//
//   - adminPool (nexis super-user, RLS-bypass) serves the two call sites
//     that exist today: the Temporal activity goroutine's Insert (no
//     principal in ctx, so RLS would silently drop the row) and the
//     operator-side ListByOrg (org-scoped at the SQL layer, same
//     defence-in-depth posture as webhook_deliveries reads).
//
// The table is append-only at the application boundary — no Update/Delete.
type LineageRepo struct {
	pool      *pgxpool.Pool // nexis_app — RLS-aware, fallback for db.FromCtx
	adminPool *pgxpool.Pool // nexis super-user — activity writes + operator reads
}

// NewLineageRepo constructs the repo. adminPool may be nil — it falls back
// to pool, which works when RLS is off but drops activity-side writes once
// the 0028 policy is enabled.
func NewLineageRepo(pool, adminPool *pgxpool.Pool) *LineageRepo {
	if adminPool == nil {
		adminPool = pool
	}
	return &LineageRepo{pool: pool, adminPool: adminPool}
}

// LineageEvent is the in-memory shape of one lineage_events row. ID +
// CreatedAt are server-assigned on insert; the caller leaves them zero.
// Event is the full OpenLineage RunEvent JSON verbatim; the typed fields
// duplicate the queryable columns.
type LineageEvent struct {
	ID            string
	OrgID         string
	WorkflowRunID string
	EventType     string // OpenLineage RunState: START|RUNNING|COMPLETE|ABORT|FAIL|OTHER
	EventTime     time.Time
	JobNamespace  string
	JobName       string
	RunID         string // OpenLineage run.runId (deterministic UUIDv5)
	Event         map[string]any
	CreatedAt     time.Time
}

// Insert appends one RunEvent row. Routes through db.FromCtx with the admin
// pool as fallback — the only writer today is the Temporal activity
// goroutine, which has no RLS tx in ctx; a request-path caller that does
// carry one is honoured automatically.
//
// nil-safe — a nil repo is a silent no-op so the recovery activities can
// emit best-effort without wiring ceremony (matches WebhookDeliveriesRepo).
func (r *LineageRepo) Insert(ctx context.Context, ev LineageEvent) error {
	if r == nil {
		return nil
	}
	eventJSON, err := json.Marshal(ev.Event)
	if err != nil {
		return err
	}
	q := db.FromCtx(ctx, r.adminPool)
	_, err = q.Exec(ctx, `
        INSERT INTO lineage_events (
            org_id, workflow_run_id, event_type, event_time,
            job_namespace, job_name, run_id, event
        ) VALUES ($1,$2::uuid,$3,$4,$5,$6,$7::uuid,$8)`,
		ev.OrgID, ev.WorkflowRunID, ev.EventType, ev.EventTime,
		ev.JobNamespace, ev.JobName, ev.RunID, eventJSON,
	)
	return err
}

// LineageFilter holds the optional filters for ListByOrg. Empty
// WorkflowRunID / JobName match everything. Limit defaults to 50 (max 200);
// Offset defaults to 0.
type LineageFilter struct {
	OrgID         string
	WorkflowRunID string
	JobName       string
	Limit         int
	Offset        int
}

// ListByOrg returns the most-recent N lineage events for an org, optionally
// narrowed to one workflow run and/or job name, plus the unpaginated total.
//
// Runs on the admin pool with the org_id filter as the tenancy boundary —
// the HTTP handler passes principal.OrgID, never a URL value, mirroring the
// webhook-deliveries read path.
func (r *LineageRepo) ListByOrg(ctx context.Context, f LineageFilter) ([]LineageEvent, int, error) {
	if r == nil || r.adminPool == nil {
		return []LineageEvent{}, 0, nil
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	// workflow_run_id compares as text so a malformed caller-supplied id
	// yields zero rows instead of a uuid-cast error.
	var total int
	if err := r.adminPool.QueryRow(ctx, `
        SELECT count(*) FROM lineage_events
        WHERE org_id=$1
          AND ($2='' OR workflow_run_id::text=$2)
          AND ($3='' OR job_name=$3)`,
		f.OrgID, f.WorkflowRunID, f.JobName,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []LineageEvent{}, 0, nil
	}

	rows, err := r.adminPool.Query(ctx, `
        SELECT id::text, org_id::text, workflow_run_id::text, event_type,
               event_time, job_namespace, job_name, run_id::text,
               COALESCE(event::text, ''), created_at
        FROM lineage_events
        WHERE org_id=$1
          AND ($2='' OR workflow_run_id::text=$2)
          AND ($3='' OR job_name=$3)
        ORDER BY created_at DESC
        LIMIT $4 OFFSET $5`,
		f.OrgID, f.WorkflowRunID, f.JobName, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]LineageEvent, 0, limit)
	for rows.Next() {
		var ev LineageEvent
		var eventStr string
		if err := rows.Scan(
			&ev.ID, &ev.OrgID, &ev.WorkflowRunID, &ev.EventType,
			&ev.EventTime, &ev.JobNamespace, &ev.JobName, &ev.RunID,
			&eventStr, &ev.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		if eventStr != "" {
			_ = json.Unmarshal([]byte(eventStr), &ev.Event)
		}
		out = append(out, ev)
	}
	return out, total, rows.Err()
}
