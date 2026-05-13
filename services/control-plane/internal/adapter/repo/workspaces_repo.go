package repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// pgUniqueViolation is the SQLSTATE Postgres returns for unique-constraint
// violations. The workspace service uses this to retry the slug suffix on
// conflict.
const pgUniqueViolation = "23505"

// WorkspacesRepo persists rows in the workspaces table. Tenant-scoped reads
// (Get, List) route through db.FromCtx(ctx, pool) so the per-request RLS tx
// is used when present. The system-level methods (UpdateStatus, ListAllReady,
// HasAny) call adminPool directly — they run outside the request lifecycle
// (state machine goroutine, cron, signup-time has-any) so there is no RLS
// principal to pin and the queries must not be filtered out by the policy.
type WorkspacesRepo struct {
	pool      *pgxpool.Pool // nexis_app — RLS-aware, fallback for db.FromCtx
	adminPool *pgxpool.Pool // nexis superuser — bypasses RLS for system paths
}

// NewWorkspacesRepo constructs a WorkspacesRepo. pool is the application pool
// used for tenant-scoped queries inside the per-request RLS tx; adminPool is
// the admin pool used for system-level paths (state machine writes, cron
// reads, signup has-any). adminPool may be nil — system paths will then fall
// back to pool, which works when RLS is not yet enabled but breaks once the
// 0008 policies are applied.
func NewWorkspacesRepo(pool, adminPool *pgxpool.Pool) *WorkspacesRepo {
	if adminPool == nil {
		adminPool = pool
	}
	return &WorkspacesRepo{pool: pool, adminPool: adminPool}
}

// IsUniqueViolation reports whether err is a Postgres unique-constraint
// violation. Used by the workspace service to detect slug collisions and
// retry with a numeric suffix. Re-exported as a helper so the service doesn't
// need to import pgconn directly.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

// Insert creates a new workspace row. ID + CreatedAt + UpdatedAt are returned
// from the DB so the caller's struct stays in sync with the persisted state.
// Caller is responsible for retrying on slug unique-violation; see
// IsUniqueViolation.
//
// Runs under the per-request RLS tx via db.FromCtx so the INSERT obeys the
// tenant policy on workspaces (org_id must equal app.current_org_id).
func (r *WorkspacesRepo) Insert(ctx context.Context, w *domain.Workspace) error {
	q := db.FromCtx(ctx, r.pool)
	return q.QueryRow(ctx, `
        INSERT INTO workspaces (org_id, name, slug, region, status, provisioning_step, created_at, updated_at)
        VALUES ($1,$2,$3,$4,$5,$6, now(), now())
        RETURNING id, created_at, updated_at`,
		w.OrgID, w.Name, w.Slug, w.Region, string(w.Status), w.ProvisioningStep,
	).Scan(&w.ID, &w.CreatedAt, &w.UpdatedAt)
}

// UpdateStatus advances the workspace's lifecycle. When status == ready the
// ready_at column is set to the current wall clock; on subsequent calls
// COALESCE preserves the original ready_at.
//
// The state machine runs on its own goroutine outside any RLS tx and with a
// fresh context.Background(), so this method uses the admin pool directly to
// bypass RLS — the application pool would filter the UPDATE through the
// tenant policy and match zero rows.
func (r *WorkspacesRepo) UpdateStatus(ctx context.Context, id string, status domain.WorkspaceStatus, step, message string) error {
	var readyAt *time.Time
	if status == domain.WSReady {
		n := time.Now()
		readyAt = &n
	}
	_, err := r.adminPool.Exec(ctx, `
        UPDATE workspaces
        SET status=$2, provisioning_step=$3, status_message=$4,
            ready_at = COALESCE($5, ready_at), updated_at=now()
        WHERE id=$1`, id, string(status), step, message, readyAt)
	return err
}

// Get returns one workspace by (org_id, id). Returns domain.ErrNotFound when
// no row matches. The org_id filter is defence-in-depth on top of the RLS
// policy — calling Get with a foreign org_id returns ErrNotFound even if RLS
// is misconfigured.
func (r *WorkspacesRepo) Get(ctx context.Context, orgID, id string) (*domain.Workspace, error) {
	q := db.FromCtx(ctx, r.pool)
	var w domain.Workspace
	err := q.QueryRow(ctx, `
        SELECT id, org_id, name, slug, region, status, COALESCE(status_message, ''), COALESCE(provisioning_step, ''), created_at, ready_at, updated_at
        FROM workspaces WHERE org_id=$1 AND id=$2`, orgID, id).Scan(
		&w.ID, &w.OrgID, &w.Name, &w.Slug, &w.Region, &w.Status, &w.StatusMessage, &w.ProvisioningStep, &w.CreatedAt, &w.ReadyAt, &w.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// List returns all of an org's workspaces ordered by created_at descending.
// The returned slice is never nil even when empty so JSON serialisation emits
// `[]` rather than `null`.
func (r *WorkspacesRepo) List(ctx context.Context, orgID string) ([]domain.Workspace, error) {
	q := db.FromCtx(ctx, r.pool)
	rows, err := q.Query(ctx, `
        SELECT id, org_id, name, slug, region, status, COALESCE(status_message, ''), COALESCE(provisioning_step, ''), created_at, ready_at, updated_at
        FROM workspaces WHERE org_id=$1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Workspace{}
	for rows.Next() {
		var w domain.Workspace
		if err := rows.Scan(&w.ID, &w.OrgID, &w.Name, &w.Slug, &w.Region, &w.Status, &w.StatusMessage, &w.ProvisioningStep, &w.CreatedAt, &w.ReadyAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListAllReady enumerates every workspace in status=ready across ALL orgs.
// Used by the usage_ticker cron — it bypasses RLS via the bare admin pool
// because there is no principal in ctx at cron time.
func (r *WorkspacesRepo) ListAllReady(ctx context.Context) ([]domain.Workspace, error) {
	rows, err := r.adminPool.Query(ctx, `
        SELECT id, org_id FROM workspaces WHERE status='ready'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Workspace{}
	for rows.Next() {
		var w domain.Workspace
		if err := rows.Scan(&w.ID, &w.OrgID); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// HasAny reports whether the given org has any workspace row at all. Called
// by the auth handlers (Signup / Login / Me) to fill AuthResp.HasWorkspace.
// Runs on the bare admin pool because during signup there is no principal in
// ctx yet to pin RLS to.
func (r *WorkspacesRepo) HasAny(ctx context.Context, orgID string) (bool, error) {
	var n int
	if err := r.adminPool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE org_id=$1`, orgID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// OwnsWorkspace reports whether (orgID, workspaceID) names a real row. Uses
// the admin pool so it can be called from contexts outside a request tx —
// the SSE handler runs this check before opening the long-lived stream,
// before RLS would even be useful.
func (r *WorkspacesRepo) OwnsWorkspace(ctx context.Context, orgID, workspaceID string) (bool, error) {
	var n int
	if err := r.adminPool.QueryRow(ctx,
		`SELECT count(*) FROM workspaces WHERE org_id=$1 AND id=$2`, orgID, workspaceID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// DefaultForOrg returns the org's "default" workspace id — the oldest one
// created. Used by the Phase 6 Sentinel detector to pick a workspace to bind
// the auto-triggered RecoveryPipeline run to: the goroutine has no UI
// principal in ctx so it can't ask the user which workspace to use.
//
// Returns domain.ErrNotFound when the org has no workspaces. Uses the admin
// pool — the goroutine runs on context.Background.
func (r *WorkspacesRepo) DefaultForOrg(ctx context.Context, orgID string) (string, error) {
	var id string
	err := r.adminPool.QueryRow(ctx,
		`SELECT id FROM workspaces WHERE org_id=$1 ORDER BY created_at ASC LIMIT 1`,
		orgID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return id, nil
}
