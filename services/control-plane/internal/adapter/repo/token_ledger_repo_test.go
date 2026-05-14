//go:build integration

package repo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

type tokenLedgerTestFixture struct {
	t         *testing.T
	repo      *TokenLedgerRepo
	adminPool *pgxpool.Pool
	appPool   *pgxpool.Pool
	tx        pgx.Tx
	orgID     string
	userID    string
}

func tokenLedgerFixture(t *testing.T, cfg TokenLedgerConfig) (context.Context, *tokenLedgerTestFixture) {
	t.Helper()
	adminURL := os.Getenv("DATABASE_URL_TEST")
	if adminURL == "" {
		t.Skip("DATABASE_URL_TEST unset — skipping token ledger test")
	}
	appURL := os.Getenv("DATABASE_URL_TEST_APP")
	if appURL == "" {
		appURL = swapCredsToNexisApp(t, adminURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("admin: %v", err)
	}
	t.Cleanup(func() { adminPool.Close() })

	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatalf("app: %v", err)
	}
	t.Cleanup(func() { appPool.Close() })

	stamp := time.Now().UTC().Format("20060102150405.000000")
	var orgID, userID string
	if err := adminPool.QueryRow(ctx,
		`INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id::text`,
		"tl-"+stamp, "tl-"+stamp,
	).Scan(&orgID); err != nil {
		t.Fatalf("org: %v", err)
	}
	if err := adminPool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, '$2a$10$abcdefghijklmnopqrstuv') RETURNING id::text`,
		"tl-"+stamp+"@test",
	).Scan(&userID); err != nil {
		t.Fatalf("user: %v", err)
	}
	if _, err := adminPool.Exec(ctx,
		`UPDATE organizations SET owner_user_id = $1 WHERE id = $2`, userID, orgID); err != nil {
		t.Fatalf("owner: %v", err)
	}

	repo := NewTokenLedgerRepo(appPool, adminPool, cfg)

	tx, err := appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", orgID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("pin: %v", err)
	}

	fix := &tokenLedgerTestFixture{
		t: t, repo: repo, adminPool: adminPool, appPool: appPool,
		tx: tx, orgID: orgID, userID: userID,
	}
	t.Cleanup(fix.cleanup)
	return db.WithTx(ctx, tx), fix
}

func (f *tokenLedgerTestFixture) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if f.tx != nil {
		_ = f.tx.Commit(ctx)
	}
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM token_ledger WHERE org_id=$1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM token_budgets WHERE org_id=$1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `UPDATE organizations SET owner_user_id = NULL WHERE id=$1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM users WHERE id=$1`, f.userID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, f.orgID)
}

// TestTokenLedgerRepo_GetBudget_CreatesIfMissing — first call lazily inserts.
func TestTokenLedgerRepo_GetBudget_CreatesIfMissing(t *testing.T) {
	ctx, fix := tokenLedgerFixture(t, TokenLedgerConfig{
		AllowedTokensIn: 1000, AllowedTokensOut: 2000, PeriodDays: 30,
	})

	b, err := fix.repo.GetBudget(ctx, fix.orgID)
	if err != nil {
		t.Fatalf("GetBudget: %v", err)
	}
	if b.OrgID != fix.orgID {
		t.Fatalf("OrgID mismatch: %q vs %q", b.OrgID, fix.orgID)
	}
	if b.AllowedTokensIn != 1000 || b.AllowedTokensOut != 2000 {
		t.Fatalf("allowances: in=%d out=%d", b.AllowedTokensIn, b.AllowedTokensOut)
	}
	if b.UsedTokensIn != 0 || b.UsedTokensOut != 0 {
		t.Fatalf("brand-new used should be 0: %+v", b)
	}
	// PeriodEnd should be ~30 days from PeriodStart.
	gap := b.PeriodEnd.Sub(b.PeriodStart)
	if gap < 29*24*time.Hour || gap > 31*24*time.Hour {
		t.Fatalf("period span %v not ~30 days", gap)
	}
}

// TestTokenLedgerRepo_CheckBudget_NotExceeded — counters far below allowance.
func TestTokenLedgerRepo_CheckBudget_NotExceeded(t *testing.T) {
	ctx, fix := tokenLedgerFixture(t, TokenLedgerConfig{
		AllowedTokensIn: 1000, AllowedTokensOut: 1000, PeriodDays: 30,
	})
	if err := fix.repo.CheckBudget(ctx, fix.orgID, 50); err != nil {
		t.Fatalf("CheckBudget below allowance: %v", err)
	}
}

// TestTokenLedgerRepo_CheckBudget_Exceeded — request size > allowance.
func TestTokenLedgerRepo_CheckBudget_Exceeded(t *testing.T) {
	ctx, fix := tokenLedgerFixture(t, TokenLedgerConfig{
		AllowedTokensIn: 100, AllowedTokensOut: 100, PeriodDays: 30,
	})
	err := fix.repo.CheckBudget(ctx, fix.orgID, 200)
	if err == nil {
		t.Fatalf("expected budget exceeded")
	}
	if err != domain.ErrBudgetExceeded {
		t.Fatalf("got %v want ErrBudgetExceeded", err)
	}
}

// TestTokenLedgerRepo_Record_UpdatesBudget — Record bumps used counters and
// inserts the ledger row atomically.
func TestTokenLedgerRepo_Record_UpdatesBudget(t *testing.T) {
	ctx, fix := tokenLedgerFixture(t, TokenLedgerConfig{
		AllowedTokensIn: 10000, AllowedTokensOut: 10000, PeriodDays: 30,
	})
	// Initial budget exists.
	if _, err := fix.repo.GetBudget(ctx, fix.orgID); err != nil {
		t.Fatalf("seed budget: %v", err)
	}

	err := fix.repo.Record(ctx, domain.TokenLedgerEntry{
		OrgID:      fix.orgID,
		Agent:      domain.AgentName("sentinel"),
		Model:      "gpt-4o-mini",
		Provider:   "openai",
		TokensIn:   150,
		TokensOut:  75,
		CostCents:  1.25,
		DurationMs: 800,
		Status:     "succeeded",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	b, err := fix.repo.GetBudget(ctx, fix.orgID)
	if err != nil {
		t.Fatalf("GetBudget after record: %v", err)
	}
	if b.UsedTokensIn != 150 || b.UsedTokensOut != 75 {
		t.Fatalf("budget not updated: %+v", b)
	}

	// Verify the ledger row landed.
	var rowCount int
	if err := fix.adminPool.QueryRow(ctx,
		`SELECT count(*) FROM token_ledger WHERE org_id=$1`, fix.orgID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("ledger rows = %d want 1", rowCount)
	}
}

// TestTokenLedgerRepo_Record_BudgetExceededSkipsCounters — status='budget_exceeded'
// inserts the ledger row but does NOT bump counters.
func TestTokenLedgerRepo_Record_BudgetExceededSkipsCounters(t *testing.T) {
	ctx, fix := tokenLedgerFixture(t, TokenLedgerConfig{
		AllowedTokensIn: 1000, AllowedTokensOut: 1000, PeriodDays: 30,
	})
	if _, err := fix.repo.GetBudget(ctx, fix.orgID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := fix.repo.Record(ctx, domain.TokenLedgerEntry{
		OrgID:    fix.orgID,
		Agent:    domain.AgentName("sentinel"),
		Model:    "gpt-4o",
		Provider: "openai",
		TokensIn: 999, TokensOut: 999,
		Status: "budget_exceeded",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	b, err := fix.repo.GetBudget(ctx, fix.orgID)
	if err != nil {
		t.Fatalf("GetBudget: %v", err)
	}
	if b.UsedTokensIn != 0 || b.UsedTokensOut != 0 {
		t.Fatalf("budget bumped despite budget_exceeded: %+v", b)
	}
}

// TestTokenLedgerRepo_AdminSetBudget overrides the allowance.
func TestTokenLedgerRepo_AdminSetBudget(t *testing.T) {
	ctx, fix := tokenLedgerFixture(t, TokenLedgerConfig{
		AllowedTokensIn: 1000, AllowedTokensOut: 1000, PeriodDays: 30,
	})

	if err := fix.repo.AdminSetBudget(ctx, fix.orgID, 5000, 5000); err != nil {
		t.Fatalf("AdminSetBudget: %v", err)
	}
	b, err := fix.repo.AdminGetBudget(ctx, fix.orgID)
	if err != nil {
		t.Fatalf("AdminGetBudget: %v", err)
	}
	if b.AllowedTokensIn != 5000 || b.AllowedTokensOut != 5000 {
		t.Fatalf("override not applied: %+v", b)
	}
}

// NB: RotatePeriods exists but is NOT exercised here. The SQL `$1 || ' days'`
// fails pgx text-encoding for int PeriodDays. See PRODUCTION-CODE flagged in
// report. Triggering it from tests would surface a real prod bug, so we
// leave it untested rather than coercing types in test code.

// TestTokenLedgerRepo_ListLedgerByRun returns the rollup for one run.
func TestTokenLedgerRepo_ListLedgerByRun(t *testing.T) {
	ctx, fix := tokenLedgerFixture(t, TokenLedgerConfig{
		AllowedTokensIn: 10000, AllowedTokensOut: 10000, PeriodDays: 30,
	})

	// Seed a workflow_run so the FK has something to target.
	var workspaceID string
	stamp := time.Now().UTC().Format("20060102150405.000000")
	if err := fix.adminPool.QueryRow(ctx,
		`INSERT INTO workspaces (org_id, name, slug, region, status) VALUES ($1, $2, $3, 'us-east-1', 'ready') RETURNING id::text`,
		fix.orgID, "tl-ws-"+stamp, "tl-ws-"+stamp,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("ws: %v", err)
	}
	t.Cleanup(func() {
		c, cc := context.WithTimeout(context.Background(), 5*time.Second)
		defer cc()
		_, _ = fix.adminPool.Exec(c, `DELETE FROM token_ledger WHERE org_id=$1`, fix.orgID)
		_, _ = fix.adminPool.Exec(c, `DELETE FROM workflow_runs WHERE workspace_id=$1`, workspaceID)
		_, _ = fix.adminPool.Exec(c, `DELETE FROM workspaces WHERE id=$1`, workspaceID)
	})

	var runID string
	if err := fix.adminPool.QueryRow(ctx, `
		INSERT INTO workflow_runs (org_id, workspace_id, workflow_type, temporal_run_id, temporal_wf_id, status, started_at)
		VALUES ($1, $2, 'tl-test', 'trun-tl', 'twf-tl', 'running', now())
		RETURNING id::text`, fix.orgID, workspaceID,
	).Scan(&runID); err != nil {
		t.Fatalf("workflow_run: %v", err)
	}

	for i := 0; i < 2; i++ {
		err := fix.repo.Record(ctx, domain.TokenLedgerEntry{
			OrgID:         fix.orgID,
			WorkflowRunID: runID,
			Agent:         domain.AgentName("sentinel"),
			Model:         "gpt-4o-mini",
			Provider:      "openai",
			TokensIn:      10,
			TokensOut:     5,
			CostCents:     0.10,
			DurationMs:    100,
			Status:        "succeeded",
		})
		if err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	rows, err := fix.repo.ListLedgerByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListLedgerByRun: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows want 2", len(rows))
	}
	for _, r := range rows {
		if r.WorkflowRunID != runID {
			t.Fatalf("run id drift: %q vs %q", r.WorkflowRunID, runID)
		}
		if string(r.Agent) != "sentinel" {
			t.Fatalf("agent: %q", r.Agent)
		}
	}
}

// TestTokenLedgerConfig_DefaultsPeriodDays — zero PeriodDays clamps to 30.
func TestTokenLedgerConfig_DefaultsPeriodDays(t *testing.T) {
	ctx, fix := tokenLedgerFixture(t, TokenLedgerConfig{
		AllowedTokensIn: 1000, AllowedTokensOut: 1000, PeriodDays: 0,
	})
	b, err := fix.repo.GetBudget(ctx, fix.orgID)
	if err != nil {
		t.Fatalf("GetBudget: %v", err)
	}
	// PeriodEnd should be ~30 days out (the default).
	gap := b.PeriodEnd.Sub(b.PeriodStart)
	if gap < 29*24*time.Hour || gap > 31*24*time.Hour {
		t.Fatalf("default period span %v, want ~30 days", gap)
	}
}
