package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// PGStore is the pgx-backed implementation of Store used in production. It
// queries Postgres directly without an ORM; the schema mirrors
// migrations/0002_phase2_auth.up.sql.
//
// Stage 3 — RLS routing:
//
// The PGStore is supplied with an *admin* pool (the role that owns the
// schema). Methods that bootstrap pre-session state — organizations, users,
// org_members, sessions, magic_tokens — must run against the pool directly:
// they execute before a principal exists, so the RLS middleware has no
// org_id to pin and no per-request tx to thread.
//
// Methods that read/write tenant tables AFTER auth (today: api_keys CRUD;
// Stage 4 will add audit_log writes) call qry(ctx). qry returns the
// request-scoped pgx.Tx attached by the RLS middleware (which already issued
// `SET LOCAL app.current_org_id`) if one is present, otherwise falls back to
// the pool. That fallback path is exercised only by the memStore-backed unit
// tests and by direct adapter callers — production traffic always carries a
// tx in ctx.
//
// TODO(testing): add `pgstore_test.go` (build-tag `integration`) once
// DATABASE_URL_TEST is wired into CI. RLS coverage for now lives in
// tests/integration/rls_test.go.
type PGStore struct {
	pool *pgxpool.Pool
}

// qry returns the request-scoped pgx.Tx if one is attached to ctx (see
// internal/platform/db/tx.go), otherwise the owning pool. Use this in every
// tenant-table query so RLS is honoured automatically.
func (s *PGStore) qry(ctx context.Context) db.Querier { return db.FromCtx(ctx, s.pool) }

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

// Tenant-table ops below route through s.qry(ctx) so the per-request RLS tx
// (with SET LOCAL app.current_org_id) is used when present. See the package
// doc on PGStore for the bootstrap-vs-tenant split.

func (s *PGStore) CreateAPIKey(ctx context.Context, k *domain.APIKey, hash []byte) error {
	const q = `
		INSERT INTO api_keys (id, org_id, user_id, prefix, hash, scopes, name, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`
	scopes := k.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return s.qry(ctx).QueryRow(ctx, q,
		k.ID, k.OrgID, k.UserID, k.Prefix, hash, scopes, k.Name, k.CreatedAt,
	).Scan(&k.ID, &k.CreatedAt)
}

func (s *PGStore) GetAPIKeyByHash(ctx context.Context, hash []byte) (*domain.APIKey, error) {
	const q = `
		SELECT id, org_id, user_id, prefix, name, scopes, created_at, last_used_at, revoked_at
		FROM api_keys WHERE hash = $1`
	var k domain.APIKey
	err := s.qry(ctx).QueryRow(ctx, q, hash).Scan(
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
	rows, err := s.qry(ctx).Query(ctx, q, orgID)
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
	tag, err := s.qry(ctx).Exec(ctx, q, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// --- invites (Phase 3) -----------------------------------------------------
//
// org_invites is RLS-protected — every method routes through s.qry(ctx) so
// the per-request tx (with SET LOCAL app.current_org_id) is used when one is
// present. Routes that hit these methods always run after RequireAuth + RLS,
// so the tenant pin is always in place in production.

// CreateInvite inserts a new pending invite. PK is token_hash.
func (s *PGStore) CreateInvite(ctx context.Context, i *domain.Invite) error {
	const q = `
		INSERT INTO org_invites (token_hash, org_id, email, role, inviter_user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := s.qry(ctx).Exec(ctx, q,
		i.TokenHash, i.OrgID, i.Email, string(i.Role), i.InviterUserID, i.ExpiresAt,
	)
	return err
}

// GetInvite returns the row for the given token hash. We deliberately do NOT
// route this through the tenant-scoped qry — the claim flow runs before the
// claimer has a session, so app.current_org_id is unset and an RLS-bound query
// would return zero rows. Reading directly off the pool sidesteps that. The
// token itself is unguessable (32 random bytes), so we lose no security by
// skipping RLS here.
func (s *PGStore) GetInvite(ctx context.Context, tokenHash []byte) (*domain.Invite, error) {
	const q = `
		SELECT token_hash, org_id, email, role, inviter_user_id, expires_at, claimed_at
		FROM org_invites WHERE token_hash = $1`
	var i domain.Invite
	var role string
	err := s.pool.QueryRow(ctx, q, tokenHash).Scan(
		&i.TokenHash, &i.OrgID, &i.Email, &role, &i.InviterUserID, &i.ExpiresAt, &i.ClaimedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	i.Role = domain.Role(role)
	return &i, nil
}

// ListInvites returns rows for orgID ordered by expires_at DESC. RLS-bound
// via qry(ctx) — the admin/owner listing the invites must already be signed
// into the org.
func (s *PGStore) ListInvites(ctx context.Context, orgID string) ([]domain.Invite, error) {
	const q = `
		SELECT token_hash, org_id, email, role, inviter_user_id, expires_at, claimed_at
		FROM org_invites WHERE org_id = $1
		ORDER BY expires_at DESC`
	rows, err := s.qry(ctx).Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Invite{}
	for rows.Next() {
		var i domain.Invite
		var role string
		if err := rows.Scan(&i.TokenHash, &i.OrgID, &i.Email, &role, &i.InviterUserID, &i.ExpiresAt, &i.ClaimedAt); err != nil {
			return nil, err
		}
		i.Role = domain.Role(role)
		out = append(out, i)
	}
	return out, rows.Err()
}

// MarkInviteClaimed sets claimed_at to now() for the matching token hash.
// Pool-bound (same rationale as GetInvite) — the claimer has no session yet.
func (s *PGStore) MarkInviteClaimed(ctx context.Context, tokenHash []byte) error {
	const q = `UPDATE org_invites SET claimed_at = now() WHERE token_hash = $1 AND claimed_at IS NULL`
	tag, err := s.pool.Exec(ctx, q, tokenHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteInvite removes an invite by token hash. RLS-bound via qry(ctx) —
// revocation only happens through the authenticated admin/owner endpoint.
func (s *PGStore) DeleteInvite(ctx context.Context, tokenHash []byte) error {
	const q = `DELETE FROM org_invites WHERE token_hash = $1`
	tag, err := s.qry(ctx).Exec(ctx, q, tokenHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// GetUserPreferences reads the users.preferences jsonb column. Runs against
// the admin pool because users is not org-scoped (preferences are a per-user
// attribute, not a per-tenant one) and the row may need to be fetched even
// when an RLS-bound tx isn't open.
func (s *PGStore) GetUserPreferences(ctx context.Context, userID string) (map[string]any, error) {
	const q = `SELECT COALESCE(preferences, '{}'::jsonb)::text FROM users WHERE id = $1`
	var raw string
	if err := s.pool.QueryRow(ctx, q, userID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if raw == "" {
		return map[string]any{}, nil
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("pgstore: unmarshal preferences: %w", err)
	}
	return out, nil
}

// UpdateUserPreferences performs a last-write-wins overwrite of the row's
// preferences column. The UI submits the full preferences object so a merge
// is unnecessary; if a future caller needs field-level merge, do it in the
// handler before calling this method.
func (s *PGStore) UpdateUserPreferences(ctx context.Context, userID string, prefs map[string]any) error {
	if prefs == nil {
		prefs = map[string]any{}
	}
	b, err := json.Marshal(prefs)
	if err != nil {
		return fmt.Errorf("pgstore: marshal preferences: %w", err)
	}
	const q = `UPDATE users SET preferences = $1::jsonb WHERE id = $2`
	tag, err := s.pool.Exec(ctx, q, b, userID)
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
