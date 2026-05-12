//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	auditadapter "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/audit"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

// TestAudit_VerifyEndpoint exercises the full Stage 4 chain end-to-end:
//
//  1. Signup writes an audit row OUTSIDE any RLS tx (the signup route is
//     unauthenticated). The HMACWriter falls back to its admin pool.
//  2. Two CreateAPIKey calls each write an audit row INSIDE the RLS tx the
//     middleware opens. The writer reads the tx from ctx and the INSERTs
//     participate in the same atomic boundary as the api_keys INSERT.
//  3. GET /v1/audit/verify hits the admin pool directly (NOT the RLS tx),
//     walks every row, and returns {ok: true, rows_checked: >=3}.
//
// This is the acceptance test for "the chain is verifiable end-to-end".
func TestAudit_VerifyEndpoint(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL_TEST")
	if adminURL == "" {
		t.Skip("DATABASE_URL_TEST unset — skipping audit verify integration test")
	}
	appURL := os.Getenv("DATABASE_URL_TEST_APP")
	if appURL == "" {
		appURL = swapCredsToNexisApp(t, adminURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin pool: %v", err)
	}
	t.Cleanup(func() { adminPool.Close() })

	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatalf("connect app pool: %v", err)
	}
	t.Cleanup(func() { appPool.Close() })

	var createdEmails []string
	t.Cleanup(func() { cleanupTestData(t, adminPool, createdEmails) })
	// Also clean any audit rows we appended — cleanupTestData doesn't touch
	// audit_log because rls_test.go (which owns that helper) predates Stage 4.
	// We collect org ids after signup to drive the deletion.
	var createdOrgIDs []string
	t.Cleanup(func() {
		if len(createdOrgIDs) == 0 {
			return
		}
		c, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		_, _ = adminPool.Exec(c, `DELETE FROM audit_log WHERE org_id = ANY($1)`, createdOrgIDs)
	})

	secret := []byte("audit-verify-test-secret-12345678")
	provider := local.New(local.Config{
		Store:         local.NewPGStore(adminPool),
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        &local.TestMailer{},
		BaseURL:       "http://localhost:3000",
	})

	srv := httpserver.New(
		config.Config{AppEnv: "test", AppBaseURL: "http://localhost:3000", AuditSecret: string(secret)},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpserver.Deps{
			Pool:    adminPool,
			AppPool: appPool,
			Auth:    provider,
			Audit:   auditadapter.New(secret, adminPool),
		},
	)

	// Signup → 1 audit row (user.signup).
	email := uniqueEmail(t, "audit-verify")
	stamp := time.Now().UTC().Format("20060102150405.000000")
	createdEmails = append(createdEmails, email)
	cookie := rlsSignup(t, srv, email, "AuditVerifyOrg-"+stamp)

	// Resolve the org id we just created (need it for audit_log cleanup).
	var orgID string
	if err := adminPool.QueryRow(ctx,
		`SELECT o.id FROM organizations o JOIN users u ON o.owner_user_id = u.id WHERE u.email = $1`,
		email,
	).Scan(&orgID); err != nil {
		t.Fatalf("lookup org for cleanup: %v", err)
	}
	createdOrgIDs = append(createdOrgIDs, orgID)

	// Two api keys → 2 more audit rows (apikey.created), both inside the
	// RLS tx the middleware opens for the request.
	createAPIKey(t, srv, cookie, "ci-1")
	createAPIKey(t, srv, cookie, "ci-2")

	// Hit the verify endpoint with the same session cookie. RequireAuth +
	// RLS sit on this route, but the handler reads the admin pool directly
	// for the chain walk — see handler.AuditVerify for the rationale.
	req := httptest.NewRequest(http.MethodGet, "/v1/audit/verify", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var res auditadapter.VerifyResult
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode verify: %v", err)
	}
	if !res.OK {
		t.Fatalf("verify OK=false, body=%+v", res)
	}
	// The walker reads ALL rows across the table (not just ours), so the
	// total can exceed our 3 if prior test runs left data. We assert >= 3 —
	// any global tamper would make OK=false first regardless.
	if res.RowsChecked < 3 {
		t.Errorf("rows_checked = %d, want >= 3 (signup + 2× apikey.created)", res.RowsChecked)
	}

	// Double-check our 3 rows are actually present in audit_log for the
	// signed-up org — paranoia against the handler silently dropping rows
	// when the RLS tx is in play.
	var ours int
	if err := adminPool.QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE org_id = $1`, orgID,
	).Scan(&ours); err != nil {
		t.Fatalf("count audit_log for org: %v", err)
	}
	if ours < 3 {
		t.Errorf("audit_log rows for org = %d, want >= 3 (signup + 2× apikey.created)", ours)
	}
}
