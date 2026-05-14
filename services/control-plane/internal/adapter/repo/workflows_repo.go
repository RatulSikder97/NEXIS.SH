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
//
// project_id is optional — written when the caller bound the run to a
// project at start time. Empty string lands as SQL NULL via NULLIF/cast.
func (r *WorkflowRepo) InsertRun(ctx context.Context, w *domain.WorkflowRun) error {
	q := db.FromCtx(ctx, r.app)
	_, err := q.Exec(ctx, `
        INSERT INTO workflow_runs (
            id, org_id, workspace_id, workflow_type,
            temporal_run_id, temporal_wf_id, status,
            current_step, input, output, error,
            started_at, completed_at, duration_ms, created_by,
            project_id
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14, NULLIF($15, '')::uuid,
                  NULLIF($16, '')::uuid)`,
		w.ID, w.OrgID, w.WorkspaceID, w.WorkflowType,
		w.TemporalRunID, w.TemporalWfID, string(w.Status),
		w.CurrentStep, jsonOrNil(w.Input), jsonOrNil(w.Output), w.Error,
		w.StartedAt, w.CompletedAt, w.DurationMs, w.CreatedBy,
		w.ProjectID,
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
//
// projectID is an optional dedicated-column filter — when non-empty the
// query narrows to runs whose workflow_runs.project_id matches the value.
// Empty disables the filter. The string is cast to uuid in SQL so an
// invalid UUID bubbles up as a postgres "invalid input syntax" error;
// HTTP callers should pre-validate.
func (r *WorkflowRepo) ListRuns(ctx context.Context, orgID, workspaceID, projectID string, limit int, before time.Time) ([]domain.WorkflowRun, error) {
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
          AND ($5 = '' OR project_id = $5::uuid)
        ORDER BY started_at DESC
        LIMIT $4`, orgID, workspaceID, nullTime(before), limit, projectID)
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

// OrgActivityFilter narrows the cross-workflow activity feed read. OrgID is
// the required tenancy key (matched against workflow_runs.org_id, since
// activity_events.org_id is denormalised but the JOIN doubles as a defence-
// in-depth check). The other fields are optional and pass through as
// permissive SQL filters when set.
//
// Kind maps the operator-facing UI filter (`?kind=start|log|finish|error`)
// onto the canonical activity_events.status values. The mapping is
// intentionally loose so the SQL stays readable; see kindToStatuses.
type OrgActivityFilter struct {
	OrgID       string
	Since       time.Time // exclusive lower bound on ts; zero time disables
	Kind        string    // start | log | finish | error
	AgentRole   string    // exact match on activity_events.agent_role; empty disables
	WorkspaceID string    // exact match on workflow_runs.workspace_id; empty disables
	Limit       int       // default 50, max 200
	Offset      int       // default 0
}

// kindToStatuses converts the operator-facing kind filter into the
// activity_events.status values it should match. The mapping is documented
// alongside the constants in domain/workflow.go.
//
//	start  → 'started'
//	log    → 'retrying'  (replay/log-style frames)
//	finish → 'succeeded'
//	error  → 'failed' OR 'timed_out'
//
// Empty kind returns nil, which the caller treats as "no status filter".
func kindToStatuses(kind string) []string {
	switch kind {
	case "start":
		return []string{"started"}
	case "log":
		return []string{"retrying"}
	case "finish":
		return []string{"succeeded"}
	case "error":
		return []string{"failed", "timed_out"}
	default:
		return nil
	}
}

// ListOrgActivity returns the cross-workflow activity feed for one org,
// ordered most-recent first. Used by the operator-facing
// /v1/orgs/{org_id}/activity endpoint + its SSE companion.
//
// Runs on the admin pool — the operator endpoint authenticates via session
// middleware and the principal's OrgID is the tenancy boundary. The SQL
// JOINs workflow_runs so the workspace_id filter (and the second-line
// org_id defence) works against the canonical column.
//
// Returns the page rows + the unpaginated total so the dashboard can render
// "N of M" without an extra round-trip.
func (r *WorkflowRepo) ListOrgActivity(ctx context.Context, f OrgActivityFilter) ([]domain.ActivityEvent, int, error) {
	if r == nil || r.admin == nil {
		return []domain.ActivityEvent{}, 0, nil
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	var sinceArg any
	if !f.Since.IsZero() {
		sinceArg = f.Since
	}
	statuses := kindToStatuses(f.Kind)
	// Pass an explicit empty array when no status filter applies so the SQL
	// can guard with `(cardinality($N::text[])=0 OR ae.status = ANY($N::text[]))`.
	statusFilter := statuses
	if statusFilter == nil {
		statusFilter = []string{}
	}

	// Total (unpaginated) for the pager.
	var total int
	if err := r.admin.QueryRow(ctx, `
        SELECT count(*)
        FROM activity_events ae
        JOIN workflow_runs wr ON wr.id = ae.workflow_run_id
        WHERE wr.org_id = $1
          AND ($2::timestamptz IS NULL OR ae.ts > $2)
          AND ($3 = ''       OR ae.agent_role = $3)
          AND ($4 = ''       OR wr.workspace_id::text = $4)
          AND (cardinality($5::text[])=0 OR ae.status = ANY($5::text[]))`,
		f.OrgID, sinceArg, f.AgentRole, f.WorkspaceID, statusFilter,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []domain.ActivityEvent{}, 0, nil
	}

	rows, err := r.admin.Query(ctx, `
        SELECT ae.id, ae.org_id, ae.workflow_run_id, ae.seq, ae.agent_role, ae.activity_name,
               ae.status, ae.attempt, COALESCE(ae.message, ''), ae.payload, ae.ts
        FROM activity_events ae
        JOIN workflow_runs wr ON wr.id = ae.workflow_run_id
        WHERE wr.org_id = $1
          AND ($2::timestamptz IS NULL OR ae.ts > $2)
          AND ($3 = ''       OR ae.agent_role = $3)
          AND ($4 = ''       OR wr.workspace_id::text = $4)
          AND (cardinality($5::text[])=0 OR ae.status = ANY($5::text[]))
        ORDER BY ae.ts DESC, ae.seq DESC
        LIMIT $6 OFFSET $7`,
		f.OrgID, sinceArg, f.AgentRole, f.WorkspaceID, statusFilter, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]domain.ActivityEvent, 0, limit)
	for rows.Next() {
		var e domain.ActivityEvent
		var role, status string
		var payload []byte
		if err := rows.Scan(
			&e.ID, &e.OrgID, &e.WorkflowRunID, &e.Seq, &role, &e.ActivityName,
			&status, &e.Attempt, &e.Message, &payload, &e.TS,
		); err != nil {
			return nil, 0, err
		}
		e.AgentRole = domain.AgentRole(role)
		e.Status = domain.ActivityStatus(status)
		e.Payload = payload
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// ListOrgActivityAfter is the streaming companion to ListOrgActivity. Used
// by the SSE handler — fetches rows with ts strictly greater than `after`,
// in ascending order so the cursor can advance. Limit caps the per-tick
// batch so a backlog after a long disconnect can't blow up the broker.
func (r *WorkflowRepo) ListOrgActivityAfter(ctx context.Context, orgID string, after time.Time, limit int) ([]domain.ActivityEvent, error) {
	if r == nil || r.admin == nil {
		return []domain.ActivityEvent{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.admin.Query(ctx, `
        SELECT ae.id, ae.org_id, ae.workflow_run_id, ae.seq, ae.agent_role, ae.activity_name,
               ae.status, ae.attempt, COALESCE(ae.message, ''), ae.payload, ae.ts
        FROM activity_events ae
        JOIN workflow_runs wr ON wr.id = ae.workflow_run_id
        WHERE wr.org_id = $1 AND ae.ts > $2
        ORDER BY ae.ts ASC, ae.seq ASC
        LIMIT $3`, orgID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ActivityEvent, 0, limit)
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
