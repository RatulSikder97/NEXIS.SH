//go:build integration

package repo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// incidentsTestFixture wires an IncidentsRepo against both pools and seeds
// a throw-away org. Insert paths run through the request-scoped app pool;
// the admin-pool read paths (PollFatalSince, UpdateProjectID, CountRecent)
// don't need a tx in ctx because the production code paths run from the
// Sentinel goroutine with no principal.
type incidentsTestFixture struct {
	t         *testing.T
	repo      *IncidentsRepo
	adminPool *pgxpool.Pool
	appPool   *pgxpool.Pool
	tx        pgx.Tx
	orgID     string
	userID    string
}

func incidentsFixture(t *testing.T) (context.Context, *incidentsTestFixture) {
	t.Helper()
	adminURL := os.Getenv("DATABASE_URL_TEST")
	if adminURL == "" {
		t.Skip("DATABASE_URL_TEST unset — skipping incidents repo test")
	}
	appURL := os.Getenv("DATABASE_URL_TEST_APP")
	if appURL == "" {
		appURL = swapCredsToNexisApp(t, adminURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	t.Cleanup(func() { adminPool.Close() })

	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatalf("connect app: %v", err)
	}
	t.Cleanup(func() { appPool.Close() })

	stamp := time.Now().UTC().Format("20060102150405.000000")
	var orgID, userID string
	if err := adminPool.QueryRow(ctx,
		`INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id::text`,
		"inc-test-"+stamp, "inc-test-"+stamp,
	).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := adminPool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, '$2a$10$abcdefghijklmnopqrstuv') RETURNING id::text`,
		"inc-"+stamp+"@nexis.test",
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := adminPool.Exec(ctx,
		`UPDATE organizations SET owner_user_id = $1 WHERE id = $2`, userID, orgID); err != nil {
		t.Fatalf("set owner: %v", err)
	}

	repo := NewIncidentsRepoWithAdmin(appPool, adminPool)

	tx, err := appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", orgID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("pin org: %v", err)
	}

	fix := &incidentsTestFixture{
		t: t, repo: repo, adminPool: adminPool, appPool: appPool,
		tx: tx, orgID: orgID, userID: userID,
	}
	t.Cleanup(fix.cleanup)
	return db.WithTx(ctx, tx), fix
}

func (f *incidentsTestFixture) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if f.tx != nil {
		_ = f.tx.Commit(ctx)
	}
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM incidents_raw WHERE org_id = $1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `UPDATE organizations SET owner_user_id = NULL WHERE id = $1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM users WHERE id = $1`, f.userID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, f.orgID)
}

func (f *incidentsTestFixture) commitAndReopen(ctx context.Context) context.Context {
	f.t.Helper()
	if f.tx != nil {
		if err := f.tx.Commit(ctx); err != nil {
			f.t.Fatalf("commit: %v", err)
		}
	}
	tx, err := f.appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		f.t.Fatalf("re-begin: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", f.orgID); err != nil {
		_ = tx.Rollback(ctx)
		f.t.Fatalf("re-pin: %v", err)
	}
	f.tx = tx
	return db.WithTx(ctx, tx)
}

// TestIncidentsRepo_Insert_HappyPath persists a row with full fingerprint
// fields and asserts they land under raw_payload._fingerprint.
func TestIncidentsRepo_Insert_HappyPath(t *testing.T) {
	ctx, fix := incidentsFixture(t)

	raw := domain.RawIncident{
		Source:                 "sentry",
		SourceEventID:          "evt-1",
		Title:                  "OOM",
		Level:                  "fatal",
		Service:                "billing-api",
		Environment:            "prod",
		Payload:                map[string]any{"stacktrace": "panic", "logs": "log-line"},
		SentryOrganizationSlug: "acme-sentry",
		SentryProjectSlug:      "billing",
		GitHubRepo:             "acme/billing",
	}
	if err := fix.repo.Insert(ctx, fix.orgID, raw); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// Re-open the tx as committed so admin-pool reads see the row.
	ctx = fix.commitAndReopen(ctx)

	var src, sentryOrg, sentryProj, ghRepo string
	if err := fix.adminPool.QueryRow(ctx, `
        SELECT source,
               COALESCE(raw_payload->'_fingerprint'->>'sentry_organization_slug', ''),
               COALESCE(raw_payload->'_fingerprint'->>'sentry_project_slug', ''),
               COALESCE(raw_payload->'_fingerprint'->>'github_repo', '')
        FROM incidents_raw WHERE org_id=$1`, fix.orgID,
	).Scan(&src, &sentryOrg, &sentryProj, &ghRepo); err != nil {
		t.Fatalf("verify insert: %v", err)
	}
	if src != "sentry" || sentryOrg != "acme-sentry" || sentryProj != "billing" || ghRepo != "acme/billing" {
		t.Fatalf("fingerprint not preserved: src=%q org=%q proj=%q gh=%q", src, sentryOrg, sentryProj, ghRepo)
	}
}

// TestIncidentsRepo_Insert_Idempotent: ON CONFLICT (org, source, source_event_id)
// DO NOTHING — re-inserting the same row is a no-op.
func TestIncidentsRepo_Insert_Idempotent(t *testing.T) {
	ctx, fix := incidentsFixture(t)

	raw := domain.RawIncident{
		Source:        "sentry",
		SourceEventID: "dup-1",
		Title:         "first",
		Level:         "fatal",
	}
	if err := fix.repo.Insert(ctx, fix.orgID, raw); err != nil {
		t.Fatalf("Insert#1: %v", err)
	}
	// Same SourceEventID, different title — must be a no-op (no constraint violation).
	raw.Title = "second"
	if err := fix.repo.Insert(ctx, fix.orgID, raw); err != nil {
		t.Fatalf("Insert#2 should be a no-op: %v", err)
	}

	ctx = fix.commitAndReopen(ctx)
	var n int
	if err := fix.adminPool.QueryRow(ctx,
		`SELECT count(*) FROM incidents_raw WHERE org_id=$1 AND source='sentry' AND source_event_id='dup-1'`,
		fix.orgID,
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("dedup failed: got %d rows, want 1", n)
	}
	// Original title preserved (DO NOTHING — the first row wins).
	var title string
	if err := fix.adminPool.QueryRow(ctx,
		`SELECT title FROM incidents_raw WHERE org_id=$1 AND source_event_id='dup-1'`, fix.orgID,
	).Scan(&title); err != nil {
		t.Fatalf("title: %v", err)
	}
	if title != "first" {
		t.Fatalf("DO NOTHING did not preserve original: got %q", title)
	}
}

// TestIncidentsRepo_PollFatalSince filters by org + level=fatal + source IN.
func TestIncidentsRepo_PollFatalSince(t *testing.T) {
	ctx, fix := incidentsFixture(t)

	// Seed: 1 fatal sentry, 1 warning sentry, 1 fatal datadog, 1 fatal github (should be skipped).
	for _, raw := range []domain.RawIncident{
		{Source: "sentry", SourceEventID: "f1", Title: "ouch", Level: "fatal"},
		{Source: "sentry", SourceEventID: "w1", Title: "warn", Level: "warning"},
		{Source: "datadog", SourceEventID: "f2", Title: "p1", Level: "fatal"},
		{Source: "github", SourceEventID: "f3", Title: "gh", Level: "fatal"},
	} {
		if err := fix.repo.Insert(ctx, fix.orgID, raw); err != nil {
			t.Fatalf("Insert(%q): %v", raw.SourceEventID, err)
		}
	}
	ctx = fix.commitAndReopen(ctx)

	rows, err := fix.repo.PollFatalSince(ctx, fix.orgID, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("PollFatalSince: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("PollFatalSince got %d, want 2 (sentry+datadog only)", len(rows))
	}
	sources := map[string]bool{}
	for _, r := range rows {
		sources[r.Source] = true
	}
	if !sources["sentry"] || !sources["datadog"] {
		t.Fatalf("PollFatalSince missing source: %+v", sources)
	}
	if sources["github"] {
		t.Fatalf("PollFatalSince picked up github — WHERE clause broken")
	}
}

// TestIncidentsRepo_PollFatalSince_RespectsSinceCutoff.
func TestIncidentsRepo_PollFatalSince_RespectsSinceCutoff(t *testing.T) {
	ctx, fix := incidentsFixture(t)

	// Seed 1 fatal row now.
	if err := fix.repo.Insert(ctx, fix.orgID, domain.RawIncident{
		Source: "sentry", SourceEventID: "now-1", Title: "now", Level: "fatal",
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	ctx = fix.commitAndReopen(ctx)

	// since=future → no rows.
	rows, err := fix.repo.PollFatalSince(ctx, fix.orgID, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("PollFatalSince future: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows for future since, got %d", len(rows))
	}
}

// TestIncidentsRepo_PollFatalSince_NilAdminReturnsErr is the misconfig guard.
func TestIncidentsRepo_PollFatalSince_NilAdminReturnsErr(t *testing.T) {
	// Build a repo with adminPool=nil via the legacy single-pool constructor
	// and intentionally nil the pool field too — both nil triggers ErrUnknown.
	r := &IncidentsRepo{pool: nil, adminPool: nil}
	_, err := r.PollFatalSince(context.Background(), "x", time.Now())
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, domain.ErrUnknown) {
		t.Fatalf("got %v, want wrap of domain.ErrUnknown", err)
	}
}

// TestIncidentsRepo_UpdateProjectID stamps the project_id on a row.
func TestIncidentsRepo_UpdateProjectID(t *testing.T) {
	ctx, fix := incidentsFixture(t)

	// Need a workspace + project to FK against.
	stamp := time.Now().UTC().Format("20060102150405.000000")
	var workspaceID, projectID string
	if err := fix.adminPool.QueryRow(ctx, `
		INSERT INTO workspaces (org_id, name, slug, region, status)
		VALUES ($1, $2, $3, 'us-east-1', 'ready') RETURNING id::text`,
		fix.orgID, "wsi-"+stamp, "wsi-"+stamp,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("workspace: %v", err)
	}
	t.Cleanup(func() {
		c, cc := context.WithTimeout(context.Background(), 5*time.Second)
		defer cc()
		_, _ = fix.adminPool.Exec(c, `DELETE FROM projects WHERE workspace_id=$1`, workspaceID)
		_, _ = fix.adminPool.Exec(c, `DELETE FROM workspaces WHERE id=$1`, workspaceID)
	})

	if err := fix.adminPool.QueryRow(ctx, `
		INSERT INTO projects (org_id, workspace_id, name, slug, environment, recovery_policy)
		VALUES ($1, $2, 'p', 'p-`+stamp+`', 'prod', '{}'::jsonb) RETURNING id::text`,
		fix.orgID, workspaceID,
	).Scan(&projectID); err != nil {
		t.Fatalf("project: %v", err)
	}

	if err := fix.repo.Insert(ctx, fix.orgID, domain.RawIncident{
		Source: "sentry", SourceEventID: "up-1", Title: "x", Level: "fatal",
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	ctx = fix.commitAndReopen(ctx)

	var incID string
	if err := fix.adminPool.QueryRow(ctx,
		`SELECT id::text FROM incidents_raw WHERE org_id=$1 AND source_event_id='up-1'`, fix.orgID,
	).Scan(&incID); err != nil {
		t.Fatalf("lookup incident id: %v", err)
	}

	if err := fix.repo.UpdateProjectID(ctx, incID, projectID); err != nil {
		t.Fatalf("UpdateProjectID: %v", err)
	}

	var gotProject *string
	if err := fix.adminPool.QueryRow(ctx,
		`SELECT project_id::text FROM incidents_raw WHERE id=$1::uuid`, incID,
	).Scan(&gotProject); err != nil {
		t.Fatalf("verify project_id: %v", err)
	}
	if gotProject == nil || *gotProject != projectID {
		t.Fatalf("project_id not stamped: got %v want %s", gotProject, projectID)
	}

	// Idempotency: re-stamping is a no-op.
	if err := fix.repo.UpdateProjectID(ctx, incID, projectID); err != nil {
		t.Fatalf("UpdateProjectID re-stamp: %v", err)
	}
}

// TestIncidentsRepo_UpdateProjectID_RejectsEmpty asserts misuse is caught.
func TestIncidentsRepo_UpdateProjectID_RejectsEmpty(t *testing.T) {
	_, fix := incidentsFixture(t)
	if err := fix.repo.UpdateProjectID(context.Background(), "", "proj"); err == nil {
		t.Fatalf("empty incident id should error")
	}
	if err := fix.repo.UpdateProjectID(context.Background(), "inc", ""); err == nil {
		t.Fatalf("empty project id should error")
	}
}

// TestIncidentsRepo_CountRecent counts rows within the lookback window.
func TestIncidentsRepo_CountRecent(t *testing.T) {
	ctx, fix := incidentsFixture(t)

	// 3 fresh rows.
	for i, evID := range []string{"r1", "r2", "r3"} {
		if err := fix.repo.Insert(ctx, fix.orgID, domain.RawIncident{
			Source: "sentry", SourceEventID: evID, Title: "t", Level: "warning",
		}); err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
	}
	ctx = fix.commitAndReopen(ctx)

	n, err := fix.repo.CountRecent(ctx, fix.orgID, 5*time.Minute)
	if err != nil {
		t.Fatalf("CountRecent: %v", err)
	}
	if n != 3 {
		t.Fatalf("CountRecent got %d want 3", n)
	}

	// Zero window collapses to secs=1 internally — should still see rows
	// inserted in the last second, but allow that some race may push them
	// outside that window. So we accept 0..3.
	n2, err := fix.repo.CountRecent(ctx, fix.orgID, 0)
	if err != nil {
		t.Fatalf("CountRecent 0: %v", err)
	}
	if n2 > 3 {
		t.Fatalf("CountRecent 0 leaked: got %d, want 0..3", n2)
	}
}

// TestIncidentsRepo_MaxReceivedAt returns the latest received_at or 1970.
func TestIncidentsRepo_MaxReceivedAt(t *testing.T) {
	ctx, fix := incidentsFixture(t)

	// Empty: returns 1970.
	ts, err := fix.repo.MaxReceivedAt(ctx, fix.orgID)
	if err != nil {
		t.Fatalf("MaxReceivedAt empty: %v", err)
	}
	if ts.Year() != 1970 {
		t.Fatalf("empty MaxReceivedAt got %v, want 1970", ts)
	}

	// Insert one row and re-check.
	if err := fix.repo.Insert(ctx, fix.orgID, domain.RawIncident{
		Source: "sentry", SourceEventID: "m1", Level: "warning",
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	ctx = fix.commitAndReopen(ctx)

	ts, err = fix.repo.MaxReceivedAt(ctx, fix.orgID)
	if err != nil {
		t.Fatalf("MaxReceivedAt after: %v", err)
	}
	if ts.Year() == 1970 {
		t.Fatalf("MaxReceivedAt should now be recent, got %v", ts)
	}
	if time.Since(ts) > 1*time.Minute {
		t.Fatalf("MaxReceivedAt too old: %v", ts)
	}
}

// TestIncidentsRepo_AdminNilGuards covers the misconfig branches that bypass
// admin queries when adminPool is nil.
func TestIncidentsRepo_AdminNilGuards(t *testing.T) {
	r := &IncidentsRepo{pool: nil, adminPool: nil}

	if _, err := r.CountRecent(context.Background(), "org", time.Minute); err == nil {
		t.Fatalf("CountRecent nil-admin should error")
	}
	if _, err := r.MaxReceivedAt(context.Background(), "org"); err == nil {
		t.Fatalf("MaxReceivedAt nil-admin should error")
	}
	if err := r.UpdateProjectID(context.Background(), "inc", "proj"); err == nil {
		t.Fatalf("UpdateProjectID nil-admin should error")
	}
}
