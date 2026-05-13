// Package repo — approval_decisions adapter. Phase 6 Stage 7.
//
// ApprovalRepo dual-pools like TokenLedgerRepo: the app pool participates in
// the per-request RLS tx (db.FromCtx) for HTTP reads + signal-side updates
// triggered from the workflow's signal handler; the admin pool services the
// activity-side Create which runs OUTSIDE any request tx (the workflow worker
// has no GUC bound).
//
// Tests live in approval_repo_test.go and are env-gated against a real
// Postgres so they exercise the RLS policy installed by migration 0017.
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

// ApprovalRepo persists approval_decisions rows. Implements
// domain.ApprovalRepository.
type ApprovalRepo struct {
	pool      *pgxpool.Pool
	adminPool *pgxpool.Pool
}

// NewApprovalRepo constructs an ApprovalRepo. adminPool may be nil — falls
// back to pool so single-pool tests still work.
func NewApprovalRepo(pool, admin *pgxpool.Pool) *ApprovalRepo {
	if admin == nil {
		admin = pool
	}
	return &ApprovalRepo{pool: pool, adminPool: admin}
}

// Create inserts a new pending decision row via the admin pool. The
// ApprovalGate activity is system-owned (no request tx) so it cannot rely
// on the GUC binding. risk_score is stored as numeric(5,2); we pass the
// float64 value verbatim and pg coerces.
func (r *ApprovalRepo) Create(ctx context.Context, d domain.ApprovalDecision) (string, error) {
	var id string
	dec := d.Decision
	if dec == "" {
		dec = domain.ApprovalPending
	}
	err := r.adminPool.QueryRow(ctx, `
        INSERT INTO approval_decisions
          (org_id, workspace_id, workflow_run_id, severity, decision, scenario, risk_score)
        VALUES ($1, $2, $3, $4, $5, NULLIF($6,''), $7)
        RETURNING id::text`,
		d.OrgID, d.WorkspaceID, d.WorkflowRunID,
		string(d.Severity), string(dec), d.Scenario, d.RiskScore,
	).Scan(&id)
	return id, err
}

// GetByRun returns the row keyed by workflow_run_id. Uses the request tx
// when one is attached (HTTP path), pool otherwise (system path). RLS
// returns ErrNotFound when the caller is bound to a different org.
func (r *ApprovalRepo) GetByRun(ctx context.Context, runID string) (domain.ApprovalDecision, error) {
	q := db.FromCtx(ctx, r.pool)
	var d domain.ApprovalDecision
	var decidedBy *string
	var decidedAt *time.Time
	var notes, scenario *string
	var risk *float64
	err := q.QueryRow(ctx, `
        SELECT id::text, org_id::text, workspace_id::text, workflow_run_id::text,
               severity, decision,
               decided_by::text, decided_at,
               notes, scenario, risk_score, created_at
        FROM approval_decisions WHERE workflow_run_id=$1`, runID,
	).Scan(
		&d.ID, &d.OrgID, &d.WorkspaceID, &d.WorkflowRunID,
		(*string)(&d.Severity), (*string)(&d.Decision),
		&decidedBy, &decidedAt,
		&notes, &scenario, &risk, &d.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ApprovalDecision{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ApprovalDecision{}, err
	}
	if decidedBy != nil {
		d.DecidedBy = *decidedBy
	}
	if decidedAt != nil {
		d.DecidedAt = *decidedAt
	}
	if notes != nil {
		d.Notes = *notes
	}
	if scenario != nil {
		d.Scenario = *scenario
	}
	if risk != nil {
		d.RiskScore = *risk
	}
	return d, nil
}

// UpdateDecision flips a row from pending → terminal (approved | rejected |
// auto_approved | timeout_rejected). decidedBy is nullable; an empty string
// is stored as SQL NULL so auto/timeout rows don't fabricate a user id.
func (r *ApprovalRepo) UpdateDecision(ctx context.Context, runID string, state domain.ApprovalDecisionState, decidedBy, notes string, at time.Time) error {
	q := db.FromCtx(ctx, r.adminPool)
	var by any
	if decidedBy != "" {
		by = decidedBy
	}
	var notesArg any
	if notes != "" {
		notesArg = notes
	}
	_, err := q.Exec(ctx, `
        UPDATE approval_decisions
        SET decision=$1, decided_by=$2, decided_at=$3, notes=$4
        WHERE workflow_run_id=$5`,
		string(state), by, at.UTC().Truncate(time.Microsecond), notesArg, runID,
	)
	return err
}

// ListPending returns the org's pending decisions newest-first, capped at
// limit (defaults to 50 when <= 0). Uses the request tx via db.FromCtx so
// RLS enforces the tenant boundary even though we pass orgID explicitly.
func (r *ApprovalRepo) ListPending(ctx context.Context, orgID string, limit int) ([]domain.ApprovalDecision, error) {
	if limit <= 0 {
		limit = 50
	}
	q := db.FromCtx(ctx, r.pool)
	rows, err := q.Query(ctx, `
        SELECT id::text, org_id::text, workspace_id::text, workflow_run_id::text,
               severity, decision, scenario, risk_score, created_at
        FROM approval_decisions
        WHERE org_id=$1 AND decision='pending'
        ORDER BY created_at DESC LIMIT $2`,
		orgID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ApprovalDecision{}
	for rows.Next() {
		var d domain.ApprovalDecision
		var scenario *string
		var risk *float64
		if err := rows.Scan(
			&d.ID, &d.OrgID, &d.WorkspaceID, &d.WorkflowRunID,
			(*string)(&d.Severity), (*string)(&d.Decision),
			&scenario, &risk, &d.CreatedAt,
		); err != nil {
			return nil, err
		}
		if scenario != nil {
			d.Scenario = *scenario
		}
		if risk != nil {
			d.RiskScore = *risk
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// compile-time conformance check
var _ domain.ApprovalRepository = (*ApprovalRepo)(nil)
