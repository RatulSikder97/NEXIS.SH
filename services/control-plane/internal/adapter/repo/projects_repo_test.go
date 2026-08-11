//go:build integration

package repo

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// TestProjectsRepo_CreateAndGet round-trips a row with every selector field
// populated. Verifies persistence + the readback projection.
func TestProjectsRepo_CreateAndGet(t *testing.T) {
	ctx, fix := projectsFixture(t)
	defer fix.Close()

	avail := 0.9995
	latency := 250
	errRate := 0.5
	in := domain.Project{
		OrgID:       fix.orgID,
		WorkspaceID: fix.workspaceID,
		Name:        "Orders API",
		Slug:        "orders-api-cag",
		Description: "primary order service",
		Environment: domain.EnvironmentProd,
		OwnerUserID: fix.userID,
		Selectors: domain.ProjectSelectors{
			GitHubRepo:                  "acme/orders-api",
			GitHubInstallationID:        12345,
			GitHubDefaultBranch:         "main",
			SentryOrganizationSlug:      "acme",
			SentryProjectSlug:           "orders-api-prod",
			ArgoCDServerURL:             "https://argocd.acme.com",
			ArgoCDAppName:               "orders-api-prod",
			ArgoCDProject:               "default",
			PagerDutyServiceID:          "P123ABC",
			PagerDutyEscalationPolicyID: "PE123XYZ",
			DatadogServiceTag:           "service:orders-api",
			DatadogEnvTag:               "env:prod",
			SlackChannelID:              "C01ABC",
		},
		Policy: domain.DefaultRecoveryPolicy(),
		SLO: domain.SLOTarget{
			AvailabilityTarget: &avail,
			LatencyP95Ms:       &latency,
			ErrorRatePct:       &errRate,
		},
	}

	created, err := fix.repo.Create(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected generated id, got empty")
	}
	if created.CreatedAt.IsZero() {
		t.Fatalf("expected created_at populated")
	}

	got, err := fix.repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Orders API" || got.Slug != "orders-api-cag" {
		t.Fatalf("get round-trip: %+v", got)
	}
	if got.Selectors.GitHubRepo != "acme/orders-api" ||
		got.Selectors.SentryOrganizationSlug != "acme" ||
		got.Selectors.PagerDutyServiceID != "P123ABC" ||
		got.Selectors.DatadogServiceTag != "service:orders-api" {
		t.Fatalf("selectors did not round-trip: %+v", got.Selectors)
	}
	if got.SLO.AvailabilityTarget == nil || *got.SLO.AvailabilityTarget != 0.9995 {
		t.Fatalf("availability target did not round-trip: %+v", got.SLO)
	}
	if got.Policy.MediumCountdownSeconds != 120 || !got.Policy.RollbackOnSLOBreach {
		t.Fatalf("policy did not round-trip: %+v", got.Policy)
	}
}

// TestProjectsRepo_List_FiltersArchived ensures archived rows disappear from
// List output but stay reachable via Get.
func TestProjectsRepo_List_FiltersArchived(t *testing.T) {
	ctx, fix := projectsFixture(t)
	defer fix.Close()

	live := mustCreateProject(t, ctx, fix, "live-laf", "live")
	archived := mustCreateProject(t, ctx, fix, "archived-laf", "archived")
	if err := fix.repo.Archive(ctx, archived.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	rows, err := fix.repo.List(ctx, fix.workspaceID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("list returned 0 rows, expected at least live")
	}
	for _, r := range rows {
		if r.ID == archived.ID {
			t.Fatalf("archived project leaked into List: %+v", r)
		}
	}
	// Live row must still be visible.
	var found bool
	for _, r := range rows {
		if r.ID == live.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("live project missing from List output")
	}
}

// TestProjectsRepo_Update_PartialFields exercises the dynamic SQL builder:
// changing name + recovery_policy in one call leaves slug + selectors intact.
func TestProjectsRepo_Update_PartialFields(t *testing.T) {
	ctx, fix := projectsFixture(t)
	defer fix.Close()

	p := mustCreateProject(t, ctx, fix, "to-update-upf", "first-name")

	newPolicy := domain.RecoveryPolicy{
		AutoMergeLowSeverity:    true,
		AutoMergeMediumSeverity: false,
		MediumCountdownSeconds:  300,
		KillSwitchEnabled:       true,
		ApproverUserIDs:         []string{fix.userID},
		MaxConcurrentRecoveries: 3,
		RollbackOnSLOBreach:     true,
	}
	updated, err := fix.repo.Update(ctx, p.ID, map[string]any{
		"name":            "second-name",
		"recovery_policy": newPolicy,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "second-name" {
		t.Fatalf("expected name=second-name, got %q", updated.Name)
	}
	if updated.Slug != p.Slug {
		t.Fatalf("slug should not change on update; was %q now %q", p.Slug, updated.Slug)
	}
	if updated.Policy.MediumCountdownSeconds != 300 || !updated.Policy.AutoMergeLowSeverity {
		t.Fatalf("policy did not round-trip after update: %+v", updated.Policy)
	}

	// Forbidden field rejected without touching the DB.
	if _, err := fix.repo.Update(ctx, p.ID, map[string]any{"slug": "evil-slug"}); err == nil {
		t.Fatalf("expected ErrFieldNotUpdatable for slug update")
	}
}

// TestProjectsRepo_Archive_HidesFromList — Archive + List interplay was already
// hit in TestProjectsRepo_List_FiltersArchived, but the spec lists this case
// explicitly. We assert idempotency on top: a second Archive is a no-op.
func TestProjectsRepo_Archive_HidesFromList(t *testing.T) {
	ctx, fix := projectsFixture(t)
	defer fix.Close()

	p := mustCreateProject(t, ctx, fix, "to-archive-ahfl", "to-archive")
	if err := fix.repo.Archive(ctx, p.ID); err != nil {
		t.Fatalf("first archive: %v", err)
	}
	if err := fix.repo.Archive(ctx, p.ID); err != nil {
		t.Fatalf("second archive (expected idempotent): %v", err)
	}

	rows, err := fix.repo.List(ctx, fix.workspaceID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if r.ID == p.ID {
			t.Fatalf("archived row visible in List: %+v", r)
		}
	}
}

// TestProjectsRepo_MatchByFingerprint_SentryShape covers the Sentry source.
func TestProjectsRepo_MatchByFingerprint_SentryShape(t *testing.T) {
	ctx, fix := projectsFixture(t)
	defer fix.Close()

	p := mustCreateProjectWithSelectors(t, ctx, fix, "sentry-match-mfs", "sentry-match", domain.ProjectSelectors{
		SentryOrganizationSlug: "sentry-org-x-mfs",
		SentryProjectSlug:      "service-x-mfs",
	})
	// MatchByFingerprint reads via the admin pool (cross-tenant Sentinel
	// sweep) so the project insert must be committed before the match runs.
	ctx = commitAndReopen(t, ctx, fix)

	gotID, ok, err := fix.repo.MatchByFingerprint(ctx, fix.orgID, domain.IncidentFingerprint{
		Source:                 "sentry",
		SentryOrganizationSlug: "sentry-org-x-mfs",
		SentryProjectSlug:      "service-x-mfs",
	})
	if err != nil || !ok || gotID != p.ID {
		t.Fatalf("sentry match: id=%s ok=%v err=%v want id=%s", gotID, ok, err, p.ID)
	}
}

// TestProjectsRepo_MatchByFingerprint_DatadogShape covers Datadog's
// service-tag matcher.
func TestProjectsRepo_MatchByFingerprint_DatadogShape(t *testing.T) {
	ctx, fix := projectsFixture(t)
	defer fix.Close()

	p := mustCreateProjectWithSelectors(t, ctx, fix, "dd-match-mfd", "dd-match", domain.ProjectSelectors{
		DatadogServiceTag: "service:billing-mfd",
	})
	ctx = commitAndReopen(t, ctx, fix)

	gotID, ok, err := fix.repo.MatchByFingerprint(ctx, fix.orgID, domain.IncidentFingerprint{
		Source:            "datadog",
		DatadogServiceTag: "service:billing-mfd",
	})
	if err != nil || !ok || gotID != p.ID {
		t.Fatalf("datadog match: id=%s ok=%v err=%v want id=%s", gotID, ok, err, p.ID)
	}
}

// TestProjectsRepo_MatchByFingerprint_PagerDutyShape covers PagerDuty.
func TestProjectsRepo_MatchByFingerprint_PagerDutyShape(t *testing.T) {
	ctx, fix := projectsFixture(t)
	defer fix.Close()

	p := mustCreateProjectWithSelectors(t, ctx, fix, "pd-match-mfp", "pd-match", domain.ProjectSelectors{
		PagerDutyServiceID: "PSVC123-MFP",
	})
	ctx = commitAndReopen(t, ctx, fix)

	gotID, ok, err := fix.repo.MatchByFingerprint(ctx, fix.orgID, domain.IncidentFingerprint{
		Source:             "pagerduty",
		PagerDutyServiceID: "PSVC123-MFP",
	})
	if err != nil || !ok || gotID != p.ID {
		t.Fatalf("pagerduty match: id=%s ok=%v err=%v want id=%s", gotID, ok, err, p.ID)
	}
}

// TestProjectsRepo_MatchByFingerprint_GitHubShape covers GitHub-repo.
func TestProjectsRepo_MatchByFingerprint_GitHubShape(t *testing.T) {
	ctx, fix := projectsFixture(t)
	defer fix.Close()

	p := mustCreateProjectWithSelectors(t, ctx, fix, "gh-match-mfg", "gh-match", domain.ProjectSelectors{
		GitHubRepo: "acme/api-mfg",
	})
	ctx = commitAndReopen(t, ctx, fix)

	gotID, ok, err := fix.repo.MatchByFingerprint(ctx, fix.orgID, domain.IncidentFingerprint{
		Source:     "github",
		GitHubRepo: "acme/api-mfg",
	})
	if err != nil || !ok || gotID != p.ID {
		t.Fatalf("github match: id=%s ok=%v err=%v want id=%s", gotID, ok, err, p.ID)
	}
}

// TestProjectsRepo_MatchByFingerprint_NoMatch_ReturnsFalse asserts non-match
// returns (false, nil) rather than ErrNotFound. Sentinel relies on the bool to
// fall back to the workspace default.
func TestProjectsRepo_MatchByFingerprint_NoMatch_ReturnsFalse(t *testing.T) {
	ctx, fix := projectsFixture(t)
	defer fix.Close()
	mustCreateProject(t, ctx, fix, "unrelated-nm", "unrelated")

	id, ok, err := fix.repo.MatchByFingerprint(ctx, fix.orgID, domain.IncidentFingerprint{
		Source:                 "sentry",
		SentryOrganizationSlug: "no-such-org",
		SentryProjectSlug:      "no-such-proj",
	})
	if err != nil {
		t.Fatalf("no-match: unexpected err %v", err)
	}
	if ok || id != "" {
		t.Fatalf("expected no-match, got id=%q ok=%v", id, ok)
	}
}

// TestProjectsRepo_UniqueSlugPerWorkspace verifies the (workspace_id, slug)
// unique constraint is plumbed through pgx so the usecase can detect collision.
func TestProjectsRepo_UniqueSlugPerWorkspace(t *testing.T) {
	ctx, fix := projectsFixture(t)
	defer fix.Close()

	first := mustCreateProject(t, ctx, fix, "shared-slug-usp", "first")
	if first.Slug != "shared-slug-usp" {
		t.Fatalf("first slug unexpected: %q", first.Slug)
	}

	dup := domain.Project{
		OrgID:       fix.orgID,
		WorkspaceID: fix.workspaceID,
		Name:        "second",
		Slug:        "shared-slug-usp",
		Environment: domain.EnvironmentProd,
		Policy:      domain.DefaultRecoveryPolicy(),
	}
	if _, err := fix.repo.Create(ctx, dup); err == nil {
		t.Fatalf("expected unique-violation on duplicate slug, got nil")
	} else if !IsUniqueViolation(err) {
		t.Fatalf("expected IsUniqueViolation, got %v", err)
	}
}

// TestProjectsRepo_CountByWorkspace returns the count of non-archived rows.
func TestProjectsRepo_CountByWorkspace(t *testing.T) {
	ctx, fix := projectsFixture(t)
	defer fix.Close()

	baseline, err := fix.repo.CountByWorkspace(ctx, fix.workspaceID)
	if err != nil {
		t.Fatalf("count baseline: %v", err)
	}
	mustCreateProject(t, ctx, fix, "count-a-cbw", "count-a")
	mustCreateProject(t, ctx, fix, "count-b-cbw", "count-b")
	archived := mustCreateProject(t, ctx, fix, "count-c-cbw", "count-c")
	if err := fix.repo.Archive(ctx, archived.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	got, err := fix.repo.CountByWorkspace(ctx, fix.workspaceID)
	if err != nil {
		t.Fatalf("count after: %v", err)
	}
	// We added 2 live + 1 archived; archived doesn't count.
	if got != baseline+2 {
		t.Fatalf("count after: got %d, want %d", got, baseline+2)
	}
}

// --- fixture machinery ------------------------------------------------------

// projectsTestFixture bundles the pools + identifier set the projects tests
// reuse. Close drops the rows we created so the DB ends each run clean.
//
// tx is the per-fixture RLS-bound transaction opened on the application pool
// with app.current_org_id pinned to orgID. It threads through ctx via
// db.WithTx so the repo's db.FromCtx returns this tx (instead of the bare
// admin pool fallback) and tenant queries see the RLS policy enforced.
type projectsTestFixture struct {
	t           *testing.T
	repo        *ProjectsRepo
	adminPool   *pgxpool.Pool
	appPool     *pgxpool.Pool
	tx          pgx.Tx
	orgID       string
	workspaceID string
	userID      string
}

// projectsFixture is the shared setup: spins up a fresh org + workspace + user
// directly via the admin pool, returns a ProjectsRepo wired against both
// pools, and registers a cleanup that drops the test rows.
//
// Tests skip when DATABASE_URL_TEST is unset — matching the rls_test.go +
// audit_list_test.go convention.
func projectsFixture(t *testing.T) (context.Context, *projectsTestFixture) {
	t.Helper()
	adminURL := os.Getenv("DATABASE_URL_TEST")
	if adminURL == "" {
		t.Skip("DATABASE_URL_TEST unset — skipping projects repo test")
	}
	appURL := os.Getenv("DATABASE_URL_TEST_APP")
	if appURL == "" {
		appURL = swapCredsToNexisAppForProjects(t, adminURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

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

	stamp := time.Now().UTC().Format("20060102150405.000000")
	email := "proj-repo-" + stamp + "@nexis.test"

	var orgID, userID, workspaceID string
	if err := adminPool.QueryRow(ctx, `
		INSERT INTO organizations (name, slug)
		VALUES ($1, $2)
		RETURNING id::text`,
		"projects-test-"+stamp, "projects-test-"+stamp,
	).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := adminPool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ($1, '$2a$10$abcdefghijklmnopqrstuv')
		RETURNING id::text`,
		email,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := adminPool.Exec(ctx,
		`UPDATE organizations SET owner_user_id = $1 WHERE id = $2`,
		userID, orgID); err != nil {
		t.Fatalf("set org owner: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`,
		orgID, userID); err != nil {
		t.Fatalf("insert org_member: %v", err)
	}
	if err := adminPool.QueryRow(ctx, `
		INSERT INTO workspaces (org_id, name, slug, region, status)
		VALUES ($1, $2, $3, 'us-east-1', 'ready')
		RETURNING id::text`,
		orgID, "ws-projects-"+stamp, "ws-projects-"+stamp,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	repo := NewProjectsRepoWithAdmin(appPool, adminPool)

	// Open the per-request RLS tx the production middleware would open.
	// nexis_app respects the policy on `projects`, so we must pin
	// app.current_org_id to orgID before any tenant query runs. We use
	// set_config(name, value, true) — the tx-local form — for the same
	// reason production does: SET LOCAL doesn't accept parameter
	// placeholders, set_config() does.
	tx, err := appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin app tx: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", orgID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("pin app.current_org_id: %v", err)
	}

	fix := &projectsTestFixture{
		t:           t,
		repo:        repo,
		adminPool:   adminPool,
		appPool:     appPool,
		tx:          tx,
		orgID:       orgID,
		workspaceID: workspaceID,
		userID:      userID,
	}

	// Commit-or-rollback the tx FIRST, then drop the test rows via the
	// admin pool. Tx must close before the admin DELETEs run so the row
	// locks the tx holds are released.
	t.Cleanup(func() { fix.cleanup() })

	return db.WithTx(ctx, tx), fix
}

// cleanup removes the rows the fixture created. Ordering matters: the test's
// RLS-bound tx must close FIRST (commits the inserts so the admin DELETEs
// further down can see them), then projects → workspace → org_member → owner
// FK → user → org via the admin pool which bypasses RLS.
func (f *projectsTestFixture) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if f.tx != nil {
		// Commit so the rows the test wrote land in the DB and the admin
		// DELETE below can see them. A rollback would lose the test data
		// before cleanup can scrub it, leaving orphans on the next run.
		_ = f.tx.Commit(ctx)
	}
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM projects WHERE workspace_id = $1`, f.workspaceID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM workspaces WHERE id = $1`, f.workspaceID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM org_members WHERE org_id = $1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `UPDATE organizations SET owner_user_id = NULL WHERE id = $1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM users WHERE id = $1`, f.userID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, f.orgID)
}

// Close is a small alias so tests can defer it for readability.
func (f *projectsTestFixture) Close() {}

// mustCreateProject is the shortcut for tests that don't care about the
// selector shape — only that a row exists with the given slug.
func mustCreateProject(t *testing.T, ctx context.Context, fix *projectsTestFixture, slug, name string) domain.Project {
	t.Helper()
	return mustCreateProjectWithSelectors(t, ctx, fix, slug, name, domain.ProjectSelectors{})
}

// commitAndReopen commits the fixture's current tx (flushing all writes so the
// admin pool can see them) and opens a fresh RLS-bound tx pinned to the same
// orgID. Returns the new ctx threading the fresh tx. Used by tests that need
// the admin pool to see project rows — MatchByFingerprint reads via the admin
// pool and won't see uncommitted writes.
func commitAndReopen(t *testing.T, ctx context.Context, fix *projectsTestFixture) context.Context {
	t.Helper()
	if fix.tx != nil {
		if err := fix.tx.Commit(ctx); err != nil {
			t.Fatalf("commit fixture tx: %v", err)
		}
	}
	tx, err := fix.appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("re-begin app tx: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", fix.orgID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("re-pin app.current_org_id: %v", err)
	}
	fix.tx = tx
	return db.WithTx(ctx, tx)
}

// mustCreateProjectWithSelectors inserts a project with the given selector
// payload. Errors fail the test.
func mustCreateProjectWithSelectors(t *testing.T, ctx context.Context, fix *projectsTestFixture, slug, name string, sel domain.ProjectSelectors) domain.Project {
	t.Helper()
	p, err := fix.repo.Create(ctx, domain.Project{
		OrgID:       fix.orgID,
		WorkspaceID: fix.workspaceID,
		Name:        name,
		Slug:        slug,
		Environment: domain.EnvironmentProd,
		Selectors:   sel,
		Policy:      domain.DefaultRecoveryPolicy(),
	})
	if err != nil {
		t.Fatalf("create project %q: %v", slug, err)
	}
	return p
}

// swapCredsToNexisAppForProjects mirrors the helper in rls_test.go. Duplicated
// because that helper lives under tests/integration and isn't reachable from
// internal/adapter/repo.
func swapCredsToNexisAppForProjects(t *testing.T, src string) string {
	t.Helper()
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
