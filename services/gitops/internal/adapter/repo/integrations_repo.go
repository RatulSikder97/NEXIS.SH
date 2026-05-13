// Package repo holds the gitops service's narrow Postgres adapters.
// The service only needs (a) read the GitHub installation_id for an org and
// (b) append a "gitops.pr_opened" audit row when a PR is opened — there is
// no other persistence surface here.
package repo

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IntegrationsRepo reads the integration row keyed by (org_id, provider).
// The gitops service runs under the nexis_gitops role (migration 0018) which
// has a permissive SELECT policy on integrations so we don't need to set
// app.current_org_id — the role acts as the system-side bypass.
type IntegrationsRepo struct {
	pool *pgxpool.Pool
}

// NewIntegrationsRepo builds a repo. pool must be the admin/system pool
// (nexis_gitops user) — see migration 0018.
func NewIntegrationsRepo(p *pgxpool.Pool) *IntegrationsRepo {
	return &IntegrationsRepo{pool: p}
}

// InstallationID returns the GitHub App installation id for the given org.
// Returns 0 + nil when no connection exists; non-nil error on DB failure.
// The installation_id column is text — we parse to int64 since GitHub's
// installation ids are 64-bit numerics.
func (r *IntegrationsRepo) InstallationID(ctx context.Context, orgID string) (int64, error) {
	if r.pool == nil {
		return 0, errors.New("integrations repo: pool nil")
	}
	var s string
	err := r.pool.QueryRow(ctx, `
        SELECT installation_id FROM integrations
        WHERE org_id=$1 AND provider='github' AND status='connected'`,
		orgID,
	).Scan(&s)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}
