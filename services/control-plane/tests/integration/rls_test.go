//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
)

// TestRLS_APIKeysTenantIsolation verifies the Stage 3 acceptance:
//
//   - Two orgs signed up through /v1/auth/signup each see ONLY their own keys
//     via GET /v1/apikeys (RLS on api_keys via the per-request tx with
//     SET LOCAL app.current_org_id).
//   - A raw query as the nexis_app role outside any tx returns zero keys
//     (proving the policy is engaged at the database level).
//
// The test requires two URLs:
//
//   - DATABASE_URL_TEST: the admin (nexis superuser) URL. Drives signup —
//     org/user/membership/sessions writes happen here, RLS-free because
//     superusers bypass RLS by default. Test skips if unset.
//   - DATABASE_URL_TEST_APP: optional; the nexis_app URL used by the RLS
//     middleware. Defaults to the same host/db with nexis_app credentials.
func TestRLS_APIKeysTenantIsolation(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL_TEST")
	if adminURL == "" {
		t.Skip("DATABASE_URL_TEST unset — skipping RLS integration test")
	}
	appURL := os.Getenv("DATABASE_URL_TEST_APP")
	if appURL == "" {
		// Default: same db, swap creds. Matches the dev-compose conventions
		// from infra/postgres/init/01-nexis-app-role.sql.
		appURL = swapCredsToNexisApp(t, adminURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin pool: %v", err)
	}
	// Close via t.Cleanup so it runs AFTER the cleanupTestData callback that
	// is registered below (Cleanup runs in LIFO order; defers would close the
	// pool before cleanup got a chance to use it).
	t.Cleanup(func() { adminPool.Close() })

	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatalf("connect app pool: %v", err)
	}
	t.Cleanup(func() { appPool.Close() })

	// Track created users/orgs so we can clean up regardless of test outcome.
	// Pointer receiver so the cleanup callback closes over the live slice
	// rather than its empty zero-value snapshot.
	var createdEmails []string
	t.Cleanup(func() { cleanupTestData(t, adminPool, createdEmails) })

	// Build the auth provider on the ADMIN pool — signup needs to write to
	// RLS-protected tables (org_members, sessions) before any principal
	// exists. The nexis superuser bypasses RLS, so this is the only path
	// that works.
	provider := local.New(local.Config{
		Store:         local.NewPGStore(adminPool),
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        &local.TestMailer{},
		BaseURL:       "http://localhost:3000",
	})

	srv := httpserver.New(
		config.Config{AppEnv: "test", AppBaseURL: "http://localhost:3000"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpserver.Deps{
			Pool:    adminPool, // admin (used by Provider's pgstore for bootstrap ops)
			AppPool: appPool,   // app role (used by RLS middleware for tx)
			Auth:    provider,
		},
	)

	// Signup two distinct orgs through the HTTP surface. Org names are
	// uniqued because the slug column has a UNIQUE constraint and re-running
	// the test would otherwise clash with leftover rows from a prior failed
	// run that didn't clean up.
	emailA := uniqueEmail(t, "rls-a")
	emailB := uniqueEmail(t, "rls-b")
	createdEmails = append(createdEmails, emailA, emailB)

	stamp := time.Now().UTC().Format("20060102150405.000000")
	cookieA := rlsSignup(t, srv, emailA, "OrgA-"+stamp)
	cookieB := rlsSignup(t, srv, emailB, "OrgB-"+stamp)

	// Each org creates two api keys.
	createAPIKey(t, srv, cookieA, "A-key-1")
	createAPIKey(t, srv, cookieA, "A-key-2")
	createAPIKey(t, srv, cookieB, "B-key-1")
	createAPIKey(t, srv, cookieB, "B-key-2")

	// A's list contains exactly 2 entries, all bound to A's org.
	keysA := listAPIKeys(t, srv, cookieA)
	if len(keysA) != 2 {
		t.Fatalf("org A keys: got %d, want 2 (RLS leakage?): %+v", len(keysA), keysA)
	}

	keysB := listAPIKeys(t, srv, cookieB)
	if len(keysB) != 2 {
		t.Fatalf("org B keys: got %d, want 2 (RLS leakage?): %+v", len(keysB), keysB)
	}
	for i, k := range append(append([]dto.APIKeyResp{}, keysA...), keysB...) {
		if k.ID == "" {
			t.Fatalf("listing[%d] has empty ID: %+v", i, k)
		}
	}

	for _, k := range keysA {
		for _, kb := range keysB {
			if k.ID == kb.ID {
				t.Fatalf("RLS leak: key %s visible to both orgs", k.ID)
			}
		}
	}

	// Raw query as nexis_app outside any tx → no SET LOCAL → policy returns
	// zero rows. Use a fresh connection (not a tx) and assert count(*)=0 for
	// the keys we just created. We filter by the test's user_ids so this
	// remains stable if the table has rows from other dev work.
	if got := countAPIKeysForTest(t, ctx, appPool, keysA, keysB); got != 0 {
		t.Fatalf("raw query without tx/GUC returned %d rows — RLS not blocking", got)
	}
}

// rlsSignup posts to /v1/auth/signup and returns the resulting session cookie.
// Distinct from auth_handlers_test.go's signup helper because that one uses
// fixed-clock memStore — here we hit the real pgstore.
func rlsSignup(t *testing.T, srv http.Handler, email, orgName string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": "rls-test-pw",
		"org_name": orgName,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup %s: status=%d body=%s", email, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "nexis_session" {
			return c
		}
	}
	t.Fatalf("signup %s: no session cookie set", email)
	return nil
}

// createAPIKey posts to /v1/apikeys with the given cookie. This path runs
// THROUGH the RLS middleware, so the INSERT happens inside the per-request tx
// with SET LOCAL app.current_org_id pinned to the cookie's principal.
func createAPIKey(t *testing.T, srv http.Handler, cookie *http.Cookie, name string) {
	t.Helper()
	body, _ := json.Marshal(dto.CreateAPIKeyReq{Name: name})
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create api key %q: status=%d body=%s", name, rec.Code, rec.Body.String())
	}
}

// listAPIKeys hits GET /v1/apikeys, decoding into the public DTO. The list
// the RLS-enforced tx returns is what the test asserts on.
func listAPIKeys(t *testing.T, srv http.Handler, cookie *http.Cookie) []dto.APIKeyResp {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/apikeys", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list api keys: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out []dto.APIKeyResp
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return out
}

// countAPIKeysForTest runs a bare SELECT against the app pool without a tx or
// SET LOCAL. RLS should evaluate `org_id = NULL::uuid` for every row, which is
// NULL (falsy), so the function expects 0. The filter on the seeded IDs keeps
// the assertion stable if the table is dirty.
func countAPIKeysForTest(t *testing.T, ctx context.Context, appPool *pgxpool.Pool, keysA, keysB []dto.APIKeyResp) int {
	t.Helper()
	ids := make([]string, 0, len(keysA)+len(keysB))
	for _, k := range keysA {
		ids = append(ids, k.ID)
	}
	for _, k := range keysB {
		ids = append(ids, k.ID)
	}
	if len(ids) == 0 {
		return 0
	}
	conn, err := appPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire app conn: %v", err)
	}
	defer conn.Release()
	var n int
	// Cast id to text on both sides so we don't depend on pgx's uuid array
	// encoding choice. The count is over the test-seeded ids only — if the
	// table is dirty from other dev work, this still gives a stable answer.
	if err := conn.QueryRow(ctx,
		`SELECT count(*) FROM api_keys WHERE id::text = ANY($1)`, ids,
	).Scan(&n); err != nil {
		t.Fatalf("raw count: %v", err)
	}
	return n
}

// cleanupTestData deletes everything the test inserted via the admin pool
// (which bypasses RLS). Ordering matters for FK chains: api_keys + sessions +
// org_members → users; organizations referencing users via owner_user_id is
// cleared by setting it null first.
func cleanupTestData(t *testing.T, adminPool *pgxpool.Pool, emails []string) {
	t.Helper()
	if len(emails) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tx, err := adminPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Logf("cleanup: begin tx: %v", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Resolve test user ids.
	rows, err := tx.Query(ctx, `SELECT id FROM users WHERE email = ANY($1)`, emails)
	if err != nil {
		t.Logf("cleanup: lookup users: %v", err)
		return
	}
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Logf("cleanup: scan user id: %v", err)
			return
		}
		userIDs = append(userIDs, id)
	}
	rows.Close()
	if len(userIDs) == 0 {
		_ = tx.Commit(ctx)
		return
	}

	// Collect the org IDs we're about to detach so the org row can be removed
	// after FKs are cleared. The list is bounded by len(userIDs).
	orgRows, err := tx.Query(ctx,
		`SELECT DISTINCT org_id FROM org_members WHERE user_id = ANY($1)`, userIDs)
	if err != nil {
		t.Logf("cleanup: lookup orgs: %v", err)
		return
	}
	var orgIDs []string
	for orgRows.Next() {
		var id string
		if err := orgRows.Scan(&id); err != nil {
			orgRows.Close()
			t.Logf("cleanup: scan org id: %v", err)
			return
		}
		orgIDs = append(orgIDs, id)
	}
	orgRows.Close()

	type step struct {
		sql  string
		args []any
	}
	steps := []step{
		{`DELETE FROM api_keys WHERE user_id = ANY($1)`, []any{userIDs}},
		{`DELETE FROM sessions WHERE user_id = ANY($1)`, []any{userIDs}},
		{`DELETE FROM magic_tokens WHERE user_id = ANY($1)`, []any{userIDs}},
		{`DELETE FROM org_members WHERE user_id = ANY($1)`, []any{userIDs}},
		// Owner-FK guard: NULL the column so DELETE on users doesn't trip.
		{`UPDATE organizations SET owner_user_id = NULL WHERE owner_user_id = ANY($1)`, []any{userIDs}},
		{`DELETE FROM users WHERE id = ANY($1)`, []any{userIDs}},
	}
	if len(orgIDs) > 0 {
		// Drop the orgs only after their FKs are unwound. Skip if there are
		// no orgs to delete to avoid the empty-array edge case.
		steps = append(steps, step{`DELETE FROM organizations WHERE id = ANY($1)`, []any{orgIDs}})
	}
	for _, st := range steps {
		if _, err := tx.Exec(ctx, st.sql, st.args...); err != nil {
			t.Logf("cleanup %q: %v", strings.SplitN(st.sql, " ", 3)[1], err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Logf("cleanup commit: %v", err)
	}
}

// uniqueEmail returns a slug-safe email that won't collide across reruns of
// the same test or parallel CI shards.
func uniqueEmail(t *testing.T, prefix string) string {
	t.Helper()
	return prefix + "-" + time.Now().UTC().Format("20060102150405.000000") + "@rls.test"
}

// swapCredsToNexisApp rewrites the credentials in a libpq-style URL to the
// nexis_app role + dev password. Only used when DATABASE_URL_TEST_APP isn't
// explicitly set.
func swapCredsToNexisApp(t *testing.T, src string) string {
	t.Helper()
	// Pattern: postgres://user:pass@host:port/db?…
	const scheme = "postgres://"
	if !strings.HasPrefix(src, scheme) {
		t.Fatalf("DATABASE_URL_TEST not a postgres:// URL: %q", src)
	}
	rest := src[len(scheme):]
	at := strings.Index(rest, "@")
	if at < 0 {
		t.Fatalf("DATABASE_URL_TEST missing credentials: %q", src)
	}
	return scheme + "nexis_app:nexis_app_dev_password@" + rest[at+1:]
}
