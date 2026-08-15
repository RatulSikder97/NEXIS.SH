package usecase

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// ErrCapExceeded is returned by ProjectsService.Create when the workspace
// already holds the per-tier maximum number of active projects.
var ErrCapExceeded = errors.New("project cap exceeded")

// ErrIntegrationRequired is returned when a project asks to bind to a GitHub
// repository but the org has not connected a GitHub installation — or the
// installation id on the create payload does not match the connected one.
var ErrIntegrationRequired = errors.New("required integration not connected")

// ErrSlugAllocationFailed is returned when the auto-derived slug + every
// -2..-99 suffix collides with an existing row. Practically unreachable —
// the cap stops us at 25 projects per workspace today — but we surface a
// distinct error rather than crash on the 100th attempt.
var ErrSlugAllocationFailed = errors.New("slug allocation failed")

// ErrInvalidEnvironment is returned when the supplied environment is not one
// of dev|staging|prod.
var ErrInvalidEnvironment = errors.New("invalid environment")

// ErrInvalidSelectors is returned when the supplied selector payload fails
// shape validation (e.g. Sentry slug without org slug, Datadog tag without
// the service: prefix).
var ErrInvalidSelectors = errors.New("invalid selectors")

// DefaultProjectCap is the per-tier cap applied when capByTier is nil. Today
// every tier yields 25; the wiring is a function rather than a constant so a
// future plan tier can lift the ceiling without changing this signature.
const DefaultProjectCap = 25

// slugPattern is the regex auto-derived slugs must match. Lowercase letters,
// digits, and dashes; length 1..60. The same expression validates a manual
// override later if the API is extended.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// sentryProjectSlugPattern matches Sentry's slug rules: lowercase + digits +
// dashes, 1..200 chars. Loose enough to accept the formats Sentry actually
// emits without locking us out of edge cases we haven't seen.
var sentryProjectSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ProjectsRepoPort is the narrow surface ProjectsService consumes from the
// concrete *repo.ProjectsRepo. Defining it here lets the test layer substitute
// a fake without dragging in the real DB driver.
type ProjectsRepoPort interface {
	Create(ctx context.Context, p domain.Project) (domain.Project, error)
	Get(ctx context.Context, projectID string) (domain.Project, error)
	List(ctx context.Context, workspaceID string) ([]domain.Project, error)
	Update(ctx context.Context, projectID string, fields map[string]any) (domain.Project, error)
	Archive(ctx context.Context, projectID string) error
	CountByWorkspace(ctx context.Context, workspaceID string) (int, error)
}

// IntegrationsLookup is the narrow surface ProjectsService consumes from the
// concrete *repo.IntegrationsRepo — just enough to confirm a GitHub
// installation is connected and matches the project's chosen installation id.
type IntegrationsLookup interface {
	Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
}

// ProjectsService is the usecase wrapper. Validations + slug allocation +
// audit live here; the repo stays a thin SQL adapter.
type ProjectsService struct {
	repo      ProjectsRepoPort
	intRepo   IntegrationsLookup
	audit     domain.AuditWriter
	capByTier func(tier string) int
}

// NewProjectsService wires the dependencies. capByTier may be nil — the
// service falls back to DefaultProjectCap for every tier.
func NewProjectsService(repo ProjectsRepoPort, intRepo IntegrationsLookup, audit domain.AuditWriter, capByTier func(tier string) int) *ProjectsService {
	if capByTier == nil {
		capByTier = func(string) int { return DefaultProjectCap }
	}
	return &ProjectsService{repo: repo, intRepo: intRepo, audit: audit, capByTier: capByTier}
}

// CreateProjectInput is the public payload for ProjectsService.Create. Stored
// alongside the usecase so the HTTP handler doesn't need to import domain
// just to populate selectors.
type CreateProjectInput struct {
	WorkspaceID string
	Name        string
	Description string
	Environment domain.Environment
	OwnerUserID string
	Selectors   domain.ProjectSelectors
	Policy      *domain.RecoveryPolicy // nil → DefaultRecoveryPolicy()
	SLO         domain.SLOTarget
}

// UpdateProjectInput is the partial-update payload used by PATCH. Selectors
// (when non-nil) REPLACES the full selector record — there is no per-field
// merge today to keep the wire surface simple. Callers that want field-level
// merge can read the project first and mutate the local copy.
type UpdateProjectInput struct {
	Name        *string
	Description *string
	Environment *domain.Environment
	OwnerUserID *string
	Selectors   *domain.ProjectSelectors
	SLO         *domain.SLOTarget
}

// Create persists a new project. The flow is:
//
//  1. Validate environment + selectors.
//  2. Check the project cap.
//  3. If a GitHub repo is requested, confirm the org has a matching connected
//     GitHub integration.
//  4. Derive a unique slug from the name.
//  5. Insert the row.
//  6. Audit project.created.
//
// Cap + integration + selector errors propagate as sentinel errors so the
// HTTP handler can map them to specific status codes.
func (s *ProjectsService) Create(ctx context.Context, princ domain.Principal, in CreateProjectInput) (domain.Project, error) {
	if !in.Environment.IsValid() {
		return domain.Project{}, fmt.Errorf("%w: %s", ErrInvalidEnvironment, in.Environment)
	}
	if err := validateSelectors(in.Selectors); err != nil {
		return domain.Project{}, err
	}

	count, err := s.repo.CountByWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		return domain.Project{}, err
	}
	cap := s.capByTier("")
	if cap <= 0 {
		cap = DefaultProjectCap
	}
	if count >= cap {
		return domain.Project{}, ErrCapExceeded
	}

	if err := s.checkGitHubBinding(ctx, princ.OrgID, &in.Selectors); err != nil {
		return domain.Project{}, err
	}

	policy := domain.DefaultRecoveryPolicy()
	if in.Policy != nil {
		policy = *in.Policy
	}

	// Slug allocation: try the base; on collision, append -2..-99.
	base := slugify(in.Name)
	if base == "" {
		base = "project"
	}
	var created domain.Project
	for i := 0; i < 99; i++ {
		slug := base
		if i > 0 {
			slug = fmt.Sprintf("%s-%d", base, i+1)
		}
		row := domain.Project{
			OrgID:       princ.OrgID,
			WorkspaceID: in.WorkspaceID,
			Name:        in.Name,
			Slug:        slug,
			Description: in.Description,
			Environment: in.Environment,
			OwnerUserID: firstNonEmpty(in.OwnerUserID, princ.UserID),
			Selectors:   in.Selectors,
			Policy:      policy,
			SLO:         in.SLO,
		}
		got, err := s.repo.Create(ctx, row)
		if err == nil {
			created = got
			break
		}
		if !repo.IsUniqueViolation(err) {
			return domain.Project{}, err
		}
		// Fall through and try the next slug suffix.
	}
	if created.ID == "" {
		return domain.Project{}, ErrSlugAllocationFailed
	}

	s.auditMaybe(ctx, princ, "project.created", created.ID, map[string]any{
		"workspace_id": created.WorkspaceID,
		"slug":         created.Slug,
		"environment":  string(created.Environment),
	})
	return created, nil
}

// Update applies a partial update + audits the action. The slug is never
// updated — the column is on the forbidden list inside the repo, and the
// usecase deliberately ignores any client-supplied slug.
func (s *ProjectsService) Update(ctx context.Context, princ domain.Principal, projectID string, in UpdateProjectInput) (domain.Project, error) {
	// Verify ownership before mutating — Get under RLS returns ErrNotFound
	// when the project belongs to another tenant, so this doubles as a
	// tenancy gate without exposing cross-tenant probing.
	existing, err := s.repo.Get(ctx, projectID)
	if err != nil {
		return domain.Project{}, err
	}
	if existing.OrgID != princ.OrgID {
		return domain.Project{}, domain.ErrNotFound
	}

	fields := map[string]any{}
	if in.Name != nil {
		fields["name"] = *in.Name
	}
	if in.Description != nil {
		fields["description"] = *in.Description
	}
	if in.Environment != nil {
		if !in.Environment.IsValid() {
			return domain.Project{}, fmt.Errorf("%w: %s", ErrInvalidEnvironment, *in.Environment)
		}
		fields["environment"] = string(*in.Environment)
	}
	if in.OwnerUserID != nil {
		fields["owner_user_id"] = *in.OwnerUserID
	}
	if in.Selectors != nil {
		if err := validateSelectors(*in.Selectors); err != nil {
			return domain.Project{}, err
		}
		if err := s.checkGitHubBinding(ctx, princ.OrgID, in.Selectors); err != nil {
			return domain.Project{}, err
		}
		fields["github_repo"] = in.Selectors.GitHubRepo
		fields["github_installation_id"] = in.Selectors.GitHubInstallationID
		fields["github_default_branch"] = in.Selectors.GitHubDefaultBranch
		fields["sentry_organization_slug"] = in.Selectors.SentryOrganizationSlug
		fields["sentry_project_slug"] = in.Selectors.SentryProjectSlug
		fields["argocd_server_url"] = in.Selectors.ArgoCDServerURL
		fields["argocd_app_name"] = in.Selectors.ArgoCDAppName
		fields["argocd_project"] = in.Selectors.ArgoCDProject
		fields["pagerduty_service_id"] = in.Selectors.PagerDutyServiceID
		fields["pagerduty_escalation_policy_id"] = in.Selectors.PagerDutyEscalationPolicyID
		fields["datadog_service_tag"] = in.Selectors.DatadogServiceTag
		fields["datadog_env_tag"] = in.Selectors.DatadogEnvTag
		fields["slack_channel_id"] = in.Selectors.SlackChannelID
	}
	if in.SLO != nil {
		fields["slo_availability_target"] = in.SLO.AvailabilityTarget
		fields["slo_latency_p95_ms"] = in.SLO.LatencyP95Ms
		fields["slo_error_rate_pct"] = in.SLO.ErrorRatePct
	}

	if len(fields) == 0 {
		return existing, nil
	}
	updated, err := s.repo.Update(ctx, projectID, fields)
	if err != nil {
		return domain.Project{}, err
	}

	s.auditMaybe(ctx, princ, "project.updated", updated.ID, map[string]any{
		"workspace_id": updated.WorkspaceID,
		"slug":         updated.Slug,
	})
	return updated, nil
}

// Archive soft-deletes the row + audits project.archived. Idempotent — a
// second archive on the same id is a no-op at the SQL layer.
func (s *ProjectsService) Archive(ctx context.Context, princ domain.Principal, projectID string) error {
	existing, err := s.repo.Get(ctx, projectID)
	if err != nil {
		return err
	}
	if existing.OrgID != princ.OrgID {
		return domain.ErrNotFound
	}
	if err := s.repo.Archive(ctx, projectID); err != nil {
		return err
	}
	s.auditMaybe(ctx, princ, "project.archived", projectID, map[string]any{
		"workspace_id": existing.WorkspaceID,
		"slug":         existing.Slug,
	})
	return nil
}

// Get returns a single project, scoped to the principal's org. ErrNotFound for
// cross-tenant probes.
func (s *ProjectsService) Get(ctx context.Context, princ domain.Principal, projectID string) (domain.Project, error) {
	p, err := s.repo.Get(ctx, projectID)
	if err != nil {
		return domain.Project{}, err
	}
	if p.OrgID != princ.OrgID {
		return domain.Project{}, domain.ErrNotFound
	}
	return p, nil
}

// List returns every project in the workspace (non-archived). The repo's
// query is scoped to workspace_id; RLS plus the implicit org_id index pin it
// to the principal's tenancy.
func (s *ProjectsService) List(ctx context.Context, princ domain.Principal, workspaceID string) ([]domain.Project, error) {
	rows, err := s.repo.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	// Defence in depth — filter again in Go in case RLS is misconfigured.
	out := make([]domain.Project, 0, len(rows))
	for _, r := range rows {
		if r.OrgID == princ.OrgID {
			out = append(out, r)
		}
	}
	return out, nil
}

// UpdatePolicy is the dedicated PUT /recovery-policy entry point. It bypasses
// the generic Update so the audit emits the policy-specific action, and so a
// future ABAC rule can scope policy changes more tightly than name/desc.
func (s *ProjectsService) UpdatePolicy(ctx context.Context, princ domain.Principal, projectID string, policy domain.RecoveryPolicy) (domain.Project, error) {
	existing, err := s.repo.Get(ctx, projectID)
	if err != nil {
		return domain.Project{}, err
	}
	if existing.OrgID != princ.OrgID {
		return domain.Project{}, domain.ErrNotFound
	}
	updated, err := s.repo.Update(ctx, projectID, map[string]any{
		"recovery_policy": policy,
	})
	if err != nil {
		return domain.Project{}, err
	}
	s.auditMaybe(ctx, princ, "project.policy_updated", updated.ID, map[string]any{
		"workspace_id": updated.WorkspaceID,
		"slug":         updated.Slug,
	})
	return updated, nil
}

// auditMaybe routes to the audit writer when one is wired. Audit failures are
// not surfaced — the mutation already succeeded, and the writer logs the
// failure internally.
func (s *ProjectsService) auditMaybe(ctx context.Context, princ domain.Principal, action, target string, meta map[string]any) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Write(ctx, princ, action, target, meta)
}

// checkGitHubBinding confirms a project that references a GitHub repo has the
// org-level GitHub integration connected, and that the supplied
// installation_id matches the stored one. We don't call GitHub directly here
// — that work happened at Connect time and would be a 300ms tax on every
// project mutation otherwise.
func (s *ProjectsService) checkGitHubBinding(ctx context.Context, orgID string, sel *domain.ProjectSelectors) error {
	if sel.GitHubRepo == "" {
		return nil
	}
	if s.intRepo == nil {
		// Missing integrations lookup is treated as "fail closed" — a project
		// referencing a repo must verify the org has connected GitHub, and we
		// can't do that without the lookup port.
		return ErrIntegrationRequired
	}
	conn, _, err := s.intRepo.Get(ctx, orgID, domain.IntegrationGitHub)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return ErrIntegrationRequired
		}
		return err
	}
	if conn.Status != domain.StatusConnected {
		return ErrIntegrationRequired
	}
	// installation_id: when the caller didn't supply one, inherit the org's.
	//
	// Binding a repo through the console sends only github_repo, so projects
	// were stored with installation_id = 0 and every later use — Deploy now,
	// GitOps PR creation — failed with "project has no github installation
	// bound" even though the org had the App installed. Inheriting here means
	// a repo binding is complete the moment it is made.
	if sel.GitHubInstallationID == 0 {
		if conn.InstallationID != "" {
			if id, perr := strconv.ParseInt(conn.InstallationID, 10, 64); perr == nil && id > 0 {
				sel.GitHubInstallationID = id
			}
		}
		return nil
	}
	if conn.InstallationID == "" {
		return ErrIntegrationRequired
	}
	want := fmt.Sprintf("%d", sel.GitHubInstallationID)
	if conn.InstallationID != want {
		return ErrIntegrationRequired
	}
	return nil
}

// validateSelectors enforces the project-shape rules the repo doesn't catch:
//
//   - Sentry: project_slug requires org_slug + matches sentryProjectSlugPattern.
//   - Datadog: service tag must start with "service:".
//
// Other selector fields are free-form text that downstream adapters parse.
func validateSelectors(s domain.ProjectSelectors) error {
	if s.SentryProjectSlug != "" {
		if s.SentryOrganizationSlug == "" {
			return fmt.Errorf("%w: sentry_project_slug requires sentry_organization_slug", ErrInvalidSelectors)
		}
		if !sentryProjectSlugPattern.MatchString(s.SentryProjectSlug) {
			return fmt.Errorf("%w: sentry_project_slug %q must be alphanumeric+dashes", ErrInvalidSelectors, s.SentryProjectSlug)
		}
	}
	if s.DatadogServiceTag != "" && !strings.HasPrefix(s.DatadogServiceTag, "service:") {
		return fmt.Errorf("%w: datadog_service_tag must start with 'service:'", ErrInvalidSelectors)
	}
	return nil
}

// slugify reduces a free-form name to a URL-safe slug. Mirrors the workspace
// slugifier so the projects + workspaces wire shape stays consistent. Clamps
// to 60 chars per the spec.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, c := range s {
		switch {
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			b.WriteRune(c)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 60 {
		out = strings.TrimRight(out[:60], "-")
	}
	if out != "" && !slugPattern.MatchString(out) {
		// Defensive — slugify only emits a-z0-9- so the regex should always
		// match. Return empty so the caller substitutes "project".
		return ""
	}
	return out
}

// firstNonEmpty returns a if non-empty, else b. Tiny helper to keep call
// sites readable.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
