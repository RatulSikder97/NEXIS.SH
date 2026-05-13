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

// TokenLedgerRepo dual-pools the way BillingRepo does: app pool for
// RLS-scoped HTTP reads, admin pool for activity-side writes that run
// outside any request tx (the LLM activity is system-owned and passes
// orgID explicitly).
type TokenLedgerRepo struct {
	pool      *pgxpool.Pool
	adminPool *pgxpool.Pool
	cfg       TokenLedgerConfig
}

// TokenLedgerConfig carries the per-org per-period defaults injected at
// boot. PeriodDays defaults to 30 when <= 0.
type TokenLedgerConfig struct {
	AllowedTokensIn  int64
	AllowedTokensOut int64
	PeriodDays       int
}

// NewTokenLedgerRepo constructs a TokenLedgerRepo. adminPool may be nil —
// it falls back to pool so tests with a single connection still work.
func NewTokenLedgerRepo(pool, adminPool *pgxpool.Pool, cfg TokenLedgerConfig) *TokenLedgerRepo {
	if adminPool == nil {
		adminPool = pool
	}
	if cfg.PeriodDays <= 0 {
		cfg.PeriodDays = 30
	}
	return &TokenLedgerRepo{pool: pool, adminPool: adminPool, cfg: cfg}
}

// CheckBudget returns ErrBudgetExceeded when the current period's
// used_tokens_in + expectedTokensIn breaches the allowance, or when the
// period's output cap is exhausted.
func (r *TokenLedgerRepo) CheckBudget(ctx context.Context, orgID string, expectedTokensIn int) error {
	b, err := r.GetBudget(ctx, orgID)
	if err != nil {
		return err
	}
	if b.UsedTokensIn+int64(expectedTokensIn) > b.AllowedTokensIn {
		return domain.ErrBudgetExceeded
	}
	if b.UsedTokensOut >= b.AllowedTokensOut {
		return domain.ErrBudgetExceeded
	}
	return nil
}

// GetBudget reads or lazily creates the current period's row. Uses the admin
// pool so it works from both HTTP handlers (which RLS-set first via ctx) and
// activities (which have no ctx-bound tx).
func (r *TokenLedgerRepo) GetBudget(ctx context.Context, orgID string) (domain.TokenBudget, error) {
	var b domain.TokenBudget
	now := time.Now().UTC().Truncate(time.Microsecond)
	periodStart := now.Truncate(24 * time.Hour) // midnight UTC of today
	periodEnd := periodStart.AddDate(0, 0, r.cfg.PeriodDays)

	// INSERT … ON CONFLICT DO NOTHING then SELECT — two roundtrips but
	// race-safe across concurrent first-time calls.
	_, err := r.adminPool.Exec(ctx, `
        INSERT INTO token_budgets
          (org_id, period_start, period_end, allowed_tokens_in, allowed_tokens_out)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (org_id, period_start) DO NOTHING`,
		orgID, periodStart, periodEnd, r.cfg.AllowedTokensIn, r.cfg.AllowedTokensOut)
	if err != nil {
		return b, err
	}

	err = r.adminPool.QueryRow(ctx, `
        SELECT org_id::text, period_start, period_end,
               allowed_tokens_in, allowed_tokens_out,
               used_tokens_in,    used_tokens_out
        FROM token_budgets
        WHERE org_id=$1 AND period_start <= $2 AND period_end > $2
        ORDER BY period_start DESC
        LIMIT 1`, orgID, now).Scan(
		&b.OrgID, &b.PeriodStart, &b.PeriodEnd,
		&b.AllowedTokensIn, &b.AllowedTokensOut,
		&b.UsedTokensIn, &b.UsedTokensOut,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, domain.ErrNotFound
	}
	return b, err
}

// Record persists one row + updates token_budgets atomically inside a tx.
// budget_exceeded rows never reached the provider so they don't bump used
// counters; every other status (succeeded, schema_mismatch, provider_error)
// is counted against the budget so retries are reflected.
func (r *TokenLedgerRepo) Record(ctx context.Context, e domain.TokenLedgerEntry) error {
	tx, err := r.adminPool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var runID any
	if e.WorkflowRunID != "" {
		runID = e.WorkflowRunID
	}
	_, err = tx.Exec(ctx, `
        INSERT INTO token_ledger
          (org_id, workflow_run_id, agent, model, provider,
           tokens_in, tokens_out, cached_tokens, cost_cents,
           duration_ms, status, recorded_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		e.OrgID, runID, string(e.Agent), e.Model, e.Provider,
		e.TokensIn, e.TokensOut, e.CachedTokens, e.CostCents,
		e.DurationMs, e.Status, time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return err
	}

	if e.Status != "budget_exceeded" && (e.TokensIn > 0 || e.TokensOut > 0) {
		_, err = tx.Exec(ctx, `
            UPDATE token_budgets
            SET used_tokens_in  = used_tokens_in  + $2,
                used_tokens_out = used_tokens_out + $3,
                updated_at      = now()
            WHERE org_id=$1
              AND period_start <= now()
              AND period_end   > now()`,
			e.OrgID, e.TokensIn, e.TokensOut)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// RotatePeriods is called by the daily cron job — closes finished periods
// and opens fresh ones for every org with a previously open period.
// Idempotent via UNIQUE (org_id, period_start).
func (r *TokenLedgerRepo) RotatePeriods(ctx context.Context, now time.Time) error {
	_, err := r.adminPool.Exec(ctx, `
        INSERT INTO token_budgets
          (org_id, period_start, period_end, allowed_tokens_in, allowed_tokens_out)
        SELECT DISTINCT ON (org_id)
          org_id,
          period_end                              AS period_start,
          period_end + ($1 || ' days')::interval  AS period_end,
          $2 AS allowed_tokens_in,
          $3 AS allowed_tokens_out
        FROM token_budgets
        WHERE period_end <= $4
        ORDER BY org_id, period_end DESC
        ON CONFLICT (org_id, period_start) DO NOTHING`,
		r.cfg.PeriodDays, r.cfg.AllowedTokensIn, r.cfg.AllowedTokensOut, now)
	return err
}

// ListLedgerByRun returns the per-agent rollup for one workflow run. Used by
// the modified GET /v1/workspaces/{ws}/pipelines/{run_id} handler. App pool
// + db.FromCtx — RLS-scoped.
func (r *TokenLedgerRepo) ListLedgerByRun(ctx context.Context, runID string) ([]domain.TokenLedgerEntry, error) {
	q := db.FromCtx(ctx, r.pool)
	rows, err := q.Query(ctx, `
        SELECT org_id::text, COALESCE(workflow_run_id::text,''), agent, model, provider,
               tokens_in, tokens_out, cached_tokens, cost_cents::float8,
               duration_ms, status
        FROM token_ledger
        WHERE workflow_run_id=$1
        ORDER BY recorded_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TokenLedgerEntry{}
	for rows.Next() {
		var e domain.TokenLedgerEntry
		var agent string
		if err := rows.Scan(&e.OrgID, &e.WorkflowRunID, &agent, &e.Model, &e.Provider,
			&e.TokensIn, &e.TokensOut, &e.CachedTokens, &e.CostCents,
			&e.DurationMs, &e.Status); err != nil {
			return nil, err
		}
		e.Agent = domain.AgentName(agent)
		out = append(out, e)
	}
	return out, rows.Err()
}

// AdminGetBudget returns the budget for an org without RLS pinning. Used by
// admin token-budget endpoints + the eval CLI.
func (r *TokenLedgerRepo) AdminGetBudget(ctx context.Context, orgID string) (domain.TokenBudget, error) {
	return r.GetBudget(ctx, orgID)
}

// AdminSetBudget updates the current period's allotments for one org. Idempotent.
func (r *TokenLedgerRepo) AdminSetBudget(ctx context.Context, orgID string, allowedIn, allowedOut int64) error {
	// Ensure a row exists for the current period.
	_, _ = r.GetBudget(ctx, orgID)
	_, err := r.adminPool.Exec(ctx, `
        UPDATE token_budgets
        SET allowed_tokens_in=$2,
            allowed_tokens_out=$3,
            updated_at=now()
        WHERE org_id=$1
          AND period_start <= now()
          AND period_end   > now()`, orgID, allowedIn, allowedOut)
	return err
}
