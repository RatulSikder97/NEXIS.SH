package domain

import (
	"context"
	"time"
)

// TokenBudget is the per-org per-period allotment. Periods are rolling: the
// cron rotation job opens a new period when the previous one ends.
type TokenBudget struct {
	OrgID            string
	PeriodStart      time.Time
	PeriodEnd        time.Time
	AllowedTokensIn  int64
	AllowedTokensOut int64
	UsedTokensIn     int64
	UsedTokensOut    int64
}

// TokenLedgerEntry is one row in token_ledger. Status is one of:
//
//	succeeded | schema_mismatch | budget_exceeded | provider_error
type TokenLedgerEntry struct {
	OrgID         string
	WorkflowRunID string // empty string when not workflow-bound (e.g. seed)
	Agent         AgentName
	Model         string
	Provider      string
	TokensIn      int
	TokensOut     int
	CachedTokens  int
	CostCents     float64
	DurationMs    int
	Status        string
}

// TokenLedger is the port the agents layer + cron depend on. Implementations
// live in internal/adapter/repo/token_ledger_repo.go (Postgres-backed).
type TokenLedger interface {
	// CheckBudget returns ErrBudgetExceeded if the current period's
	// used_tokens_in + expectedTokensIn would breach allowed_tokens_in.
	// expectedTokensIn is a rough upper bound the agent passes; the
	// implementation is intentionally permissive (only checks input tokens,
	// because output tokens are unknown until completion).
	CheckBudget(ctx context.Context, orgID string, expectedTokensIn int) error

	// Record persists one row + updates token_budgets atomically inside a tx.
	Record(ctx context.Context, e TokenLedgerEntry) error

	// GetBudget returns the current period's budget, creating one with the
	// configured defaults if missing. Caller is expected to be inside an RLS
	// tx (request-scoped) or to use an admin-pool variant for cron paths.
	GetBudget(ctx context.Context, orgID string) (TokenBudget, error)

	// RotatePeriods is called by the daily cron job — closes finished periods
	// and opens fresh ones for every org with a previously open period.
	RotatePeriods(ctx context.Context, now time.Time) error
}
