//go:build integration

package repo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type lineageTestFixture struct {
	t           *testing.T
	repo        *LineageRepo
	adminPool   *pgxpool.Pool
	appPool     *pgxpool.Pool
	orgID       string
	userID      string
	workspaceID string
	runID       string
}

// lineageFixture seeds an org + user + workspace + workflow_runs row via the
// admin pool (lineage_events FKs onto workflow_runs) and wires the repo
// dual-pool. No RLS tx is opened — the production writer is the Temporal
// activity goroutine, which inserts via the admin fallback exactly as here.
func lineageFixture(t *testing.T) (context.Context, *lineageTestFixture) {
	t.Helper()
	adminURL := os.Getenv("DATABASE_URL_TEST")
	if adminURL == "" {
		t.Skip("DATABASE_URL_TEST unset — skipping lineage repo test")
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
	var orgID, userID, workspaceID string
	if err := adminPool.QueryRow(ctx,
		`INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id::text`,
		"lin-"+stamp, "lin-"+stamp,
	).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := adminPool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, '$2a$10$abcdefghijklmnopqrstuv') RETURNING id::text`,
		"lin-"+stamp+"@test",
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := adminPool.Exec(ctx,
		`UPDATE organizations SET owner_user_id = $1 WHERE id = $2`, userID, orgID); err != nil {
		t.Fatalf("set org owner: %v", err)
	}
	if err := adminPool.QueryRow(ctx, `
		INSERT INTO workspaces (org_id, name, slug, region, status)
		VALUES ($1, $2, $3, 'us-east-1', 'ready')
		RETURNING id::text`,
		orgID, "ws-lin-"+stamp, "ws-lin-"+stamp,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	runID := uuid.NewString()
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO workflow_runs (id, org_id, workspace_id, workflow_type, temporal_run_id, temporal_wf_id, status)
		VALUES ($1, $2, $3, 'incident_recovery', $4, $5, 'running')`,
		runID, orgID, workspaceID, "trun-"+stamp, "twf-"+stamp,
	); err != nil {
		t.Fatalf("insert workflow_run: %v", err)
	}

	fix := &lineageTestFixture{
		t: t, repo: NewLineageRepo(appPool, adminPool),
		adminPool: adminPool, appPool: appPool,
		orgID: orgID, userID: userID, workspaceID: workspaceID, runID: runID,
	}
	t.Cleanup(fix.cleanup)
	return ctx, fix
}

func (f *lineageTestFixture) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM lineage_events WHERE org_id = $1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM workflow_runs WHERE org_id = $1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM workspaces WHERE id = $1`, f.workspaceID)
	_, _ = f.adminPool.Exec(ctx, `UPDATE organizations SET owner_user_id = NULL WHERE id = $1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM users WHERE id = $1`, f.userID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, f.orgID)
}

func (f *lineageTestFixture) event(jobName string, at time.Time) LineageEvent {
	return LineageEvent{
		OrgID:         f.orgID,
		WorkflowRunID: f.runID,
		EventType:     "COMPLETE",
		EventTime:     at,
		JobNamespace:  "nexis.data_engineer",
		JobName:       jobName,
		RunID:         uuid.NewString(),
		Event: map[string]any{
			"eventType": "COMPLETE",
			"eventTime": at.Format(time.RFC3339),
			"run":       map[string]any{"runId": uuid.NewString()},
			"job":       map[string]any{"namespace": "nexis.data_engineer", "name": jobName},
			"inputs":    []any{},
			"outputs":   []any{map[string]any{"namespace": "postgres://control-plane", "name": "orders"}},
		},
	}
}

// TestLineageRepo_Insert_RoundTrip persists one RunEvent and reads it back
// via ListByOrg, asserting the typed columns + the verbatim event JSON.
func TestLineageRepo_Insert_RoundTrip(t *testing.T) {
	ctx, fix := lineageFixture(t)

	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := fix.repo.Insert(ctx, fix.event("migration.propose", at)); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	events, total, err := fix.repo.ListByOrg(ctx, LineageFilter{OrgID: fix.orgID})
	if err != nil {
		t.Fatalf("ListByOrg: %v", err)
	}
	if total != 1 || len(events) != 1 {
		t.Fatalf("got total=%d events=%d, want 1/1", total, len(events))
	}
	got := events[0]
	if got.WorkflowRunID != fix.runID {
		t.Errorf("workflow_run_id = %q, want %q", got.WorkflowRunID, fix.runID)
	}
	if got.EventType != "COMPLETE" || got.JobNamespace != "nexis.data_engineer" || got.JobName != "migration.propose" {
		t.Errorf("typed columns lost: %+v", got)
	}
	if !got.EventTime.Equal(at) {
		t.Errorf("event_time = %v, want %v", got.EventTime, at)
	}
	if got.Event["eventType"] != "COMPLETE" {
		t.Errorf("event JSON lost: %+v", got.Event)
	}
	if _, err := uuid.Parse(got.RunID); err != nil {
		t.Errorf("run_id = %q not a uuid: %v", got.RunID, err)
	}
}

// TestLineageRepo_ListByOrg_Filtering — job_name + workflow_run_id filters
// narrow the page; a malformed workflow_run_id yields zero rows, not an
// error.
func TestLineageRepo_ListByOrg_Filtering(t *testing.T) {
	ctx, fix := lineageFixture(t)

	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := fix.repo.Insert(ctx, fix.event("migration.propose", at)); err != nil {
		t.Fatalf("Insert propose: %v", err)
	}
	if err := fix.repo.Insert(ctx, fix.event("migration.apply", at.Add(time.Second))); err != nil {
		t.Fatalf("Insert apply: %v", err)
	}

	_, total, err := fix.repo.ListByOrg(ctx, LineageFilter{OrgID: fix.orgID})
	if err != nil {
		t.Fatalf("ListByOrg all: %v", err)
	}
	if total != 2 {
		t.Fatalf("all total = %d, want 2", total)
	}

	events, total, err := fix.repo.ListByOrg(ctx, LineageFilter{OrgID: fix.orgID, JobName: "migration.apply"})
	if err != nil {
		t.Fatalf("ListByOrg job filter: %v", err)
	}
	if total != 1 || len(events) != 1 || events[0].JobName != "migration.apply" {
		t.Fatalf("job filter got total=%d events=%+v", total, events)
	}

	_, total, err = fix.repo.ListByOrg(ctx, LineageFilter{OrgID: fix.orgID, WorkflowRunID: fix.runID})
	if err != nil {
		t.Fatalf("ListByOrg run filter: %v", err)
	}
	if total != 2 {
		t.Fatalf("run filter total = %d, want 2", total)
	}

	_, total, err = fix.repo.ListByOrg(ctx, LineageFilter{OrgID: fix.orgID, WorkflowRunID: "not-a-uuid"})
	if err != nil {
		t.Fatalf("malformed run id must not error: %v", err)
	}
	if total != 0 {
		t.Fatalf("malformed run id total = %d, want 0", total)
	}
}

// TestLineageRepo_NilSafe — nil repo inserts and lists are safe no-ops.
func TestLineageRepo_NilSafe(t *testing.T) {
	var r *LineageRepo
	if err := r.Insert(context.Background(), LineageEvent{}); err != nil {
		t.Fatalf("nil-repo Insert: %v", err)
	}
	events, total, err := r.ListByOrg(context.Background(), LineageFilter{OrgID: "x"})
	if err != nil || total != 0 || len(events) != 0 {
		t.Fatalf("nil-repo ListByOrg got events=%d total=%d err=%v", len(events), total, err)
	}
}
