// Package repo — feedback_examples adapter (RLHF pipeline).
//
// FeedbackRepo dual-pools like ApprovalRepo: Insert runs on the admin pool
// because the caller is the ApprovalGateFinalize activity (Temporal worker,
// no request tx / GUC); ExportUnexported runs through db.FromCtx so the
// owner-only HTTP export inherits the per-request RLS tx and can never leak
// a sibling tenant's examples even if the org filter were dropped.
package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// FeedbackRepo persists feedback_examples rows. Implements
// domain.FeedbackRepository.
type FeedbackRepo struct {
	pool      *pgxpool.Pool
	adminPool *pgxpool.Pool
}

// NewFeedbackRepo constructs a FeedbackRepo. adminPool may be nil — falls
// back to pool so single-pool tests still work.
func NewFeedbackRepo(pool, admin *pgxpool.Pool) *FeedbackRepo {
	if admin == nil {
		admin = pool
	}
	return &FeedbackRepo{pool: pool, adminPool: admin}
}

// Insert lands one example via the admin pool (activity-side, no request tx).
func (r *FeedbackRepo) Insert(ctx context.Context, fe domain.FeedbackExample) (string, error) {
	var decidedBy any
	if fe.DecidedBy != "" {
		decidedBy = fe.DecidedBy
	}
	var modified any
	if fe.ModifiedDiff != "" {
		modified = fe.ModifiedDiff
	}
	at := fe.DecidedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	var id string
	err := r.adminPool.QueryRow(ctx, `
        INSERT INTO feedback_examples
          (org_id, incident_id, workflow_run_id, scenario, patch_diff,
           decision, modified_diff, decided_by, decided_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
        RETURNING id::text`,
		fe.OrgID, fe.IncidentID, fe.WorkflowRunID, fe.Scenario, fe.PatchDiff,
		fe.Decision, modified, decidedBy, at.UTC().Truncate(time.Microsecond),
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("feedback_examples: insert: %w", err)
	}
	return id, nil
}

// ExportUnexported stamps exported_at=now() on up to limit unexported rows
// for the org and returns them oldest-first. Single UPDATE..RETURNING so the
// read + cursor advance are atomic — a crashed export never loses rows, and
// a concurrent second export cannot double-serve the same example. Runs via
// db.FromCtx so the HTTP path stays inside the per-request RLS tx.
func (r *FeedbackRepo) ExportUnexported(ctx context.Context, orgID string, limit int) ([]domain.FeedbackExample, error) {
	if limit <= 0 {
		limit = 1000
	}
	q := db.FromCtx(ctx, r.pool)
	rows, err := q.Query(ctx, `
        UPDATE feedback_examples SET exported_at = now()
        WHERE id IN (
          SELECT id FROM feedback_examples
          WHERE org_id = $1 AND exported_at IS NULL
          ORDER BY created_at ASC
          LIMIT $2
          FOR UPDATE SKIP LOCKED
        )
        RETURNING id::text, org_id::text, incident_id, workflow_run_id::text,
                  scenario, patch_diff, decision, COALESCE(modified_diff, ''),
                  COALESCE(decided_by::text, ''), decided_at, exported_at, created_at`,
		orgID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("feedback_examples: export: %w", err)
	}
	defer rows.Close()
	out := []domain.FeedbackExample{}
	for rows.Next() {
		var fe domain.FeedbackExample
		if err := rows.Scan(
			&fe.ID, &fe.OrgID, &fe.IncidentID, &fe.WorkflowRunID,
			&fe.Scenario, &fe.PatchDiff, &fe.Decision, &fe.ModifiedDiff,
			&fe.DecidedBy, &fe.DecidedAt, &fe.ExportedAt, &fe.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, fe)
	}
	return out, rows.Err()
}

// compile-time conformance check
var _ domain.FeedbackRepository = (*FeedbackRepo)(nil)
