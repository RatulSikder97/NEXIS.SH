//go:build integration

package repo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

type approvalTestFixture struct {
	t           *testing.T
	repo        *ApprovalRepo
	wfRepo      *WorkflowRepo
	adminPool   *pgxpool.Pool
	appPool     *pgxpool.Pool
	tx          pgx.Tx
	orgID       string
	workspaceID string
	userID      string
}

func approvalFixture(t *testing.T) (context.Context, *approvalTestFixture) {
	t.Helper()
	adminURL := os.Getenv("DATABASE_URL_TEST")
	if adminURL == "" {
		t.Skip("DATABASE_URL_TEST unset — skipping approval repo test")
	}
	appURL := os.Getenv("DATABASE_URL_TEST_APP")
	if appURL == "" {
		appURL = swapCredsToNexisApp(t, adminURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	t.Cleanup(func() { adminPool.Close() })

	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatalf("connect app: %v", err)
	}
	t.Cleanup(func() { appPool.Close() })

	stamp := time.Now().UTC().Format("20060102150405.000000")
	var orgID, userID, workspaceID string
	if err := adminPool.QueryRow(ctx,
		`INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id::text`,
		"app-"+stamp, "app-"+stamp,
	).Scan(&orgID); err != nil {
		t.Fatalf("org: %v", err)
	}
	if err := adminPool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, '$2a$10$abcdefghijklmnopqrstuv') RETURNING id::text`,
		"app-"+stamp+"@test",
	).Scan(&userID); err != nil {
		t.Fatalf("user: %v", err)
	}
	if _, err := adminPool.Exec(ctx,
		`UPDATE organizations SET owner_user_id = $1 WHERE id = $2`, userID, orgID); err != nil {
		t.Fatalf("owner: %v", err)
	}
	if err := adminPool.QueryRow(ctx,
		`INSERT INTO workspaces (org_id, name, slug, region, status) VALUES ($1, $2, $3, 'us-east-1', 'ready') RETURNING id::text`,
		orgID, "app-ws-"+stamp, "app-ws-"+stamp,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("ws: %v", err)
	}

	repo := NewApprovalRepo(appPool, adminPool)
	wfRepo := NewWorkflowRepo(appPool, adminPool)

	tx, err := appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", orgID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("pin: %v", err)
	}

	fix := &approvalTestFixture{
		t: t, repo: repo, wfRepo: wfRepo,
		adminPool: adminPool, appPool: appPool,
		tx: tx, orgID: orgID, workspaceID: workspaceID, userID: userID,
	}
	t.Cleanup(fix.cleanup)
	return db.WithTx(ctx, tx), fix
}

func (f *approvalTestFixture) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if f.tx != nil {
		_ = f.tx.Commit(ctx)
	}
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM approval_decisions WHERE org_id=$1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM activity_events WHERE org_id=$1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM workflow_runs WHERE org_id=$1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, f.workspaceID)
	_, _ = f.adminPool.Exec(ctx, `UPDATE organizations SET owner_user_id = NULL WHERE id=$1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM users WHERE id=$1`, f.userID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, f.orgID)
}

func (f *approvalTestFixture) commitAndReopen(ctx context.Context) context.Context {
	f.t.Helper()
	if f.tx != nil {
		if err := f.tx.Commit(ctx); err != nil {
			f.t.Fatalf("commit: %v", err)
		}
	}
	tx, err := f.appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		f.t.Fatalf("re-begin: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", f.orgID); err != nil {
		_ = tx.Rollback(ctx)
		f.t.Fatalf("re-pin: %v", err)
	}
	f.tx = tx
	return db.WithTx(ctx, tx)
}

// createWorkflowRunForApproval seeds a workflow_runs row that the
// approval_decisions FK can target. Must run inside the fixture's RLS tx so
// the INSERT obeys the tenant policy. Returns the new run id.
func (f *approvalTestFixture) createWorkflowRunForApproval(ctx context.Context) string {
	f.t.Helper()
	runID := uuid.NewString()
	if err := f.wfRepo.InsertRun(ctx, &domain.WorkflowRun{
		ID:            runID,
		OrgID:         f.orgID,
		WorkspaceID:   f.workspaceID,
		WorkflowType:  "approval-test",
		Status:        domain.WorkflowRunStatus("running"),
		TemporalRunID: "trun-" + runID,
		TemporalWfID:  "twf-" + runID,
		StartedAt:     time.Now().UTC().Truncate(time.Microsecond),
	}); err != nil {
		f.t.Fatalf("seed workflow run: %v", err)
	}
	return runID
}

// TestApprovalRepo_Create_RoundTrip exercises the basic Create + GetByRun
// path. Create writes via admin pool; GetByRun reads via the request tx so
// the RLS policy still has the GUC pinned.
func TestApprovalRepo_Create_RoundTrip(t *testing.T) {
	ctx, fix := approvalFixture(t)
	runID := fix.createWorkflowRunForApproval(ctx)
	// Commit so the admin-pool Create can resolve the workflow_run_id FK.
	ctx = fix.commitAndReopen(ctx)

	id, err := fix.repo.Create(ctx, domain.ApprovalDecision{
		OrgID:         fix.orgID,
		WorkspaceID:   fix.workspaceID,
		WorkflowRunID: runID,
		Severity:      domain.SeverityHigh,
		Scenario:      "production-rollback",
		RiskScore:     42.50,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == "" {
		t.Fatalf("expected non-empty id")
	}

	got, err := fix.repo.GetByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetByRun: %v", err)
	}
	if got.WorkflowRunID != runID {
		t.Fatalf("workflow_run_id drift: %q vs %q", got.WorkflowRunID, runID)
	}
	if got.Severity != domain.SeverityHigh {
		t.Fatalf("severity: %q", got.Severity)
	}
	if got.Decision != domain.ApprovalPending {
		t.Fatalf("default decision: %q", got.Decision)
	}
	if got.Scenario != "production-rollback" {
		t.Fatalf("scenario: %q", got.Scenario)
	}
	if got.RiskScore != 42.50 {
		t.Fatalf("risk_score: %v", got.RiskScore)
	}
}

// TestApprovalRepo_Create_DefaultsToPending — empty Decision in input defaults to "pending".
func TestApprovalRepo_Create_DefaultsToPending(t *testing.T) {
	ctx, fix := approvalFixture(t)
	runID := fix.createWorkflowRunForApproval(ctx)
	ctx = fix.commitAndReopen(ctx)

	_, err := fix.repo.Create(ctx, domain.ApprovalDecision{
		OrgID:         fix.orgID,
		WorkspaceID:   fix.workspaceID,
		WorkflowRunID: runID,
		Severity:      domain.SeverityLow,
		// Decision intentionally left empty
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := fix.repo.GetByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetByRun: %v", err)
	}
	if got.Decision != domain.ApprovalPending {
		t.Fatalf("decision default = %q, want pending", got.Decision)
	}
}

// TestApprovalRepo_GetByRun_NotFound returns domain.ErrNotFound on miss.
func TestApprovalRepo_GetByRun_NotFound(t *testing.T) {
	ctx, fix := approvalFixture(t)

	_, err := fix.repo.GetByRun(ctx, uuid.NewString())
	if err == nil {
		t.Fatalf("expected error")
	}
	if err != domain.ErrNotFound {
		t.Fatalf("got %v, want domain.ErrNotFound", err)
	}
}

// TestApprovalRepo_UpdateDecision flips pending → approved with decidedBy.
func TestApprovalRepo_UpdateDecision(t *testing.T) {
	ctx, fix := approvalFixture(t)
	runID := fix.createWorkflowRunForApproval(ctx)
	ctx = fix.commitAndReopen(ctx)

	if _, err := fix.repo.Create(ctx, domain.ApprovalDecision{
		OrgID: fix.orgID, WorkspaceID: fix.workspaceID, WorkflowRunID: runID,
		Severity: domain.SeverityMedium,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := fix.repo.UpdateDecision(ctx, runID, domain.ApprovalApproved, fix.userID, "lgtm", at); err != nil {
		t.Fatalf("UpdateDecision: %v", err)
	}
	got, err := fix.repo.GetByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetByRun: %v", err)
	}
	if got.Decision != domain.ApprovalApproved {
		t.Fatalf("decision: %q", got.Decision)
	}
	if got.DecidedBy != fix.userID {
		t.Fatalf("decided_by: %q", got.DecidedBy)
	}
	if got.Notes != "lgtm" {
		t.Fatalf("notes: %q", got.Notes)
	}
	if !got.DecidedAt.Equal(at) {
		t.Fatalf("decided_at: got %v want %v", got.DecidedAt, at)
	}
}

// TestApprovalRepo_UpdateDecision_AutoApprove_EmptyDecidedByLandsAsNull.
func TestApprovalRepo_UpdateDecision_AutoApprove(t *testing.T) {
	ctx, fix := approvalFixture(t)
	runID := fix.createWorkflowRunForApproval(ctx)
	ctx = fix.commitAndReopen(ctx)

	if _, err := fix.repo.Create(ctx, domain.ApprovalDecision{
		OrgID: fix.orgID, WorkspaceID: fix.workspaceID, WorkflowRunID: runID,
		Severity: domain.SeverityLow,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := fix.repo.UpdateDecision(ctx, runID, domain.ApprovalAutoApproved, "", "", at); err != nil {
		t.Fatalf("UpdateDecision: %v", err)
	}
	// Direct admin-pool peek — decided_by should be NULL.
	var decidedBy *string
	if err := fix.adminPool.QueryRow(ctx,
		`SELECT decided_by::text FROM approval_decisions WHERE workflow_run_id=$1`, runID,
	).Scan(&decidedBy); err != nil {
		t.Fatalf("peek: %v", err)
	}
	if decidedBy != nil {
		t.Fatalf("decided_by should be NULL for auto-approve, got %q", *decidedBy)
	}
}

// TestApprovalRepo_ListPending returns pending rows newest first.
func TestApprovalRepo_ListPending(t *testing.T) {
	ctx, fix := approvalFixture(t)
	// Three pending + one approved across separate runs.
	pendingRuns := []string{}
	for i := 0; i < 3; i++ {
		runID := fix.createWorkflowRunForApproval(ctx)
		pendingRuns = append(pendingRuns, runID)
	}
	approvedRun := fix.createWorkflowRunForApproval(ctx)

	ctx = fix.commitAndReopen(ctx)

	for _, runID := range pendingRuns {
		if _, err := fix.repo.Create(ctx, domain.ApprovalDecision{
			OrgID: fix.orgID, WorkspaceID: fix.workspaceID, WorkflowRunID: runID,
			Severity: domain.SeverityMedium,
		}); err != nil {
			t.Fatalf("Create pending: %v", err)
		}
	}
	if _, err := fix.repo.Create(ctx, domain.ApprovalDecision{
		OrgID: fix.orgID, WorkspaceID: fix.workspaceID, WorkflowRunID: approvedRun,
		Severity: domain.SeverityLow,
	}); err != nil {
		t.Fatalf("Create non-pending: %v", err)
	}
	if err := fix.repo.UpdateDecision(ctx, approvedRun, domain.ApprovalApproved,
		fix.userID, "yes", time.Now().UTC().Truncate(time.Microsecond)); err != nil {
		t.Fatalf("UpdateDecision: %v", err)
	}

	rows, err := fix.repo.ListPending(ctx, fix.orgID, 100)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("ListPending got %d want 3", len(rows))
	}
	for _, r := range rows {
		if r.Decision != domain.ApprovalPending {
			t.Fatalf("non-pending row leaked: %+v", r)
		}
	}
	// Limit defaults to 50 when <= 0.
	rows, err = fix.repo.ListPending(ctx, fix.orgID, 0)
	if err != nil {
		t.Fatalf("ListPending default: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("default limit got %d want 3", len(rows))
	}
}
