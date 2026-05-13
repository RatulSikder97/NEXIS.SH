package repo

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// InviteCodesRepo persists rows in invite_codes (Phase 8 — public beta).
//
// invite_codes is a SYSTEM-WIDE table — no RLS policy is applied because codes
// are minted by an owner of any org and consumed during signup, BEFORE a
// tenant principal exists. The redemption path takes a SELECT ... FOR UPDATE
// row-level lock so concurrent signups can't push used_count past max_uses.
//
// All paths use adminPool directly — there is no per-tenant variant of these
// queries to route through the app pool.
type InviteCodesRepo struct {
	adminPool *pgxpool.Pool
}

// NewInviteCodesRepo wires an InviteCodesRepo bound to the admin pool. The
// caller is expected to pass a non-nil pool — at boot, main.go skips
// constructing this repo when the admin pool is missing.
func NewInviteCodesRepo(adminPool *pgxpool.Pool) *InviteCodesRepo {
	return &InviteCodesRepo{adminPool: adminPool}
}

// inviteCodeAlphabet is the 32-char Crockford base32 set — lowercase i/l/o/u
// removed to dodge visually-similar pairs when codes are read off a sticker or
// dictated over a call. Total entropy at 12 chars: log2(32^12) = 60 bits.
const inviteCodeAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789"

// GenerateCode returns a fresh 12-char base32 code. crypto/rand failure is
// fatal — we don't want to fall back to math/rand and quietly weaken entropy.
func GenerateCode() (string, error) {
	const n = 12
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = inviteCodeAlphabet[int(b)%len(inviteCodeAlphabet)]
	}
	return string(out), nil
}

// Create inserts an invite code. The caller is responsible for generating the
// code string (see GenerateCode). On unique-violation the caller should retry
// with a fresh code — the 60-bit entropy makes that path practically dead.
func (r *InviteCodesRepo) Create(ctx context.Context, c domain.InviteCode) error {
	if c.MaxUses < 1 {
		c.MaxUses = 1
	}
	_, err := r.adminPool.Exec(ctx, `
        INSERT INTO invite_codes (code, max_uses, used_count, expires_at, created_by, created_at)
        VALUES ($1, $2, $3, $4, $5, COALESCE($6, now()))`,
		c.Code, c.MaxUses, c.UsedCount, c.ExpiresAt, c.CreatedBy, nullTime(c.CreatedAt),
	)
	return err
}

// List returns every code (active + expired + exhausted). Used by the
// owner-only admin view. Newest first.
//
// The caller filters out revoked/expired rows for display via InviteCode.Active.
func (r *InviteCodesRepo) List(ctx context.Context) ([]domain.InviteCode, error) {
	rows, err := r.adminPool.Query(ctx, `
        SELECT code, max_uses, used_count, expires_at, created_by, created_at
        FROM invite_codes
        ORDER BY created_at DESC
        LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.InviteCode{}
	for rows.Next() {
		var c domain.InviteCode
		var expires *time.Time
		var createdBy *string
		if err := rows.Scan(&c.Code, &c.MaxUses, &c.UsedCount, &expires, &createdBy, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.ExpiresAt = expires
		c.CreatedBy = createdBy
		out = append(out, c)
	}
	return out, rows.Err()
}

// Revoke marks a code as revoked by setting expires_at = now(). The row stays
// in the table so the audit chain (separate from invite_codes) keeps a stable
// FK target. Returns ErrNotFound when the code never existed.
func (r *InviteCodesRepo) Revoke(ctx context.Context, code string) error {
	tag, err := r.adminPool.Exec(ctx, `
        UPDATE invite_codes
           SET expires_at = now()
         WHERE code = $1`, code)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ErrInviteInvalid is returned by Redeem when the code is unknown, expired,
// or already used to its cap. The HTTP layer maps this to 403/400.
var ErrInviteInvalid = errors.New("invite code invalid or exhausted")

// Redeem atomically validates + increments used_count under SELECT ... FOR
// UPDATE so two parallel signups racing the same code can't both succeed when
// used_count would exceed max_uses.
//
// Returns ErrInviteInvalid for any failure that should surface to the caller
// as "bad code" — unknown / expired / used up — without leaking which.
func (r *InviteCodesRepo) Redeem(ctx context.Context, code string) error {
	tx, err := r.adminPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	// Idempotent rollback per the pgx pattern used elsewhere in this repo
	// (see workspaces_repo.go for the same shape).
	defer func() { _ = tx.Rollback(ctx) }()

	var maxUses, usedCount int
	var expiresAt *time.Time
	err = tx.QueryRow(ctx, `
        SELECT max_uses, used_count, expires_at
        FROM invite_codes
        WHERE code = $1
        FOR UPDATE`, code).Scan(&maxUses, &usedCount, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInviteInvalid
	}
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if usedCount >= maxUses {
		return ErrInviteInvalid
	}
	if expiresAt != nil && !expiresAt.After(now) {
		return ErrInviteInvalid
	}
	if _, err := tx.Exec(ctx,
		`UPDATE invite_codes SET used_count = used_count + 1 WHERE code = $1`, code); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// NormalizeCode upper-cases + trims whitespace so an invite code dictated
// over the phone is forgiving on signup. Stored form is always upper case,
// so the query is exact-match against this canonicalisation.
func NormalizeCode(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}
