//go:build integration

package repo

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// workflowsTestFixture mirrors projects_repo_test.go's fixture: a dedicated
// org + workspace + user seeded via the admin pool, the WorkflowRepo wired
// against both pools, plus an RLS-bound tx pinned to the org. Cleanup drops
// every row the fixture created so the DB ends clean.
type workflowsTestFixture struct {
	t           *testing.T
	repo        *WorkflowRepo
	adminPool   *pgxpool.Pool
	appPool     *pgxpool.Pool
	tx          pgx.Tx
	orgID       string
	workspaceID string
	userID      string
}

func workflowsFixture(t *testing.T) (context.Context, *workflowsTestFixture) {
	t.Helper()
	adminURL := os.Getenv("DATABASE_URL_TEST")
	if adminURL == "" {
		t.Skip("DATABASE_URL_TEST unset — skipping workflows repo test")
	}
	appURL := os.Getenv("DATABASE_URL_TEST_APP")
	if appURL == "" {
		appURL = swapCredsToNexisApp(t, adminURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin pool: %v", err)
	}
	t.Cleanup(func() { adminPool.Close() })

	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatalf("connect app pool: %v", err)
	}
	t.Cleanup(func() { appPool.Close() })

	stamp := time.Now().UTC().Format("20060102150405.000000")
	email := "wf-repo-" + stamp + "@nexis.test"

	var orgID, userID, workspaceID string
	if err := adminPool.QueryRow(ctx, `
		INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id::text`,
		"wf-test-"+stamp, "wf-test-"+stamp,
	).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := adminPool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ($1, '$2a$10$abcdefghijklmnopqrstuv')
		RETURNING id::text`,
		email,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := adminPool.Exec(ctx,
		`UPDATE organizations SET owner_user_id = $1 WHERE id = $2`,
		userID, orgID); err != nil {
		t.Fatalf("set org owner: %v", err)
	}
	if _, err := adminPool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`,
		orgID, userID); err != nil {
		t.Fatalf("insert org_member: %v", err)
	}
	if err := adminPool.QueryRow(ctx, `
		INSERT INTO workspaces (org_id, name, slug, region, status)
		VALUES ($1, $2, $3, 'us-east-1', 'ready')
		RETURNING id::text`,
		orgID, "ws-wf-"+stamp, "ws-wf-"+stamp,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	repo := NewWorkflowRepo(appPool, adminPool)

	tx, err := appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin app tx: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", orgID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("pin app.current_org_id: %v", err)
	}

	fix := &workflowsTestFixture{
		t:           t,
		repo:        repo,
		adminPool:   adminPool,
		appPool:     appPool,
		tx:          tx,
		orgID:       orgID,
		workspaceID: workspaceID,
		userID:      userID,
	}
	t.Cleanup(func() { fix.cleanup() })
	return db.WithTx(ctx, tx), fix
}

func (f *workflowsTestFixture) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if f.tx != nil {
		_ = f.tx.Commit(ctx)
	}
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM activity_events WHERE org_id = $1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM workflow_runs WHERE org_id = $1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM workspaces WHERE id = $1`, f.workspaceID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM org_members WHERE org_id = $1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `UPDATE organizations SET owner_user_id = NULL WHERE id = $1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM users WHERE id = $1`, f.userID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, f.orgID)
}

func (f *workflowsTestFixture) Close() {}

// workflowsCommitAndReopen commits the fixture tx (so admin-pool reads see
// the rows) and opens a fresh RLS-bound tx pinned to the same orgID. Used by
// tests that mix app-pool writes with admin-pool reads.
func workflowsCommitAndReopen(t *testing.T, ctx context.Context, fix *workflowsTestFixture) context.Context {
	t.Helper()
	if fix.tx != nil {
		if err := fix.tx.Commit(ctx); err != nil {
			t.Fatalf("commit fixture tx: %v", err)
		}
	}
	tx, err := fix.appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("re-begin app tx: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", fix.orgID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("re-pin app.current_org_id: %v", err)
	}
	fix.tx = tx
	return db.WithTx(ctx, tx)
}

// TestWorkflowRepo_InsertRun_RoundTrip covers the full insert/get path
// including project_id stamping (NULLIF($15,'')::uuid) and the JSON columns.
func TestWorkflowRepo_InsertRun_RoundTrip(t *testing.T) {
	ctx, fix := workflowsFixture(t)
	defer fix.Close()

	runID := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)

	w := &domain.WorkflowRun{
		ID:            runID,
		OrgID:         fix.orgID,
		WorkspaceID:   fix.workspaceID,
		WorkflowType:  "incident_recovery",
		TemporalRunID: "trun-1",
		TemporalWfID:  "twf-1",
		Status:        domain.WorkflowRunStatus("running"),
		CurrentStep:   "synthesise",
		Input:         []byte(`{"foo":"bar"}`),
		Output:        nil,
		Error:         "",
		StartedAt:     now,
		CreatedBy:     fix.userID,
	}
	if err := fix.repo.InsertRun(ctx, w); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	got, err := fix.repo.GetRun(ctx, fix.orgID, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.ID != runID || got.OrgID != fix.orgID || got.WorkspaceID != fix.workspaceID {
		t.Fatalf("identity drift: %+v", got)
	}
	if got.WorkflowType != "incident_recovery" || got.CurrentStep != "synthesise" {
		t.Fatalf("string fields lost: %+v", got)
	}
	if string(got.Input) != `{"foo": "bar"}` && string(got.Input) != `{"foo":"bar"}` {
		t.Fatalf("input JSON did not round-trip: %s", got.Input)
	}
	if got.CreatedBy != fix.userID {
		t.Fatalf("created_by lost: got %q want %q", got.CreatedBy, fix.userID)
	}
}

// TestWorkflowRepo_InsertRun_EmptyProjectIDLandsAsNull confirms the
// NULLIF($15,'')::uuid coercion: an empty ProjectID must NOT crash the cast.
func TestWorkflowRepo_InsertRun_EmptyProjectIDLandsAsNull(t *testing.T) {
	ctx, fix := workflowsFixture(t)
	defer fix.Close()

	runID := uuid.NewString()
	w := &domain.WorkflowRun{
		ID:            runID,
		OrgID:         fix.orgID,
		WorkspaceID:   fix.workspaceID,
		WorkflowType:  "noop",
		TemporalRunID: "trun-2",
		TemporalWfID:  "twf-2",
		Status:        domain.WorkflowRunStatus("queued"),
		StartedAt:     time.Now().UTC().Truncate(time.Microsecond),
		ProjectID:     "", // empty must NULLIF to NULL — would crash if cast directly
	}
	if err := fix.repo.InsertRun(ctx, w); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	// Verify project_id is NULL in the DB via admin pool.
	ctx = workflowsCommitAndReopen(t, ctx, fix)
	var projectID *string
	if err := fix.adminPool.QueryRow(ctx,
		`SELECT project_id::text FROM workflow_runs WHERE id=$1`, runID,
	).Scan(&projectID); err != nil {
		t.Fatalf("verify project_id: %v", err)
	}
	if projectID != nil {
		t.Fatalf("expected NULL project_id, got %q", *projectID)
	}
}

// TestWorkflowRepo_UpdateTemporalIDs writes IDs after the insert lands.
func TestWorkflowRepo_UpdateTemporalIDs(t *testing.T) {
	ctx, fix := workflowsFixture(t)
	defer fix.Close()

	runID := uuid.NewString()
	w := &domain.WorkflowRun{
		ID:            runID,
		OrgID:         fix.orgID,
		WorkspaceID:   fix.workspaceID,
		WorkflowType:  "noop",
		Status:        domain.WorkflowRunStatus("running"),
		TemporalRunID: "pending",
		TemporalWfID:  "pending",
		StartedAt:     time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := fix.repo.InsertRun(ctx, w); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	if err := fix.repo.UpdateTemporalIDs(ctx, runID, "real-wf-id", "real-run-id"); err != nil {
		t.Fatalf("UpdateTemporalIDs: %v", err)
	}
	got, err := fix.repo.GetRun(ctx, fix.orgID, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.TemporalWfID != "real-wf-id" || got.TemporalRunID != "real-run-id" {
		t.Fatalf("temporal ids did not update: %+v", got)
	}
}

// TestWorkflowRepo_UpdateRunStatus moves a row to a terminal state via the
// admin pool, then verifies duration_ms is computed from started_at.
func TestWorkflowRepo_UpdateRunStatus(t *testing.T) {
	ctx, fix := workflowsFixture(t)
	defer fix.Close()

	runID := uuid.NewString()
	start := time.Now().UTC().Add(-5 * time.Second).Truncate(time.Microsecond)
	w := &domain.WorkflowRun{
		ID:            runID,
		OrgID:         fix.orgID,
		WorkspaceID:   fix.workspaceID,
		WorkflowType:  "noop",
		Status:        domain.WorkflowRunStatus("running"),
		TemporalRunID: "trun-3",
		TemporalWfID:  "twf-3",
		StartedAt:     start,
	}
	if err := fix.repo.InsertRun(ctx, w); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	// Commit so the admin-pool UPDATE in UpdateRunStatus can see the row.
	ctx = workflowsCommitAndReopen(t, ctx, fix)

	completedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := fix.repo.UpdateRunStatus(ctx, runID, domain.WorkflowRunStatus("succeeded"),
		"done", "", []byte(`{"ok":true}`), &completedAt); err != nil {
		t.Fatalf("UpdateRunStatus: %v", err)
	}

	got, err := fix.repo.GetRun(ctx, fix.orgID, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if string(got.Status) != "succeeded" {
		t.Fatalf("status=%q want succeeded", got.Status)
	}
	if got.CompletedAt == nil {
		t.Fatalf("completed_at should be populated")
	}
	if got.DurationMs == nil || *got.DurationMs < 4000 {
		t.Fatalf("duration_ms not computed; got %v", got.DurationMs)
	}
}

// TestWorkflowRepo_ListRuns_OrdersByStartedAtDesc inserts three rows with
// distinct started_at timestamps and asserts list returns newest first.
func TestWorkflowRepo_ListRuns_OrdersByStartedAtDesc(t *testing.T) {
	ctx, fix := workflowsFixture(t)
	defer fix.Close()

	base := time.Now().UTC().Truncate(time.Microsecond)
	for i, offset := range []time.Duration{-3 * time.Second, -2 * time.Second, -1 * time.Second} {
		w := &domain.WorkflowRun{
			ID:            uuid.NewString(),
			OrgID:         fix.orgID,
			WorkspaceID:   fix.workspaceID,
			WorkflowType:  "list-test",
			Status:        domain.WorkflowRunStatus("running"),
			TemporalRunID: "trun-list-" + strings.Repeat("x", i+1),
			TemporalWfID:  "twf-list-" + strings.Repeat("x", i+1),
			StartedAt:     base.Add(offset),
		}
		if err := fix.repo.InsertRun(ctx, w); err != nil {
			t.Fatalf("InsertRun[%d]: %v", i, err)
		}
	}

	rows, err := fix.repo.ListRuns(ctx, fix.orgID, fix.workspaceID, "", 10, time.Time{})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(rows) < 3 {
		t.Fatalf("ListRuns returned %d rows, want >= 3", len(rows))
	}
	// rows[0].started_at must be the most recent (closest to base).
	for i := 1; i < len(rows); i++ {
		if rows[i].StartedAt.After(rows[i-1].StartedAt) {
			t.Fatalf("ListRuns not ordered DESC at i=%d: %v vs %v", i,
				rows[i].StartedAt, rows[i-1].StartedAt)
		}
	}
}

// TestWorkflowRepo_GetRun_NotFound returns domain.ErrNotFound for an unknown id.
func TestWorkflowRepo_GetRun_NotFound(t *testing.T) {
	ctx, fix := workflowsFixture(t)
	defer fix.Close()

	missing := uuid.NewString()
	_, err := fix.repo.GetRun(ctx, fix.orgID, missing)
	if err == nil {
		t.Fatalf("expected ErrNotFound, got nil")
	}
	if err != domain.ErrNotFound {
		t.Fatalf("expected domain.ErrNotFound, got %v", err)
	}
}

// TestWorkflowRepo_InsertEvent_AndListEvents inserts a workflow run, two
// activity events (seq 1 and 2), and asserts both ListEvents and NextSeq.
func TestWorkflowRepo_InsertEvent_AndListEvents(t *testing.T) {
	ctx, fix := workflowsFixture(t)
	defer fix.Close()

	runID := uuid.NewString()
	w := &domain.WorkflowRun{
		ID:            runID,
		OrgID:         fix.orgID,
		WorkspaceID:   fix.workspaceID,
		WorkflowType:  "ev-test",
		Status:        domain.WorkflowRunStatus("running"),
		TemporalRunID: "trun-ev",
		TemporalWfID:  "twf-ev",
		StartedAt:     time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := fix.repo.InsertRun(ctx, w); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	// Commit so admin-pool InsertEvent can resolve the workflow_run_id FK.
	ctx = workflowsCommitAndReopen(t, ctx, fix)

	now := time.Now().UTC().Truncate(time.Microsecond)
	e1 := &domain.ActivityEvent{
		OrgID:         fix.orgID,
		WorkflowRunID: runID,
		Seq:           1,
		AgentRole:     domain.AgentSentinel,
		ActivityName:  "detect",
		Status:        domain.ActivityStatus("started"),
		Attempt:       1,
		Message:       "hello",
		Payload:       []byte(`{"k":"v"}`),
		TS:            now,
	}
	e2 := &domain.ActivityEvent{
		OrgID:         fix.orgID,
		WorkflowRunID: runID,
		Seq:           2,
		AgentRole:     domain.AgentSentinel,
		ActivityName:  "detect",
		Status:        domain.ActivityStatus("succeeded"),
		Attempt:       1,
		Message:       "ok",
		TS:            now.Add(time.Millisecond),
	}
	if err := fix.repo.InsertEvent(ctx, e1); err != nil {
		t.Fatalf("InsertEvent e1: %v", err)
	}
	if err := fix.repo.InsertEvent(ctx, e2); err != nil {
		t.Fatalf("InsertEvent e2: %v", err)
	}

	// ON CONFLICT (workflow_run_id, seq) DO NOTHING — second insert of e1 is a no-op.
	if err := fix.repo.InsertEvent(ctx, e1); err != nil {
		t.Fatalf("InsertEvent dup-e1: %v", err)
	}

	events, err := fix.repo.ListEvents(ctx, runID, 0, 50)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ListEvents got %d, want 2 (dup must have been ignored)", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("ListEvents not ASC by seq: %+v", events)
	}

	// NextSeq is MAX(seq)+1.
	n, err := fix.repo.NextSeq(ctx, runID)
	if err != nil {
		t.Fatalf("NextSeq: %v", err)
	}
	if n != 3 {
		t.Fatalf("NextSeq got %d, want 3", n)
	}

	// ListEvents with sinceSeq filters out earlier rows.
	later, err := fix.repo.ListEvents(ctx, runID, 1, 50)
	if err != nil {
		t.Fatalf("ListEvents(sinceSeq=1): %v", err)
	}
	if len(later) != 1 || later[0].Seq != 2 {
		t.Fatalf("sinceSeq=1 expected seq=2 only, got %+v", later)
	}
}

// TestWorkflowRepo_NextSeq_EmptyReturns1 covers the COALESCE(MAX,0)+1 edge
// when there are no rows for the run yet.
func TestWorkflowRepo_NextSeq_EmptyReturns1(t *testing.T) {
	ctx, fix := workflowsFixture(t)
	defer fix.Close()

	// Use a uuid that has no events.
	n, err := fix.repo.NextSeq(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("NextSeq empty: %v", err)
	}
	if n != 1 {
		t.Fatalf("NextSeq empty got %d, want 1", n)
	}
}

// TestWorkflowRepo_ListOrgActivity_Pagination inserts 5 events across two
// workflow runs in the same org and verifies the cross-workflow feed +
// total + paging behaviour.
func TestWorkflowRepo_ListOrgActivity_Pagination(t *testing.T) {
	ctx, fix := workflowsFixture(t)
	defer fix.Close()

	// Two workflow runs in the same workspace.
	run1ID := uuid.NewString()
	run2ID := uuid.NewString()
	for _, id := range []string{run1ID, run2ID} {
		w := &domain.WorkflowRun{
			ID:            id,
			OrgID:         fix.orgID,
			WorkspaceID:   fix.workspaceID,
			WorkflowType:  "feed-test",
			Status:        domain.WorkflowRunStatus("running"),
			TemporalRunID: "trun-feed-" + id,
			TemporalWfID:  "twf-feed-" + id,
			StartedAt:     time.Now().UTC().Truncate(time.Microsecond),
		}
		if err := fix.repo.InsertRun(ctx, w); err != nil {
			t.Fatalf("InsertRun(%s): %v", id, err)
		}
	}
	ctx = workflowsCommitAndReopen(t, ctx, fix)

	// 3 events on run1, 2 on run2 — across multiple statuses.
	base := time.Now().UTC().Truncate(time.Microsecond)
	events := []domain.ActivityEvent{
		{OrgID: fix.orgID, WorkflowRunID: run1ID, Seq: 1, AgentRole: domain.AgentSentinel, ActivityName: "a", Status: domain.ActivityStatus("started"), Attempt: 1, TS: base.Add(-4 * time.Second)},
		{OrgID: fix.orgID, WorkflowRunID: run1ID, Seq: 2, AgentRole: domain.AgentSentinel, ActivityName: "a", Status: domain.ActivityStatus("succeeded"), Attempt: 1, TS: base.Add(-3 * time.Second)},
		{OrgID: fix.orgID, WorkflowRunID: run1ID, Seq: 3, AgentRole: domain.AgentSentinel, ActivityName: "a", Status: domain.ActivityStatus("failed"), Attempt: 1, TS: base.Add(-2 * time.Second)},
		{OrgID: fix.orgID, WorkflowRunID: run2ID, Seq: 1, AgentRole: domain.AgentBackend, ActivityName: "b", Status: domain.ActivityStatus("started"), Attempt: 1, TS: base.Add(-1 * time.Second)},
		{OrgID: fix.orgID, WorkflowRunID: run2ID, Seq: 2, AgentRole: domain.AgentBackend, ActivityName: "b", Status: domain.ActivityStatus("succeeded"), Attempt: 1, TS: base},
	}
	for i := range events {
		if err := fix.repo.InsertEvent(ctx, &events[i]); err != nil {
			t.Fatalf("InsertEvent[%d]: %v", i, err)
		}
	}

	// Full feed — should have all 5 rows.
	rows, total, err := fix.repo.ListOrgActivity(ctx, OrgActivityFilter{OrgID: fix.orgID, Limit: 100})
	if err != nil {
		t.Fatalf("ListOrgActivity: %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rows))
	}

	// Filter by agent_role.
	rows, total, err = fix.repo.ListOrgActivity(ctx, OrgActivityFilter{OrgID: fix.orgID, AgentRole: string(domain.AgentBackend), Limit: 100})
	if err != nil {
		t.Fatalf("ListOrgActivity backend: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("backend filter got total=%d rows=%d, want 2/2", total, len(rows))
	}

	// kind=finish → status='succeeded' (2 rows).
	rows, total, err = fix.repo.ListOrgActivity(ctx, OrgActivityFilter{OrgID: fix.orgID, Kind: "finish", Limit: 100})
	if err != nil {
		t.Fatalf("ListOrgActivity finish: %v", err)
	}
	if total != 2 {
		t.Fatalf("kind=finish total=%d want 2", total)
	}

	// kind=error → failed + timed_out (1 row).
	rows, total, err = fix.repo.ListOrgActivity(ctx, OrgActivityFilter{OrgID: fix.orgID, Kind: "error", Limit: 100})
	if err != nil {
		t.Fatalf("ListOrgActivity error: %v", err)
	}
	if total != 1 {
		t.Fatalf("kind=error total=%d want 1", total)
	}

	// Workspace filter — only rows for our workspace_id.
	rows, total, err = fix.repo.ListOrgActivity(ctx, OrgActivityFilter{OrgID: fix.orgID, WorkspaceID: fix.workspaceID, Limit: 100})
	if err != nil {
		t.Fatalf("ListOrgActivity ws-filter: %v", err)
	}
	if total != 5 {
		t.Fatalf("ws-filter total=%d want 5", total)
	}

	// Limit + offset paging — 3+2.
	page1, _, err := fix.repo.ListOrgActivity(ctx, OrgActivityFilter{OrgID: fix.orgID, Limit: 3, Offset: 0})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("page1 len=%d want 3", len(page1))
	}
	page2, _, err := fix.repo.ListOrgActivity(ctx, OrgActivityFilter{OrgID: fix.orgID, Limit: 3, Offset: 3})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len=%d want 2", len(page2))
	}
}

// TestWorkflowRepo_ListOrgActivity_NilRepoIsSafe is the nil-pool guard.
func TestWorkflowRepo_ListOrgActivity_NilRepoIsSafe(t *testing.T) {
	var nilRepo *WorkflowRepo
	rows, total, err := nilRepo.ListOrgActivity(context.Background(), OrgActivityFilter{OrgID: "x"})
	if err != nil {
		t.Fatalf("nil-repo err: %v", err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("nil-repo got total=%d rows=%d, want 0/0", total, len(rows))
	}

	rows, err = nilRepo.ListOrgActivityAfter(context.Background(), "x", time.Now(), 10)
	if err != nil {
		t.Fatalf("nil-repo after err: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("nil-repo after rows=%d want 0", len(rows))
	}
}

// TestWorkflowRepo_ListOrgActivityAfter is the SSE companion's happy path.
func TestWorkflowRepo_ListOrgActivityAfter(t *testing.T) {
	ctx, fix := workflowsFixture(t)
	defer fix.Close()

	runID := uuid.NewString()
	w := &domain.WorkflowRun{
		ID:            runID,
		OrgID:         fix.orgID,
		WorkspaceID:   fix.workspaceID,
		WorkflowType:  "after-test",
		Status:        domain.WorkflowRunStatus("running"),
		TemporalRunID: "trun-after",
		TemporalWfID:  "twf-after",
		StartedAt:     time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := fix.repo.InsertRun(ctx, w); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	ctx = workflowsCommitAndReopen(t, ctx, fix)

	base := time.Now().UTC().Truncate(time.Microsecond)
	for i := 1; i <= 3; i++ {
		e := &domain.ActivityEvent{
			OrgID:         fix.orgID,
			WorkflowRunID: runID,
			Seq:           i,
			AgentRole:     domain.AgentSentinel,
			ActivityName:  "stream",
			Status:        domain.ActivityStatus("started"),
			Attempt:       1,
			TS:            base.Add(time.Duration(i) * time.Second),
		}
		if err := fix.repo.InsertEvent(ctx, e); err != nil {
			t.Fatalf("InsertEvent[%d]: %v", i, err)
		}
	}

	// After base+1.5s should get seq 2 and 3.
	rows, err := fix.repo.ListOrgActivityAfter(ctx, fix.orgID, base.Add(1500*time.Millisecond), 10)
	if err != nil {
		t.Fatalf("ListOrgActivityAfter: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListOrgActivityAfter got %d, want 2", len(rows))
	}
	// Ascending order.
	if !rows[0].TS.Before(rows[1].TS) {
		t.Fatalf("ListOrgActivityAfter not ASC: %+v", rows)
	}
}

// TestKindToStatuses exercises the operator-facing kind→status mapping.
func TestKindToStatuses(t *testing.T) {
	cases := map[string][]string{
		"start":  {"started"},
		"log":    {"retrying"},
		"finish": {"succeeded"},
		"error":  {"failed", "timed_out"},
		"":       nil,
		"bogus":  nil,
	}
	for in, want := range cases {
		got := kindToStatuses(in)
		if !sliceEqual(got, want) {
			t.Errorf("kindToStatuses(%q) = %v, want %v", in, got, want)
		}
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestJsonOrNil + nullTime: pure helpers.
func TestJsonOrNil_AndNullTime(t *testing.T) {
	if jsonOrNil(nil) != nil {
		t.Fatalf("jsonOrNil(nil) should return nil")
	}
	if jsonOrNil([]byte{}) != nil {
		t.Fatalf("jsonOrNil(empty) should return nil")
	}
	if jsonOrNil([]byte(`{"a":1}`)) == nil {
		t.Fatalf("jsonOrNil(non-empty) should return value")
	}
	if nullTime(time.Time{}) != nil {
		t.Fatalf("nullTime(zero) should return nil")
	}
	now := time.Now()
	if nullTime(now) == nil {
		t.Fatalf("nullTime(non-zero) should return value")
	}
}

// swapCredsToNexisApp duplicates the helper in projects_repo_test.go (kept
// here to avoid renaming, since both files live in package repo).
func swapCredsToNexisApp(t *testing.T, src string) string {
	t.Helper()
	const scheme = "postgres://"
	if !strings.HasPrefix(src, scheme) {
		t.Fatalf("DATABASE_URL_TEST not a postgres:// URL: %q", src)
	}
	rest := src[len(scheme):]
	at := strings.Index(rest, "@")
	if at < 0 {
		t.Fatalf("DATABASE_URL_TEST missing credentials: %q", src)
	}
	return scheme + "nexis_app:nexis_app_dev_password@" + rest[at+1:]
}
