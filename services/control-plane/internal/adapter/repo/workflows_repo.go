package repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// WorkflowRepo persists workflow_runs + activity_events. Dual pool — like
// WorkspacesRepo from Phase 3.5 — because Phase 4 has two distinct write
// origins:
//
//  1. HTTP handlers (POST /pipelines, GET /pipelines/...) run inside the
//     per-request RLS tx. These use `app` via db.FromCtx so app.current_org_id
//     is enforced.
//  2. Temporal worker activities run outside any HTTP request — they have no
//     principal to pin RLS to. These use `admin` directly so the RLS policy
//     doesn't filter the writes out to zero rows.
type WorkflowRepo struct {
	app   *pgxpool.Pool // nexis_app — RLS-aware, used inside request tx via db.FromCtx
	admin *pgxpool.Pool // nexis superuser — bypasses RLS for worker activities + SSE replay
}

// NewWorkflowRepo constructs a WorkflowRepo. admin may be nil — system paths
// fall back to app, which works only when RLS is off (early dev).
func NewWorkflowRepo(app, admin *pgxpool.Pool) *WorkflowRepo {
	if admin == nil {
		admin = app
	}
	return &WorkflowRepo{app: app, admin: admin}
}

// InsertRun creates a workflow_runs row. Runs inside the request tx via
// db.FromCtx so the INSERT obeys the tenant policy. Caller fills id +
// org_id + workspace_id + status + started_at; Temporal ids are written by
// UpdateTemporalIDs once ExecuteWorkflow returns.
func (r *WorkflowRepo) InsertRun(ctx context.Context, w *domain.WorkflowRun) error {
	q := db.FromCtx(ctx, r.app)
	_, err := q.Exec(ctx, `
        INSERT INTO workflow_runs (
            id, org_id, workspace_id, workflow_type,
            temporal_run_id, temporal_wf_id, status,
            current_step, input, output, error,
            started_at, completed_at, duration_ms, created_by
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14, NULLIF($15, '')::uuid)`,
		w.ID, w.OrgID, w.WorkspaceID, w.WorkflowType,
		w.TemporalRunID, w.TemporalWfID, string(w.Status),
		w.CurrentStep, jsonOrNil(w.Input), jsonOrNil(w.Output), w.Error,
		w.StartedAt, w.CompletedAt, w.DurationMs, w.CreatedBy,
	)
	return err
}

// UpdateTemporalIDs records the Temporal-assigned WorkflowID + RunID after
// ExecuteWorkflow returns. Routes through db.FromCtx so the UPDATE lands in
// the same request tx as the InsertRun that preceded it — using the admin
// pool here would race the tx commit and match zero rows. Falls back to the
// app pool when there is no tx (post-commit callers).
func (r *WorkflowRepo) UpdateTemporalIDs(ctx context.Context, runID, tempWfID, tempRunID string) error {
	q := db.FromCtx(ctx, r.app)
	_, err := q.Exec(ctx,
		`UPDATE workflow_runs SET temporal_wf_id=$2, temporal_run_id=$3 WHERE id=$1`,
		runID, tempWfID, tempRunID)
	return err
}

// UpdateRunStatus advances the workflow_runs row's terminal state. Always
// runs on the admin pool because callers — the reaper goroutine + the
// Temporal worker — are outside any RLS tx.
func (r *WorkflowRepo) UpdateRunStatus(
	ctx context.Context,
	runID string,
	status domain.WorkflowRunStatus,
	currentStep, errStr string,
	output []byte,
	completedAt *time.Time,
) error {
	var durationMs *int64
	if completedAt != nil {
		// Compute duration from the started_at column rather than passing it
		// in — keeps the SQL idempotent on retries.
	}
	_, err := r.admin.Exec(ctx, `
        UPDATE workflow_runs
        SET status        = $2,
            current_step  = COALESCE(NULLIF($3, ''), current_step),
            error         = NULLIF($4, ''),
            output        = COALESCE($5::jsonb, output),
            completed_at  = COALESCE($6, completed_at),
            duration_ms   = CASE WHEN $6::timestamptz IS NOT NULL THEN
                                EXTRACT(EPOCH FROM ($6::timestamptz - started_at))*1000
                            ELSE duration_ms END
        WHERE id=$1`, runID, string(status), currentStep, errStr, jsonOrNil(output), completedAt)
	_ = durationMs
	return err
}

// GetRun fetches a single workflow_runs row by (org_id, id). org_id is a
// defence-in-depth filter on top of the RLS policy. Returns ErrNotFound when
// no row matches.
func (r *WorkflowRepo) GetRun(ctx context.Context, orgID, runID string) (*domain.WorkflowRun, error) {
	q := db.FromCtx(ctx, r.app)
	var w domain.WorkflowRun
	var input, output []byte
	var createdBy *string
	err := q.QueryRow(ctx, `
        SELECT id, org_id, workspace_id, workflow_type,
               COALESCE(temporal_run_id, ''), COALESCE(temporal_wf_id, ''),
               status, COALESCE(current_step, ''),
               input, output, COALESCE(error, ''),
               started_at, completed_at, duration_ms,
               created_by
        FROM workflow_runs
        WHERE org_id=$1 AND id=$2`, orgID, runID).Scan(
		&w.ID, &w.OrgID, &w.WorkspaceID, &w.WorkflowType,
		&w.TemporalRunID, &w.TemporalWfID,
		&w.Status, &w.CurrentStep,
		&input, &output, &w.Error,
		&w.StartedAt, &w.CompletedAt, &w.DurationMs,
		&createdBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	w.Input = input
	w.Output = output
	if createdBy != nil {
		w.CreatedBy = *createdBy
	}
	return &w, nil
}

// ListRuns returns workflow_runs for one (org, workspace), most-recent first.
// `before` is the cursor — pass time.Time{} for the first page. `limit` is
// capped at 200.
func (r *WorkflowRepo) ListRuns(ctx context.Context, orgID, workspaceID string, limit int, before time.Time) ([]domain.WorkflowRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := db.FromCtx(ctx, r.app)
	rows, err := q.Query(ctx, `
        SELECT id, org_id, workspace_id, workflow_type,
               COALESCE(temporal_run_id, ''), COALESCE(temporal_wf_id, ''),
               status, COALESCE(current_step, ''),
               COALESCE(error, ''),
               started_at, completed_at, duration_ms
        FROM workflow_runs
        WHERE org_id=$1 AND workspace_id=$2
          AND ($3::timestamptz IS NULL OR started_at < $3)
        ORDER BY started_at DESC
        LIMIT $4`, orgID, workspaceID, nullTime(before), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.WorkflowRun{}
	for rows.Next() {
		var w domain.WorkflowRun
		if err := rows.Scan(
			&w.ID, &w.OrgID, &w.WorkspaceID, &w.WorkflowType,
			&w.TemporalRunID, &w.TemporalWfID,
			&w.Status, &w.CurrentStep,
			&w.Error,
			&w.StartedAt, &w.CompletedAt, &w.DurationMs,
		); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// InsertEvent persists one activity_events row. Always uses the admin pool —
// callers are Temporal activity functions that run outside any HTTP request
// tx, so RLS would have nothing to bind to.
func (r *WorkflowRepo) InsertEvent(ctx context.Context, e *domain.ActivityEvent) error {
	_, err := r.admin.Exec(ctx, `
        INSERT INTO activity_events (
            org_id, workflow_run_id, seq, agent_role, activity_name,
            status, attempt, message, payload, ts
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
        ON CONFLICT (workflow_run_id, seq) DO NOTHING`,
		e.OrgID, e.WorkflowRunID, e.Seq, string(e.AgentRole), e.ActivityName,
		string(e.Status), e.Attempt, e.Message, jsonOrNil(e.Payload), e.TS,
	)
	return err
}

// ListEvents returns all activity_events for a run with seq > sinceSeq,
// oldest first. Uses the admin pool because callers (SSE handler) have
// already done a workspace-ownership check before this point — no need to
// thread RLS through a long-lived stream.
func (r *WorkflowRepo) ListEvents(ctx context.Context, runID string, sinceSeq, limit int) ([]domain.ActivityEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := r.admin.Query(ctx, `
        SELECT id, org_id, workflow_run_id, seq, agent_role, activity_name,
               status, attempt, COALESCE(message, ''), payload, ts
        FROM activity_events
        WHERE workflow_run_id=$1 AND seq > $2
        ORDER BY seq ASC
        LIMIT $3`, runID, sinceSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ActivityEvent{}
	for rows.Next() {
		var e domain.ActivityEvent
		var role, status string
		var payload []byte
		if err := rows.Scan(
			&e.ID, &e.OrgID, &e.WorkflowRunID, &e.Seq, &role, &e.ActivityName,
			&status, &e.Attempt, &e.Message, &payload, &e.TS,
		); err != nil {
			return nil, err
		}
		e.AgentRole = domain.AgentRole(role)
		e.Status = domain.ActivityStatus(status)
		e.Payload = payload
		out = append(out, e)
	}
	return out, rows.Err()
}

// NextSeq returns the next unused seq for a given run. Used by the recorder
// activity to assign monotonically increasing event ids without depending on
// in-process state (Temporal can replay an activity, so the seq must be
// stable per (run, attempt) pair — the unique index on (run, seq) makes
// duplicate inserts harmless).
func (r *WorkflowRepo) NextSeq(ctx context.Context, runID string) (int, error) {
	var n int
	err := r.admin.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM activity_events WHERE workflow_run_id=$1`, runID).Scan(&n)
	return n, err
}

// jsonOrNil returns nil for an empty byte slice so the JSONB column lands
// as SQL NULL rather than an empty string (which Postgres rejects).
func jsonOrNil(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// nullTime turns a zero time.Time into a SQL NULL for the optional `before`
// cursor in ListRuns.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
