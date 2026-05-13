package repo

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// NASATLXRepo persists nasa_tlx_responses rows. Tenant-scoped — the RLS
// policy enforces org_id = current_setting('app.current_org_id') so callers
// must come through the per-request RLS tx (db.FromCtx). adminPool is wired
// only as a fallback for code paths that don't carry a tx; in practice every
// NASA-TLX HTTP call is authenticated and rides the RLS middleware.
type NASATLXRepo struct {
	pool      *pgxpool.Pool
	adminPool *pgxpool.Pool
}

// NewNASATLXRepo constructs an NASATLXRepo. adminPool may be nil — db.FromCtx
// then falls through to pool, which under RLS will see only the caller's
// rows (the desired property at the HTTP layer).
func NewNASATLXRepo(pool, adminPool *pgxpool.Pool) *NASATLXRepo {
	if adminPool == nil {
		adminPool = pool
	}
	return &NASATLXRepo{pool: pool, adminPool: adminPool}
}

// Insert persists one NASA-TLX response. All six subscales are validated by
// the CHECK constraint on the table; the handler also validates upfront so
// the user gets a 400 instead of a CHECK violation.
func (r *NASATLXRepo) Insert(ctx context.Context, resp *domain.NASATLXResponse) error {
	q := db.FromCtx(ctx, r.pool)
	return q.QueryRow(ctx, `
        INSERT INTO nasa_tlx_responses (
            user_id, org_id, recovery_run_id,
            mental_demand, physical_demand, temporal_demand,
            performance, effort, frustration,
            notes, created_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11, now()))
        RETURNING id, created_at`,
		resp.UserID, resp.OrgID, resp.RecoveryRunID,
		resp.MentalDemand, resp.PhysicalDemand, resp.TemporalDemand,
		resp.Performance, resp.Effort, resp.Frustration,
		nullableString(resp.Notes), nullTime(resp.CreatedAt),
	).Scan(&resp.ID, &resp.CreatedAt)
}

// ListByOrg returns the org's recent responses, newest first. Capped at 200.
// Routes through db.FromCtx so RLS pins the org_id; the WHERE filter is
// defence-in-depth.
func (r *NASATLXRepo) ListByOrg(ctx context.Context, orgID string, limit int) ([]domain.NASATLXResponse, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := db.FromCtx(ctx, r.pool)
	rows, err := q.Query(ctx, `
        SELECT id, user_id, org_id, recovery_run_id,
               mental_demand, physical_demand, temporal_demand,
               performance, effort, frustration,
               COALESCE(notes, ''), created_at
        FROM nasa_tlx_responses
        WHERE org_id = $1
        ORDER BY created_at DESC
        LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.NASATLXResponse{}
	for rows.Next() {
		var x domain.NASATLXResponse
		var userID, orgIDOut, runID *string
		if err := rows.Scan(
			&x.ID, &userID, &orgIDOut, &runID,
			&x.MentalDemand, &x.PhysicalDemand, &x.TemporalDemand,
			&x.Performance, &x.Effort, &x.Frustration,
			&x.Notes, &x.CreatedAt,
		); err != nil {
			return nil, err
		}
		x.UserID = userID
		x.OrgID = orgIDOut
		x.RecoveryRunID = runID
		out = append(out, x)
	}
	return out, rows.Err()
}

// nullableString returns nil for empty so notes stays NULL in the DB rather
// than ''. Keeps queries that filter on notes IS NULL working.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// timeNonzero is a thin helper kept here so unit tests can stub out time
// concerns without exporting `now` from the package. Currently unused outside
// the package itself.
var _ = time.Now
