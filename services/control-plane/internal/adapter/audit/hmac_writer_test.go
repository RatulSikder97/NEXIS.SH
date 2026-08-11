//go:build integration

package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// newTestPool connects to DATABASE_URL_TEST (admin role — bypasses RLS so the
// test can read audit_log directly). The pool is closed via t.Cleanup so the
// test still gets a chance to inspect rows in cleanup paths.
func newTestPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("DATABASE_URL_TEST unset — skipping audit HMAC integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool, ctx
}

// seedOrg inserts a throwaway org + user pair via the admin pool. Returns the
// org id, which the caller threads into Write as Principal.OrgID. Cleanup
// removes the audit rows and the org/user inserted here.
func seedOrg(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (orgID, userID string) {
	t.Helper()

	stamp := time.Now().UTC().Format("20060102150405.000000000")
	email := "audit-" + stamp + "@audit.test"
	orgName := "AuditOrg-" + stamp
	slug := "auditorg-" + stamp

	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email) VALUES ($1) RETURNING id`, email,
	).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO organizations (name, slug, owner_user_id) VALUES ($1, $2, $3) RETURNING id`,
		orgName, slug, userID,
	).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}

	t.Cleanup(func() {
		// LIFO order: this runs before pool.Close.
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(c, `DELETE FROM audit_log WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(c, `UPDATE organizations SET owner_user_id = NULL WHERE id = $1`, orgID)
		_, _ = pool.Exec(c, `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id = $1`, userID)
	})

	return orgID, userID
}

// TestHMACWriter_AppendsChainedRows writes two audit rows for the same org and
// asserts:
//
//  1. row 2's prev_hash equals row 1's row_hash (chain linkage)
//  2. each row's row_hash matches HMAC(secret, prev || canonicalJSON(payload))
//     when we recompute it from the persisted columns.
//
// Row 1 has NULL prev_hash and is HMAC'd over nil || payload.
func TestHMACWriter_AppendsChainedRows(t *testing.T) {
	pool, ctx := newTestPool(t)
	orgID, userID := seedOrg(t, ctx, pool)

	secret := []byte("audit-test-secret-not-for-prod-12345678")
	w := New(secret, pool)
	p := domain.Principal{UserID: userID, OrgID: orgID, Role: domain.RoleOwner}

	if err := w.Write(ctx, p, "user.signup", userID, map[string]any{"email": "x@y"}); err != nil {
		t.Fatalf("write row 1: %v", err)
	}
	if err := w.Write(ctx, p, "user.login", userID, map[string]any{"email": "x@y"}); err != nil {
		t.Fatalf("write row 2: %v", err)
	}

	type row struct {
		id        string
		actor     string
		action    string
		target    string
		metaRaw   []byte
		prev      []byte
		rowHash   []byte
		createdAt time.Time
	}
	rows, err := pool.Query(ctx, `
        SELECT id, actor, action, target, metadata, prev_hash, row_hash, created_at
        FROM audit_log
        WHERE org_id = $1
        ORDER BY created_at`, orgID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var got []row
	for rows.Next() {
		var r row
		var tgt *string
		if err := rows.Scan(&r.id, &r.actor, &r.action, &tgt, &r.metaRaw, &r.prev, &r.rowHash, &r.createdAt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if tgt != nil {
			r.target = *tgt
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}

	// Row 1: prev_hash IS NULL.
	if got[0].prev != nil {
		t.Errorf("row 1 prev_hash = %x, want NULL", got[0].prev)
	}
	// Row 2: prev_hash == row 1.row_hash.
	if !hmac.Equal(got[1].prev, got[0].rowHash) {
		t.Errorf("chain broken: row2.prev=%x, want row1.row_hash=%x", got[1].prev, got[0].rowHash)
	}

	// Recompute each row_hash from persisted columns and compare. The exact
	// same canonicalJSON path that Write uses must round-trip here.
	for i, r := range got {
		var meta map[string]any
		if len(r.metaRaw) > 0 {
			_ = json.Unmarshal(r.metaRaw, &meta)
		}
		payload := map[string]any{
			"id":         r.id,
			"org_id":     orgID,
			"actor":      r.actor,
			"action":     r.action,
			"target":     r.target,
			"metadata":   meta,
			"created_at": r.createdAt.UTC().Format(time.RFC3339Nano),
		}
		mac := hmac.New(sha256.New, secret)
		mac.Write(r.prev) // nil for i==0; row1.row_hash for i==1
		mac.Write(canonicalJSON(payload))
		want := mac.Sum(nil)
		if !hmac.Equal(want, r.rowHash) {
			t.Errorf("row %d hash mismatch:\n want=%x\n  got=%x", i, want, r.rowHash)
		}
	}
}

// TestVerify_DetectsTamper writes two rows then mutates row 1's action column.
// Verify must report OK=false with FirstBadRowID == row 1's id (because the
// recomputed HMAC over the mutated payload no longer matches row_hash).
func TestVerify_DetectsTamper(t *testing.T) {
	pool, ctx := newTestPool(t)
	orgID, userID := seedOrg(t, ctx, pool)

	secret := []byte("audit-test-secret-not-for-prod-12345678")
	w := New(secret, pool)
	p := domain.Principal{UserID: userID, OrgID: orgID, Role: domain.RoleOwner}

	if err := w.Write(ctx, p, "user.signup", userID, map[string]any{"email": "x@y"}); err != nil {
		t.Fatalf("write row 1: %v", err)
	}
	if err := w.Write(ctx, p, "user.login", userID, map[string]any{"email": "x@y"}); err != nil {
		t.Fatalf("write row 2: %v", err)
	}

	// Baseline: chain is intact across the whole table.
	res, err := Verify(ctx, pool, secret)
	if err != nil {
		t.Fatalf("verify (clean): %v", err)
	}
	if !res.OK {
		t.Fatalf("verify (clean): OK=false, FirstBadRowID=%q rows=%d", res.FirstBadRowID, res.RowsChecked)
	}
	if res.RowsChecked < 2 {
		t.Fatalf("verify (clean): rows_checked=%d, want >= 2", res.RowsChecked)
	}

	// Resolve row 1's id so we can mutate it and assert that exact id surfaces
	// as FirstBadRowID.
	var row1ID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM audit_log WHERE org_id = $1 ORDER BY created_at LIMIT 1`, orgID,
	).Scan(&row1ID); err != nil {
		t.Fatalf("lookup row1: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE audit_log SET action='z' WHERE id=$1`, row1ID); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	res, err = Verify(ctx, pool, secret)
	if err != nil {
		t.Fatalf("verify (tampered): %v", err)
	}
	if res.OK {
		t.Fatal("verify (tampered): OK=true, want false")
	}
	if res.FirstBadRowID != row1ID {
		t.Errorf("verify (tampered): FirstBadRowID=%q, want %q", res.FirstBadRowID, row1ID)
	}
}
