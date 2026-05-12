package local

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// PGStore is the pgx-backed implementation of Store used in production. It
// queries Postgres directly without an ORM; the schema mirrors
// migrations/0002_phase2_auth.up.sql.
//
// TODO(stage-3): switch every method to accept *pgx.Tx instead of using the
// pool, so the RLS middleware can bind `SET LOCAL app.current_org_id` to the
// same transaction the writes run in.
//
// TODO(testing): add `pgstore_test.go` (build-tag `integration`) once a
// DATABASE_URL_TEST is wired into CI. Coverage for now lives in memStore unit
// tests; the SQL strings here are exercised by Stage 2 integration tests that
// exercise the full HTTP signup→login round trip.
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore constructs a PGStore against the supplied pool.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

// --- org / user / membership ----------------------------------------------

func (s *PGStore) CreateOrganization(ctx context.Context, o *domain.Organization) error {
	const q = `
		INSERT INTO organizations (id, name, slug, owner_user_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`
	return s.pool.QueryRow(ctx, q, o.ID, o.Name, o.Slug, o.OwnerUserID, o.CreatedAt).Scan(&o.ID, &o.CreatedAt)
}

func (s *PGStore) CreateUser(ctx context.Context, u *domain.User) error {
	const q = `
		INSERT INTO users (id, email, password_hash, mfa_enabled, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`
	return s.pool.QueryRow(ctx, q, u.ID, u.Email, u.PasswordHash, u.MFAEnabled, u.CreatedAt).Scan(&u.ID, &u.CreatedAt)
}

func (s *PGStore) CreateMembership(ctx context.Context, orgID, userID string, role domain.Role) error {
	const q = `
		INSERT INTO org_members (org_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role`
	_, err := s.pool.Exec(ctx, q, orgID, userID, string(role))
	return err
}

func (s *PGStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	const q = `
		SELECT id, email, COALESCE(password_hash, ''), COALESCE(mfa_secret, ''),
		       mfa_enabled, email_verified_at, created_at
		FROM users WHERE email = $1`
	var u domain.User
	err := s.pool.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.MFASecret,
		&u.MFAEnabled, &u.EmailVerifiedAt, &u.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PGStore) GetUser(ctx context.Context, id string) (*domain.User, error) {
	const q = `
		SELECT id, email, COALESCE(password_hash, ''), COALESCE(mfa_secret, ''),
		       mfa_enabled, email_verified_at, created_at
		FROM users WHERE id = $1`
	var u domain.User
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.MFASecret,
		&u.MFAEnabled, &u.EmailVerifiedAt, &u.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PGStore) GetOrganization(ctx context.Context, id string) (*domain.Organization, error) {
	const q = `
		SELECT id, name, slug, owner_user_id, created_at
		FROM organizations WHERE id = $1`
	var o domain.Organization
	err := s.pool.QueryRow(ctx, q, id).Scan(&o.ID, &o.Name, &o.Slug, &o.OwnerUserID, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *PGStore) GetMembership(ctx context.Context, userID string) (string, domain.Role, error) {
	// Membership rows live inside an RLS-protected table. The signup path
	// currently runs without `app.current_org_id` set, so this query returns
	// zero rows in that case — callers handling pre-session contexts must rely
	// on Session.OrgID (which is set at signup). Stage 3 will replace this
	// with a transaction-bound SET LOCAL.
	const q = `SELECT org_id, role FROM org_members WHERE user_id = $1 LIMIT 1`
	var orgID string
	var role string
	err := s.pool.QueryRow(ctx, q, userID).Scan(&orgID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", domain.ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	return orgID, domain.Role(role), nil
}

func (s *PGStore) UpdateUserMFASecret(ctx context.Context, userID, secret string) error {
	const q = `UPDATE users SET mfa_secret = $2, mfa_enabled = false WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, userID, secret)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *PGStore) UpdateUserMFAEnabled(ctx context.Context, userID string, enabled bool) error {
	const q = `UPDATE users SET mfa_enabled = $2 WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, userID, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *PGStore) ClearUserMFA(ctx context.Context, userID string) error {
	const q = `UPDATE users SET mfa_secret = NULL, mfa_enabled = false WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// --- sessions --------------------------------------------------------------

func (s *PGStore) CreateSession(ctx context.Context, sess *domain.Session) error {
	const q = `
		INSERT INTO sessions (id, user_id, org_id, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`
	return s.pool.QueryRow(ctx, q, sess.ID, sess.UserID, sess.OrgID, sess.CreatedAt, sess.ExpiresAt).
		Scan(&sess.ID, &sess.CreatedAt)
}

func (s *PGStore) GetSession(ctx context.Context, id string) (*domain.Session, error) {
	const q = `SELECT id, user_id, org_id, created_at, expires_at, revoked_at FROM sessions WHERE id = $1`
	var sess domain.Session
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&sess.ID, &sess.UserID, &sess.OrgID, &sess.CreatedAt, &sess.ExpiresAt, &sess.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *PGStore) RevokeSession(ctx context.Context, id string) error {
	const q = `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// --- magic tokens ----------------------------------------------------------

func (s *PGStore) CreateMagicToken(ctx context.Context, hash []byte, userID, purpose string, expiresAt time.Time) error {
	const q = `
		INSERT INTO magic_tokens (token_hash, user_id, purpose, expires_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (token_hash) DO UPDATE SET expires_at = EXCLUDED.expires_at, used_at = NULL`
	_, err := s.pool.Exec(ctx, q, hash, userID, purpose, expiresAt)
	return err
}

func (s *PGStore) GetMagicToken(ctx context.Context, hash []byte) (string, string, time.Time, *time.Time, error) {
	const q = `SELECT user_id, purpose, expires_at, used_at FROM magic_tokens WHERE token_hash = $1`
	var userID, purpose string
	var expiresAt time.Time
	var usedAt *time.Time
	err := s.pool.QueryRow(ctx, q, hash).Scan(&userID, &purpose, &expiresAt, &usedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", time.Time{}, nil, domain.ErrNotFound
	}
	if err != nil {
		return "", "", time.Time{}, nil, err
	}
	return userID, purpose, expiresAt, usedAt, nil
}

func (s *PGStore) MarkMagicTokenUsed(ctx context.Context, hash []byte) error {
	const q = `UPDATE magic_tokens SET used_at = now() WHERE token_hash = $1 AND used_at IS NULL`
	tag, err := s.pool.Exec(ctx, q, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("mark magic token used: already used or missing: %w", domain.ErrNotFound)
	}
	return nil
}

// --- api keys --------------------------------------------------------------

func (s *PGStore) CreateAPIKey(ctx context.Context, k *domain.APIKey, hash []byte) error {
	const q = `
		INSERT INTO api_keys (id, org_id, user_id, prefix, hash, scopes, name, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`
	scopes := k.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return s.pool.QueryRow(ctx, q,
		k.ID, k.OrgID, k.UserID, k.Prefix, hash, scopes, k.Name, k.CreatedAt,
	).Scan(&k.ID, &k.CreatedAt)
}

func (s *PGStore) GetAPIKeyByHash(ctx context.Context, hash []byte) (*domain.APIKey, error) {
	const q = `
		SELECT id, org_id, user_id, prefix, name, scopes, created_at, last_used_at, revoked_at
		FROM api_keys WHERE hash = $1`
	var k domain.APIKey
	err := s.pool.QueryRow(ctx, q, hash).Scan(
		&k.ID, &k.OrgID, &k.UserID, &k.Prefix, &k.Name, &k.Scopes,
		&k.CreatedAt, &k.LastUsedAt, &k.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (s *PGStore) ListAPIKeysByOrg(ctx context.Context, orgID string) ([]domain.APIKey, error) {
	const q = `
		SELECT id, org_id, user_id, prefix, name, scopes, created_at, last_used_at, revoked_at
		FROM api_keys WHERE org_id = $1
		ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.APIKey
	for rows.Next() {
		var k domain.APIKey
		if err := rows.Scan(
			&k.ID, &k.OrgID, &k.UserID, &k.Prefix, &k.Name, &k.Scopes,
			&k.CreatedAt, &k.LastUsedAt, &k.RevokedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *PGStore) RevokeAPIKey(ctx context.Context, orgID, id string) error {
	const q = `UPDATE api_keys SET revoked_at = now() WHERE id = $1 AND org_id = $2 AND revoked_at IS NULL`
	tag, err := s.pool.Exec(ctx, q, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// compile-time conformance check
var _ Store = (*PGStore)(nil)
