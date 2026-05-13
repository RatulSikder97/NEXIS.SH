package repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// OrgStatsRepo reads aggregate counters off the organizations table (Phase 8).
//
// The successful_recoveries_count column is maintained by the AFTER-UPDATE
// trigger on workflow_runs (migration 0020). This repo only reads — the
// counter is never written from application code; the trigger is the single
// source of truth so we don't double-count when both code paths fire.
//
// We use adminPool because the org-stats endpoint serves the hero stats banner
// + landing-page counters that don't necessarily run inside the per-request
// RLS tx (an unauthenticated landing endpoint might in future read across
// orgs). For Phase 8 the endpoint is authenticated, so RLS would be fine, but
// adminPool keeps the read path uniform.
type OrgStatsRepo struct {
	adminPool *pgxpool.Pool
}

// NewOrgStatsRepo wires a stats reader. adminPool must be non-nil.
func NewOrgStatsRepo(adminPool *pgxpool.Pool) *OrgStatsRepo {
	return &OrgStatsRepo{adminPool: adminPool}
}

// SuccessfulRecoveriesCount returns the trigger-maintained counter. Returns
// domain.ErrNotFound when the org id doesn't match a row.
func (r *OrgStatsRepo) SuccessfulRecoveriesCount(ctx context.Context, orgID string) (int, error) {
	var n int
	err := r.adminPool.QueryRow(ctx,
		`SELECT successful_recoveries_count FROM organizations WHERE id = $1`, orgID).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return n, nil
}
