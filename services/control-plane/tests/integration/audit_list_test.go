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

// TestAudit_ListEndpoint exercises GET /v1/audit end-to-end:
//
//  1. Signup writes 1 audit row (user.signup).
//  2. Two CreateAPIKey calls write 2 more (apikey.created).
//  3. GET /v1/audit returns rows + total via the AuditLister wiring. The
//     owner principal lives in the RBAC group, so the route is reachable.
//
// Requires DATABASE_URL_TEST + the nexis_app role (see audit_verify_test.go
// for the same env-var dance).
func TestAudit_ListEndpoint(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL_TEST")
	if adminURL == "" {
		t.Skip("DATABASE_URL_TEST unset — skipping audit list integration test")
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

	var createdOrgIDs []string
	t.Cleanup(func() {
		if len(createdOrgIDs) == 0 {
			return
		}
		c, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		_, _ = adminPool.Exec(c, `DELETE FROM audit_log WHERE org_id = ANY($1)`, createdOrgIDs)
	})

	secret := []byte("audit-list-test-secret-1234567890")
	provider := local.New(local.Config{
		Store:         local.NewPGStore(adminPool),
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        &local.TestMailer{},
		BaseURL:       "http://localhost:3000",
	})

	hw := auditadapter.New(secret, adminPool)
	srv := httpserver.New(
		config.Config{AppEnv: "test", AppBaseURL: "http://localhost:3000", AuditSecret: string(secret)},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpserver.Deps{
			Pool:        adminPool,
			AppPool:     appPool,
			Auth:        provider,
			Audit:       hw,
			AuditLister: hw,
		},
	)

	email := uniqueEmail(t, "audit-list")
	stamp := time.Now().UTC().Format("20060102150405.000000")
	createdEmails = append(createdEmails, email)
	cookie := rlsSignup(t, srv, email, "AuditListOrg-"+stamp)

	var orgID string
	if err := adminPool.QueryRow(ctx,
		`SELECT o.id FROM organizations o JOIN users u ON o.owner_user_id = u.id WHERE u.email = $1`,
		email,
	).Scan(&orgID); err != nil {
		t.Fatalf("lookup org for cleanup: %v", err)
	}
	createdOrgIDs = append(createdOrgIDs, orgID)

	createAPIKey(t, srv, cookie, "list-1")
	createAPIKey(t, srv, cookie, "list-2")

	// Hit the list endpoint.
	req := httptest.NewRequest(http.MethodGet, "/v1/audit", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit list status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Rows  []auditadapter.AuditRow `json:"rows"`
		Total int                     `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if body.Total < 3 {
		t.Errorf("total = %d, want >= 3 (signup + 2× apikey.created)", body.Total)
	}
	if len(body.Rows) == 0 {
		t.Fatalf("rows empty: %+v", body)
	}
	// Each row carries our org_id.
	for i, r := range body.Rows {
		if r.OrgID != orgID {
			t.Errorf("row %d: org_id=%q, want %q (RLS leak?)", i, r.OrgID, orgID)
		}
	}

	// CSV endpoint smoke check: 200, text/csv, header row plus at least one
	// data row.
	req = httptest.NewRequest(http.MethodGet, "/v1/audit.csv", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit csv status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("csv content-type = %q, want text/csv", ct)
	}
	if rec.Body.Len() < len("id,actor,action,target,created_at\n") {
		t.Errorf("csv body suspiciously short: %q", rec.Body.String())
	}
}
