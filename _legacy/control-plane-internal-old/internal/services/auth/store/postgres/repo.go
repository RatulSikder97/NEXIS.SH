// Package postgres provides Postgres implementations of the auth service repos.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"nexis/backend/internal/platform/pg"
	platerrors "nexis/backend/internal/platform/errors"
	"nexis/backend/internal/services/auth/model"
)

type Repo struct {
	pool *pg.Pool
}

func New(pool *pg.Pool) *Repo {
	return &Repo{pool: pool}
}

// ----- Users -----

func (r *Repo) UpsertUserFromClerk(ctx context.Context, clerkUserID, email, name, avatarURL string) (model.User, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (clerk_user_id, email, name, avatar_url)
		VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''))
		ON CONFLICT (clerk_user_id) DO UPDATE
		  SET email = EXCLUDED.email,
		      name = COALESCE(EXCLUDED.name, users.name),
		      avatar_url = COALESCE(EXCLUDED.avatar_url, users.avatar_url)
		RETURNING id, clerk_user_id, email, COALESCE(name,''), COALESCE(avatar_url,''), created_at
	`, clerkUserID, email, name, avatarURL)
	var u model.User
	if err := row.Scan(&u.ID, &u.ClerkUserID, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt); err != nil {
		return model.User{}, fmt.Errorf("upsert user: %w", err)
	}
	return u, nil
}

func (r *Repo) GetUserByClerkID(ctx context.Context, clerkUserID string) (model.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, clerk_user_id, email, COALESCE(name,''), COALESCE(avatar_url,''), created_at
		  FROM users WHERE clerk_user_id = $1 AND deleted_at IS NULL
	`, clerkUserID)
	var u model.User
	if err := row.Scan(&u.ID, &u.ClerkUserID, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, platerrors.New(platerrors.KindNotFound, "user not found")
		}
		return model.User{}, err
	}
	return u, nil
}

func (r *Repo) SoftDeleteUserByClerkID(ctx context.Context, clerkUserID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET deleted_at = now() WHERE clerk_user_id = $1`, clerkUserID)
	return err
}

// ----- Orgs -----

func (r *Repo) UpsertOrgFromClerk(ctx context.Context, clerkOrgID, name, slug string) (model.Org, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO orgs (clerk_org_id, name, slug)
		VALUES ($1, $2, $3)
		ON CONFLICT (clerk_org_id) DO UPDATE
		  SET name = EXCLUDED.name,
		      slug = EXCLUDED.slug
		RETURNING id, COALESCE(clerk_org_id,''), name, slug, plan, created_at
	`, clerkOrgID, name, slug)
	var o model.Org
	if err := row.Scan(&o.ID, &o.ClerkOrgID, &o.Name, &o.Slug, &o.Plan, &o.CreatedAt); err != nil {
		return model.Org{}, fmt.Errorf("upsert org: %w", err)
	}
	return o, nil
}

func (r *Repo) GetOrgByClerkID(ctx context.Context, clerkOrgID string) (model.Org, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, COALESCE(clerk_org_id,''), name, slug, plan, created_at
		  FROM orgs WHERE clerk_org_id = $1 AND deleted_at IS NULL
	`, clerkOrgID)
	var o model.Org
	if err := row.Scan(&o.ID, &o.ClerkOrgID, &o.Name, &o.Slug, &o.Plan, &o.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Org{}, platerrors.New(platerrors.KindNotFound, "org not found")
		}
		return model.Org{}, err
	}
	return o, nil
}

func (r *Repo) GetOrgByID(ctx context.Context, orgID string) (model.Org, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, COALESCE(clerk_org_id,''), name, slug, plan, created_at
		  FROM orgs WHERE id = $1 AND deleted_at IS NULL
	`, orgID)
	var o model.Org
	if err := row.Scan(&o.ID, &o.ClerkOrgID, &o.Name, &o.Slug, &o.Plan, &o.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Org{}, platerrors.New(platerrors.KindNotFound, "org not found")
		}
		return model.Org{}, err
	}
	return o, nil
}

// ----- Memberships -----

func (r *Repo) UpsertMembership(ctx context.Context, orgID, userID string, role model.Role) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO org_members (org_id, user_id, role)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role
	`, orgID, userID, string(role))
	return err
}

func (r *Repo) DeleteMembership(ctx context.Context, orgID, userID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID)
	return err
}

func (r *Repo) MembershipsForUser(ctx context.Context, userID string) ([]model.OrgSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT o.id, o.name, o.slug, m.role
		  FROM orgs o
		  JOIN org_members m ON m.org_id = o.id
		 WHERE m.user_id = $1::uuid AND o.deleted_at IS NULL
		 ORDER BY o.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.OrgSummary
	for rows.Next() {
		var s model.OrgSummary
		var role string
		if err := rows.Scan(&s.ID, &s.Name, &s.Slug, &role); err != nil {
			return nil, err
		}
		s.Role = model.Role(role)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repo) RoleOf(ctx context.Context, orgID, userID string) (model.Role, error) {
	row := r.pool.QueryRow(ctx, `SELECT role FROM org_members WHERE org_id = $1::uuid AND user_id = $2::uuid`, orgID, userID)
	var role string
	if err := row.Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", platerrors.New(platerrors.KindForbidden, "not a member of org")
		}
		return "", err
	}
	return model.Role(role), nil
}

// ----- API Keys -----

func (r *Repo) CreateAPIKey(ctx context.Context, k model.APIKey, hash string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO api_keys (org_id, created_by_user_id, name, key_hash, key_prefix, scopes, expires_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7)
	`, k.OrgID, k.CreatedByUserID, k.Name, hash, k.Prefix, k.Scopes, k.ExpiresAt)
	return err
}

func (r *Repo) ListAPIKeys(ctx context.Context, orgID string) ([]model.APIKey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, org_id, created_by_user_id, name, key_prefix, COALESCE(scopes::text,'[]'),
		       expires_at, created_at, last_used_at, revoked_at
		  FROM api_keys WHERE org_id = $1::uuid ORDER BY created_at DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.APIKey
	for rows.Next() {
		var k model.APIKey
		var scopesJSON string
		var lastUsed, revoked *time.Time
		if err := rows.Scan(&k.ID, &k.OrgID, &k.CreatedByUserID, &k.Name, &k.Prefix, &scopesJSON, &k.ExpiresAt, &k.CreatedAt, &lastUsed, &revoked); err != nil {
			return nil, err
		}
		k.LastUsedAt = lastUsed
		k.RevokedAt = revoked
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *Repo) RevokeAPIKey(ctx context.Context, id, orgID string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE api_keys SET revoked_at = now() WHERE id = $1::uuid AND org_id = $2::uuid AND revoked_at IS NULL`, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return platerrors.New(platerrors.KindNotFound, "api key not found or already revoked")
	}
	return nil
}

// ----- Webhook idempotency -----

func (r *Repo) RecordWebhookEvent(ctx context.Context, eventID, eventType string, payload []byte) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO clerk_webhook_events (clerk_event_id, event_type, payload)
		VALUES ($1, $2, $3)
		ON CONFLICT (clerk_event_id) DO NOTHING
	`, eventID, eventType, payload)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
