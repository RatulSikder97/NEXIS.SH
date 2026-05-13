package usecase

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// TestProjects_Create_EnforcesCap verifies the per-workspace cap. With
// capByTier wired to 2, the third Create fails with ErrCapExceeded — the
// repo is not even consulted for the INSERT.
func TestProjects_Create_EnforcesCap(t *testing.T) {
	repo := newFakeProjectsRepo()
	svc := NewProjectsService(repo, &noopIntLookup{}, nil, func(string) int { return 2 })
	princ := testPrincipal()

	for i := 0; i < 2; i++ {
		if _, err := svc.Create(context.Background(), princ, baseCreateInput(fmt.Sprintf("App %d", i))); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if _, err := svc.Create(context.Background(), princ, baseCreateInput("App 3")); !errors.Is(err, ErrCapExceeded) {
		t.Fatalf("expected ErrCapExceeded on third Create, got %v", err)
	}
}

// TestProjects_Create_RequiresConnectedGitHub_WhenRepoSet asserts the
// GitHub-repo binding rule. The lookup is configured to return ErrNotFound
// for the org's GitHub integration, so the create call must fail with
// ErrIntegrationRequired without touching the repo.
func TestProjects_Create_RequiresConnectedGitHub_WhenRepoSet(t *testing.T) {
	repo := newFakeProjectsRepo()
	lookup := &fakeIntLookup{result: map[domain.IntegrationProvider]fakeIntResult{}}
	svc := NewProjectsService(repo, lookup, nil, nil)

	in := baseCreateInput("Orders")
	in.Selectors.GitHubRepo = "acme/orders-api"
	in.Selectors.GitHubInstallationID = 99
	princ := testPrincipal()

	if _, err := svc.Create(context.Background(), princ, in); !errors.Is(err, ErrIntegrationRequired) {
		t.Fatalf("expected ErrIntegrationRequired, got %v", err)
	}

	// Now wire the connection and assert the create proceeds.
	lookup.result[domain.IntegrationGitHub] = fakeIntResult{
		conn: domain.Connection{
			Provider:       domain.IntegrationGitHub,
			Status:         domain.StatusConnected,
			InstallationID: "99",
		},
	}
	if _, err := svc.Create(context.Background(), princ, in); err != nil {
		t.Fatalf("create with connected GitHub: %v", err)
	}

	// Mismatched installation_id still fails.
	in.Selectors.GitHubInstallationID = 999
	if _, err := svc.Create(context.Background(), princ, in); !errors.Is(err, ErrIntegrationRequired) {
		t.Fatalf("expected ErrIntegrationRequired for installation_id mismatch, got %v", err)
	}
}

// TestProjects_Create_SlugCollision_Appends — when the repo reports a unique
// violation on slug 'orders-api', the service retries with 'orders-api-2'.
func TestProjects_Create_SlugCollision_Appends(t *testing.T) {
	repo := newFakeProjectsRepo()
	// Reserve the bare slug so the first Create gets a unique violation.
	repo.reserveSlug("orders-api")

	svc := NewProjectsService(repo, &noopIntLookup{}, nil, nil)
	got, err := svc.Create(context.Background(), testPrincipal(), baseCreateInput("Orders API"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.Slug != "orders-api-2" {
		t.Fatalf("expected slug=orders-api-2 after collision, got %q", got.Slug)
	}
}

// TestProjects_Update_DoesNotChangeSlug — calling Update with no slug field
// (the API never lets one through anyway) leaves slug intact, even when the
// name changes.
func TestProjects_Update_DoesNotChangeSlug(t *testing.T) {
	repo := newFakeProjectsRepo()
	svc := NewProjectsService(repo, &noopIntLookup{}, nil, nil)
	princ := testPrincipal()
	created, err := svc.Create(context.Background(), princ, baseCreateInput("Orders API"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	newName := "Renamed"
	updated, err := svc.Update(context.Background(), princ, created.ID, UpdateProjectInput{
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Slug != created.Slug {
		t.Fatalf("slug changed across Update: was %q now %q", created.Slug, updated.Slug)
	}
	if updated.Name != "Renamed" {
		t.Fatalf("name did not update: %q", updated.Name)
	}
}

// TestProjects_Archive_AuditWritten — archive emits exactly one
// project.archived audit row with the expected metadata.
func TestProjects_Archive_AuditWritten(t *testing.T) {
	repo := newFakeProjectsRepo()
	aud := &fakeAudit{}
	svc := NewProjectsService(repo, &noopIntLookup{}, aud, nil)
	princ := testPrincipal()

	p, err := svc.Create(context.Background(), princ, baseCreateInput("To Archive"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	aud.reset()
	if err := svc.Archive(context.Background(), princ, p.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	rows := aud.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected 1 audit row, got %d (%+v)", len(rows), rows)
	}
	if rows[0].action != "project.archived" {
		t.Fatalf("expected action=project.archived, got %q", rows[0].action)
	}
	if rows[0].metadata["slug"] != p.Slug {
		t.Fatalf("audit metadata slug missing: %+v", rows[0].metadata)
	}
}

// TestProjects_UpdatePolicy_PersistsJSONB — the dedicated UpdatePolicy entry
// point writes the policy via the repo's recovery_policy field and emits the
// distinct project.policy_updated audit action.
func TestProjects_UpdatePolicy_PersistsJSONB(t *testing.T) {
	repo := newFakeProjectsRepo()
	aud := &fakeAudit{}
	svc := NewProjectsService(repo, &noopIntLookup{}, aud, nil)
	princ := testPrincipal()
	p, err := svc.Create(context.Background(), princ, baseCreateInput("Policy"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	aud.reset()

	want := domain.RecoveryPolicy{
		AutoMergeLowSeverity:    true,
		AutoMergeMediumSeverity: true,
		MediumCountdownSeconds:  60,
		KillSwitchEnabled:       false,
		ApproverUserIDs:         []string{"u1", "u2"},
		MaxConcurrentRecoveries: 4,
		RollbackOnSLOBreach:     true,
	}
	updated, err := svc.UpdatePolicy(context.Background(), princ, p.ID, want)
	if err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if updated.Policy.MediumCountdownSeconds != 60 || !updated.Policy.AutoMergeMediumSeverity ||
		updated.Policy.MaxConcurrentRecoveries != 4 || len(updated.Policy.ApproverUserIDs) != 2 {
		t.Fatalf("policy did not persist: %+v", updated.Policy)
	}
	rows := aud.snapshot()
	if len(rows) != 1 || rows[0].action != "project.policy_updated" {
		t.Fatalf("expected one project.policy_updated audit row, got %+v", rows)
	}
}

// TestProjects_DatadogTag_RequiresServicePrefix — Datadog service tags without
// the "service:" prefix fail validation BEFORE the repo is touched.
func TestProjects_DatadogTag_RequiresServicePrefix(t *testing.T) {
	repo := newFakeProjectsRepo()
	svc := NewProjectsService(repo, &noopIntLookup{}, nil, nil)
	in := baseCreateInput("Datadog")
	in.Selectors.DatadogServiceTag = "billing"

	_, err := svc.Create(context.Background(), testPrincipal(), in)
	if !errors.Is(err, ErrInvalidSelectors) {
		t.Fatalf("expected ErrInvalidSelectors for tag without service: prefix, got %v", err)
	}
	if repo.createCount() != 0 {
		t.Fatalf("repo Create was called despite validation failure")
	}
}

// --- fakes ------------------------------------------------------------------

func testPrincipal() domain.Principal {
	return domain.Principal{
		UserID: "00000000-0000-0000-0000-000000000001",
		OrgID:  "00000000-0000-0000-0000-000000000002",
		Role:   domain.RoleOwner,
	}
}

func baseCreateInput(name string) CreateProjectInput {
	return CreateProjectInput{
		WorkspaceID: "00000000-0000-0000-0000-000000000003",
		Name:        name,
		Environment: domain.EnvironmentProd,
		Selectors:   domain.ProjectSelectors{},
	}
}

// fakeProjectsRepo is a thread-safe in-memory ProjectsRepoPort. Enforces the
// (workspace_id, slug) uniqueness invariant so the slug-collision test can
// exercise the retry path through the same code path production hits.
type fakeProjectsRepo struct {
	mu       sync.Mutex
	rows     map[string]domain.Project
	reserved map[string]struct{} // workspace_id||slug pre-reserved (no project row)
	idSeq    int
	creates  int
}

func newFakeProjectsRepo() *fakeProjectsRepo {
	return &fakeProjectsRepo{
		rows:     map[string]domain.Project{},
		reserved: map[string]struct{}{},
	}
}

func (f *fakeProjectsRepo) reserveSlug(slug string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reserved["00000000-0000-0000-0000-000000000003||"+slug] = struct{}{}
}

func (f *fakeProjectsRepo) createCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creates
}

func (f *fakeProjectsRepo) Create(_ context.Context, p domain.Project) (domain.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++

	key := p.WorkspaceID + "||" + p.Slug
	if _, taken := f.reserved[key]; taken {
		return domain.Project{}, fakeUniqueViolation()
	}
	for _, existing := range f.rows {
		if existing.WorkspaceID == p.WorkspaceID && existing.Slug == p.Slug {
			return domain.Project{}, fakeUniqueViolation()
		}
	}

	f.idSeq++
	p.ID = fmt.Sprintf("00000000-0000-0000-0000-%012d", f.idSeq)
	f.rows[p.ID] = p
	return p, nil
}

func (f *fakeProjectsRepo) Get(_ context.Context, id string) (domain.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.rows[id]; ok {
		return p, nil
	}
	return domain.Project{}, domain.ErrNotFound
}

func (f *fakeProjectsRepo) List(_ context.Context, workspaceID string) ([]domain.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.Project{}
	for _, p := range f.rows {
		if p.WorkspaceID == workspaceID && p.ArchivedAt == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeProjectsRepo) Update(_ context.Context, id string, fields map[string]any) (domain.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.rows[id]
	if !ok {
		return domain.Project{}, domain.ErrNotFound
	}
	for k, v := range fields {
		switch k {
		case "name":
			if s, ok := v.(string); ok {
				p.Name = s
			}
		case "description":
			if s, ok := v.(string); ok {
				p.Description = s
			}
		case "environment":
			switch t := v.(type) {
			case string:
				p.Environment = domain.Environment(t)
			case domain.Environment:
				p.Environment = t
			}
		case "owner_user_id":
			if s, ok := v.(string); ok {
				p.OwnerUserID = s
			}
		case "github_repo":
			p.Selectors.GitHubRepo = anyString(v)
		case "github_installation_id":
			p.Selectors.GitHubInstallationID = anyInt64(v)
		case "github_default_branch":
			p.Selectors.GitHubDefaultBranch = anyString(v)
		case "sentry_organization_slug":
			p.Selectors.SentryOrganizationSlug = anyString(v)
		case "sentry_project_slug":
			p.Selectors.SentryProjectSlug = anyString(v)
		case "argocd_server_url":
			p.Selectors.ArgoCDServerURL = anyString(v)
		case "argocd_app_name":
			p.Selectors.ArgoCDAppName = anyString(v)
		case "argocd_project":
			p.Selectors.ArgoCDProject = anyString(v)
		case "pagerduty_service_id":
			p.Selectors.PagerDutyServiceID = anyString(v)
		case "pagerduty_escalation_policy_id":
			p.Selectors.PagerDutyEscalationPolicyID = anyString(v)
		case "datadog_service_tag":
			p.Selectors.DatadogServiceTag = anyString(v)
		case "datadog_env_tag":
			p.Selectors.DatadogEnvTag = anyString(v)
		case "slack_channel_id":
			p.Selectors.SlackChannelID = anyString(v)
		case "recovery_policy":
			switch t := v.(type) {
			case domain.RecoveryPolicy:
				p.Policy = t
			case *domain.RecoveryPolicy:
				if t != nil {
					p.Policy = *t
				}
			}
		case "slo_availability_target":
			p.SLO.AvailabilityTarget = anyFloatPtr(v)
		case "slo_latency_p95_ms":
			p.SLO.LatencyP95Ms = anyIntPtr(v)
		case "slo_error_rate_pct":
			p.SLO.ErrorRatePct = anyFloatPtr(v)
		}
	}
	f.rows[id] = p
	return p, nil
}

func (f *fakeProjectsRepo) Archive(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.rows[id]; ok {
		now := nowZeroish()
		p.ArchivedAt = &now
		f.rows[id] = p
	}
	return nil
}

func (f *fakeProjectsRepo) CountByWorkspace(_ context.Context, workspaceID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.rows {
		if p.WorkspaceID == workspaceID && p.ArchivedAt == nil {
			n++
		}
	}
	return n, nil
}

// nowZeroish returns a deterministic non-zero time pointer so archived_at
// reads non-nil without pulling time.Now into the test surface.
func nowZeroish() time.Time {
	return time.Unix(1_700_000_000, 0).UTC()
}

// fakeUniqueViolation manufactures a pgconn.PgError so repo.IsUniqueViolation
// classifies it correctly. The test mirrors production by routing through
// the same helper the workspace adapter uses.
func fakeUniqueViolation() error {
	return &pgconn.PgError{Code: "23505", Message: "duplicate key value"}
}

func anyString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func anyInt64(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int64:
		return t
	}
	return 0
}

func anyFloatPtr(v any) *float64 {
	switch t := v.(type) {
	case nil:
		return nil
	case *float64:
		return t
	case float64:
		return &t
	}
	return nil
}

func anyIntPtr(v any) *int {
	switch t := v.(type) {
	case nil:
		return nil
	case *int:
		return t
	case int:
		return &t
	}
	return nil
}

// fakeIntLookup returns a canned Connection per provider.
type fakeIntLookup struct {
	result map[domain.IntegrationProvider]fakeIntResult
}

type fakeIntResult struct {
	conn domain.Connection
	err  error
}

func (l *fakeIntLookup) Get(_ context.Context, _ string, p domain.IntegrationProvider) (domain.Connection, []byte, error) {
	r, ok := l.result[p]
	if !ok {
		return domain.Connection{}, nil, domain.ErrNotFound
	}
	return r.conn, nil, r.err
}

// noopIntLookup always reports the integration as connected — handy for tests
// that don't care about the GitHub binding path.
type noopIntLookup struct{}

func (noopIntLookup) Get(_ context.Context, _ string, _ domain.IntegrationProvider) (domain.Connection, []byte, error) {
	return domain.Connection{Status: domain.StatusConnected}, nil, nil
}

// fakeAudit captures every Write call so tests can assert on action +
// metadata. Thread-safe so a future parallel-Subtest pass doesn't race.
type fakeAudit struct {
	mu   sync.Mutex
	rows []fakeAuditRow
}

type fakeAuditRow struct {
	principal domain.Principal
	action    string
	target    string
	metadata  map[string]any
}

func (a *fakeAudit) Write(_ context.Context, p domain.Principal, action, target string, metadata map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rows = append(a.rows, fakeAuditRow{principal: p, action: action, target: target, metadata: metadata})
	return nil
}

func (a *fakeAudit) snapshot() []fakeAuditRow {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]fakeAuditRow, len(a.rows))
	copy(out, a.rows)
	return out
}

func (a *fakeAudit) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rows = nil
}
