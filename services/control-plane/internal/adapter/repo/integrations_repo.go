package repo

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// IntegrationsRepo persists per-org integration connections + their encrypted
// secret blob. Every method routes through db.FromCtx(ctx, pool) so the
// per-request RLS tx (with SET LOCAL app.current_org_id) is used when
// available; the pool fallback is exercised only by adapters that explicitly
// run outside the request lifecycle (none in Phase 3 — webhook handlers attach
// an RLS tx after they resolve org_id).
type IntegrationsRepo struct{ pool *pgxpool.Pool }

// NewIntegrationsRepo builds an IntegrationsRepo against the supplied pool.
// The pool is used only as the Querier fallback; production routes always
// carry a tx in ctx.
func NewIntegrationsRepo(pool *pgxpool.Pool) *IntegrationsRepo {
	return &IntegrationsRepo{pool: pool}
}

// Upsert inserts a new integration row or updates the existing one on the
// (org_id, provider) unique key. secret_ciphertext is the KeyVault-encrypted
// bytes from the adapter — the repo NEVER sees plaintext.
func (r *IntegrationsRepo) Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error {
	metaJSON, err := json.Marshal(c.Metadata)
	if err != nil {
		return err
	}
	q := db.FromCtx(ctx, r.pool)
	_, err = q.Exec(ctx, `
        INSERT INTO integrations (org_id, provider, status, installation_id, secret_ciphertext, metadata, last_error)
        VALUES ($1,$2,$3,$4,$5,$6,$7)
        ON CONFLICT (org_id, provider) DO UPDATE
          SET status=EXCLUDED.status,
              installation_id=EXCLUDED.installation_id,
              secret_ciphertext=EXCLUDED.secret_ciphertext,
              metadata=EXCLUDED.metadata,
              last_error=EXCLUDED.last_error,
              updated_at=now()
    `, orgID, string(c.Provider), string(c.Status), c.InstallationID, secret, metaJSON, c.LastError)
	return err
}

// Get returns the Connection AND the encrypted secret bytes for the given
// (org_id, provider). Returns domain.ErrNotFound if no row matches.
func (r *IntegrationsRepo) Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error) {
	q := db.FromCtx(ctx, r.pool)
	var c domain.Connection
	var meta []byte
	var secret []byte
	err := q.QueryRow(ctx, `
        SELECT provider, status, installation_id, metadata, last_error, created_at, updated_at, secret_ciphertext
        FROM integrations WHERE org_id=$1 AND provider=$2
    `, orgID, string(provider)).Scan(
		(*string)(&c.Provider), (*string)(&c.Status), &c.InstallationID, &meta, &c.LastError, &c.CreatedAt, &c.UpdatedAt, &secret,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Connection{}, nil, domain.ErrNotFound
	}
	if err != nil {
		return domain.Connection{}, nil, err
	}
	if len(meta) > 0 {
		_ = json.Unmarshal(meta, &c.Metadata)
	}
	return c, secret, nil
}

// List returns all connections for an org in stable (provider) order. The
// secret blob is intentionally not exposed here — callers fetch it via Get
// when they need to act on it.
func (r *IntegrationsRepo) List(ctx context.Context, orgID string) ([]domain.Connection, error) {
	q := db.FromCtx(ctx, r.pool)
	rows, err := q.Query(ctx, `
        SELECT provider, status, installation_id, metadata, last_error, created_at, updated_at
        FROM integrations WHERE org_id=$1 ORDER BY provider
    `, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Connection{}
	for rows.Next() {
		var c domain.Connection
		var meta []byte
		if err := rows.Scan(
			(*string)(&c.Provider), (*string)(&c.Status), &c.InstallationID,
			&meta, &c.LastError, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &c.Metadata)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Delete removes the (org_id, provider) row. Returns nil even if no row
// existed — the caller can treat "no connection" as the same end state.
func (r *IntegrationsRepo) Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error {
	q := db.FromCtx(ctx, r.pool)
	_, err := q.Exec(ctx, `DELETE FROM integrations WHERE org_id=$1 AND provider=$2`, orgID, string(provider))
	return err
}
