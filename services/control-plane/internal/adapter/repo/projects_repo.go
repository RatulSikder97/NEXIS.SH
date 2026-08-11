package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// ErrFieldNotUpdatable is returned by ProjectsRepo.Update when the caller asks
// to mutate a column that isn't on the allow-list (e.g. id, org_id, slug,
// created_at). Keeps the partial-update API safe from drift: adding a new
// updatable field requires explicitly extending updateAllowed.
var ErrFieldNotUpdatable = errors.New("field not updatable")

// projectsColumns is the canonical SELECT projection. Kept as a single string
// so every read site agrees on column order and the Scan target order can be
// inlined safely. NULLs from the DB are absorbed via COALESCE / pointer
// targets in scanProject below.
const projectsColumns = `
	id::text, org_id::text, workspace_id::text, name, slug,
	COALESCE(description,''), environment,
	COALESCE(owner_user_id::text,'') AS owner_user_id,
	COALESCE(github_repo,''),
	COALESCE(github_installation_id, 0)::bigint,
	COALESCE(github_default_branch,''),
	COALESCE(sentry_organization_slug,''),
	COALESCE(sentry_project_slug,''),
	COALESCE(argocd_server_url,''),
	COALESCE(argocd_app_name,''),
	COALESCE(argocd_project,''),
	COALESCE(pagerduty_service_id,''),
	COALESCE(pagerduty_escalation_policy_id,''),
	COALESCE(datadog_service_tag,''),
	COALESCE(datadog_env_tag,''),
	COALESCE(slack_channel_id,''),
	recovery_policy,
	slo_availability_target,
	slo_latency_p95_ms,
	slo_error_rate_pct,
	created_at, updated_at, archived_at`

// ProjectsRepo persists the projects aggregate. The dual-pool pattern matches
// IntegrationsRepo:
//
//   - pool (app role): used by tenant-scoped CRUD via db.FromCtx so the
//     per-request RLS tx pins app.current_org_id.
//   - adminPool (superuser): used by MatchByFingerprint, which Sentinel calls
//     from a goroutine outside any request lifecycle — that path is
//     cross-tenant by design and must NOT be filtered by RLS.
//
// adminPool may be nil in tests where the cross-tenant sweep isn't exercised;
// MatchByFingerprint returns domain.ErrUnknown when adminPool is unwired so a
// misconfigured boot fails loud.
type ProjectsRepo struct {
	pool      *pgxpool.Pool
	adminPool *pgxpool.Pool
}

// NewProjectsRepo builds a ProjectsRepo against the supplied app pool. Tests
// that don't exercise the Sentinel sweep can use this single-pool form.
func NewProjectsRepo(pool *pgxpool.Pool) *ProjectsRepo {
	return &ProjectsRepo{pool: pool}
}

// NewProjectsRepoWithAdmin wires both pools. Production paths construct via
// this form so Sentinel's MatchByFingerprint has the cross-tenant pool it
// needs.
func NewProjectsRepoWithAdmin(pool, adminPool *pgxpool.Pool) *ProjectsRepo {
	if adminPool == nil {
		adminPool = pool
	}
	return &ProjectsRepo{pool: pool, adminPool: adminPool}
}

// updateAllowed is the whitelist of columns the Update method permits. Any
// other key in the partial-update map returns ErrFieldNotUpdatable so callers
// can't sneak through forbidden columns (id, org_id, workspace_id, slug,
// created_at). Kept package-level so adding a new updatable field is a single
// edit, not a hunt-and-peck.
var updateAllowed = map[string]struct{}{
	"name":                           {},
	"description":                    {},
	"environment":                    {},
	"owner_user_id":                  {},
	"github_repo":                    {},
	"github_installation_id":         {},
	"github_default_branch":          {},
	"sentry_organization_slug":       {},
	"sentry_project_slug":            {},
	"argocd_server_url":              {},
	"argocd_app_name":                {},
	"argocd_project":                 {},
	"pagerduty_service_id":           {},
	"pagerduty_escalation_policy_id": {},
	"datadog_service_tag":            {},
	"datadog_env_tag":                {},
	"slack_channel_id":               {},
	"recovery_policy":                {},
	"slo_availability_target":        {},
	"slo_latency_p95_ms":             {},
	"slo_error_rate_pct":             {},
}

// Create inserts a new project row and returns the persisted aggregate with
// generated id + timestamps. Runs under the per-request RLS tx via
// db.FromCtx; the policy on `projects` enforces org_id == current GUC.
func (r *ProjectsRepo) Create(ctx context.Context, p domain.Project) (domain.Project, error) {
	q := db.FromCtx(ctx, r.pool)
	policyJSON, err := json.Marshal(p.Policy)
	if err != nil {
		return domain.Project{}, err
	}

	row := q.QueryRow(ctx, `
		INSERT INTO projects (
			org_id, workspace_id, name, slug, description, environment, owner_user_id,
			github_repo, github_installation_id, github_default_branch,
			sentry_organization_slug, sentry_project_slug,
			argocd_server_url, argocd_app_name, argocd_project,
			pagerduty_service_id, pagerduty_escalation_policy_id,
			datadog_service_tag, datadog_env_tag,
			slack_channel_id,
			recovery_policy,
			slo_availability_target, slo_latency_p95_ms, slo_error_rate_pct
		)
		VALUES (
			$1,$2,$3,$4,$5,$6, NULLIF($7,'')::uuid,
			NULLIF($8,''), NULLIF($9,0)::bigint, NULLIF($10,''),
			NULLIF($11,''), NULLIF($12,''),
			NULLIF($13,''), NULLIF($14,''), NULLIF($15,''),
			NULLIF($16,''), NULLIF($17,''),
			NULLIF($18,''), NULLIF($19,''),
			NULLIF($20,''),
			$21::jsonb,
			$22, $23, $24
		)
		RETURNING `+projectsColumns,
		p.OrgID, p.WorkspaceID, p.Name, p.Slug, p.Description, string(p.Environment), p.OwnerUserID,
		p.Selectors.GitHubRepo, p.Selectors.GitHubInstallationID, p.Selectors.GitHubDefaultBranch,
		p.Selectors.SentryOrganizationSlug, p.Selectors.SentryProjectSlug,
		p.Selectors.ArgoCDServerURL, p.Selectors.ArgoCDAppName, p.Selectors.ArgoCDProject,
		p.Selectors.PagerDutyServiceID, p.Selectors.PagerDutyEscalationPolicyID,
		p.Selectors.DatadogServiceTag, p.Selectors.DatadogEnvTag,
		p.Selectors.SlackChannelID,
		policyJSON,
		nullableFloat(p.SLO.AvailabilityTarget),
		nullableInt(p.SLO.LatencyP95Ms),
		nullableFloat(p.SLO.ErrorRatePct),
	)
	return scanProject(row)
}

// Get returns the row matching projectID under RLS. Cross-tenant ids return
// domain.ErrNotFound (the policy hides them) — callers always see the same
// 404-shaped error whether the row is truly missing or simply not theirs.
func (r *ProjectsRepo) Get(ctx context.Context, projectID string) (domain.Project, error) {
	q := db.FromCtx(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT `+projectsColumns+` FROM projects WHERE id = $1`, projectID)
	return scanProject(row)
}

// List returns every project in the workspace that isn't archived, ordered by
// name. The slice is never nil so JSON renders as `[]` not `null`.
func (r *ProjectsRepo) List(ctx context.Context, workspaceID string) ([]domain.Project, error) {
	q := db.FromCtx(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT `+projectsColumns+`
		FROM projects
		WHERE workspace_id = $1 AND archived_at IS NULL
		ORDER BY name ASC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.Project{}
	for rows.Next() {
		p, err := scanProjectRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Update applies the supplied partial-update map. Keys not on updateAllowed
// return ErrFieldNotUpdatable BEFORE any SQL runs. Values are accepted as `any`
// so the caller can mix string / bool / int / RecoveryPolicy struct / nil. nil
// is interpreted as "clear the column" (SQL NULL).
//
// The trigger trg_projects_updated_at bumps updated_at on the underlying row
// — no need to set it explicitly.
func (r *ProjectsRepo) Update(ctx context.Context, projectID string, fields map[string]any) (domain.Project, error) {
	if len(fields) == 0 {
		return r.Get(ctx, projectID)
	}

	// Sort keys for a deterministic SQL string — easier to read in logs and
	// makes failing tests reproducible.
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if _, ok := updateAllowed[k]; !ok {
			return domain.Project{}, fmt.Errorf("%w: %s", ErrFieldNotUpdatable, k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)+1)
	args = append(args, projectID)
	idx := 2
	for _, k := range keys {
		val := fields[k]
		switch k {
		case "recovery_policy":
			// Accept either a RecoveryPolicy struct or pre-marshalled bytes.
			b, err := marshalPolicyArg(val)
			if err != nil {
				return domain.Project{}, err
			}
			parts = append(parts, fmt.Sprintf("recovery_policy = $%d::jsonb", idx))
			args = append(args, b)
		case "environment":
			parts = append(parts, fmt.Sprintf("environment = $%d", idx))
			if env, ok := val.(domain.Environment); ok {
				args = append(args, string(env))
			} else {
				args = append(args, val)
			}
		case "owner_user_id":
			parts = append(parts, fmt.Sprintf("owner_user_id = NULLIF($%d,'')::uuid", idx))
			args = append(args, val)
		case "github_installation_id":
			parts = append(parts, fmt.Sprintf("github_installation_id = NULLIF($%d,0)::bigint", idx))
			args = append(args, val)
		case "slo_availability_target", "slo_error_rate_pct":
			// numeric — accept *float64 / float64 / nil.
			parts = append(parts, fmt.Sprintf("%s = $%d", k, idx))
			args = append(args, normalizeNumeric(val))
		case "slo_latency_p95_ms":
			parts = append(parts, fmt.Sprintf("%s = $%d", k, idx))
			args = append(args, normalizeNumeric(val))
		default:
			parts = append(parts, fmt.Sprintf("%s = $%d", k, idx))
			args = append(args, val)
		}
		idx++
	}

	q := db.FromCtx(ctx, r.pool)
	sql := `UPDATE projects SET ` + strings.Join(parts, ", ") + ` WHERE id = $1 RETURNING ` + projectsColumns
	row := q.QueryRow(ctx, sql, args...)
	return scanProject(row)
}

// Archive soft-deletes the project by setting archived_at = now(). Subsequent
// List calls hide the row but Get still returns it (useful for the audit log
// that may reference a long-archived project id).
func (r *ProjectsRepo) Archive(ctx context.Context, projectID string) error {
	q := db.FromCtx(ctx, r.pool)
	_, err := q.Exec(ctx,
		`UPDATE projects SET archived_at = now() WHERE id = $1 AND archived_at IS NULL`,
		projectID)
	return err
}

// CountByWorkspace returns the number of active (not-archived) projects in the
// workspace. Used by the project-cap check in the usecase layer.
func (r *ProjectsRepo) CountByWorkspace(ctx context.Context, workspaceID string) (int, error) {
	q := db.FromCtx(ctx, r.pool)
	var n int
	err := q.QueryRow(ctx,
		`SELECT COUNT(*) FROM projects WHERE workspace_id = $1 AND archived_at IS NULL`,
		workspaceID).Scan(&n)
	return n, err
}

// MatchByFingerprint resolves a RawIncident fingerprint to a project id. Used
// by Sentinel — the detector goroutine has no principal in ctx and must see
// every org's project rows, so this method ALWAYS uses the admin pool to
// bypass RLS.
//
// Match strategy per source (all filtered to org_id and archived_at IS NULL):
//
//   - sentry:        (sentry_organization_slug, sentry_project_slug)
//   - datadog:       datadog_service_tag
//   - pagerduty:     pagerduty_service_id
//   - github:        github_repo
//   - deploy_engine: github_repo — preview-deploy failures carry the repo
//     fingerprint so a broken build routes back to the owning project.
//
// Returns (id, true, nil) on a hit, ("", false, nil) on no match,
// ("", false, ErrUnknown) when adminPool is unwired.
func (r *ProjectsRepo) MatchByFingerprint(ctx context.Context, orgID string, fp domain.IncidentFingerprint) (string, bool, error) {
	if r.adminPool == nil {
		return "", false, domain.ErrUnknown
	}

	var (
		sql  string
		args []any
	)
	switch strings.ToLower(fp.Source) {
	case "sentry":
		if fp.SentryOrganizationSlug == "" || fp.SentryProjectSlug == "" {
			return "", false, nil
		}
		sql = `SELECT id::text FROM projects
		       WHERE org_id = $1 AND archived_at IS NULL
		         AND sentry_organization_slug = $2 AND sentry_project_slug = $3
		       LIMIT 1`
		args = []any{orgID, fp.SentryOrganizationSlug, fp.SentryProjectSlug}
	case "datadog":
		if fp.DatadogServiceTag == "" {
			return "", false, nil
		}
		sql = `SELECT id::text FROM projects
		       WHERE org_id = $1 AND archived_at IS NULL
		         AND datadog_service_tag = $2
		       LIMIT 1`
		args = []any{orgID, fp.DatadogServiceTag}
	case "pagerduty":
		if fp.PagerDutyServiceID == "" {
			return "", false, nil
		}
		sql = `SELECT id::text FROM projects
		       WHERE org_id = $1 AND archived_at IS NULL
		         AND pagerduty_service_id = $2
		       LIMIT 1`
		args = []any{orgID, fp.PagerDutyServiceID}
	case "github", "deploy_engine":
		if fp.GitHubRepo == "" {
			return "", false, nil
		}
		sql = `SELECT id::text FROM projects
		       WHERE org_id = $1 AND archived_at IS NULL
		         AND github_repo = $2
		       LIMIT 1`
		args = []any{orgID, fp.GitHubRepo}
	default:
		return "", false, nil
	}

	var id string
	err := r.adminPool.QueryRow(ctx, sql, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// scanProject reads a pgx.Row into a domain.Project. Used by Create/Get/Update
// which all share the projectsColumns projection.
func scanProject(row pgx.Row) (domain.Project, error) {
	var (
		p            domain.Project
		envStr       string
		policyBytes  []byte
		availability *float64
		latencyMs    *int
		errorRate    *float64
		installID    int64
	)
	err := row.Scan(
		&p.ID, &p.OrgID, &p.WorkspaceID, &p.Name, &p.Slug,
		&p.Description, &envStr, &p.OwnerUserID,
		&p.Selectors.GitHubRepo, &installID, &p.Selectors.GitHubDefaultBranch,
		&p.Selectors.SentryOrganizationSlug, &p.Selectors.SentryProjectSlug,
		&p.Selectors.ArgoCDServerURL, &p.Selectors.ArgoCDAppName, &p.Selectors.ArgoCDProject,
		&p.Selectors.PagerDutyServiceID, &p.Selectors.PagerDutyEscalationPolicyID,
		&p.Selectors.DatadogServiceTag, &p.Selectors.DatadogEnvTag,
		&p.Selectors.SlackChannelID,
		&policyBytes,
		&availability, &latencyMs, &errorRate,
		&p.CreatedAt, &p.UpdatedAt, &p.ArchivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Project{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Project{}, err
	}
	p.Environment = domain.Environment(envStr)
	p.Selectors.GitHubInstallationID = installID
	if len(policyBytes) > 0 {
		if err := json.Unmarshal(policyBytes, &p.Policy); err != nil {
			return domain.Project{}, err
		}
	}
	p.SLO.AvailabilityTarget = availability
	p.SLO.LatencyP95Ms = latencyMs
	p.SLO.ErrorRatePct = errorRate
	return p, nil
}

// scanProjectRow is the pgx.Rows counterpart used by List. Identical to
// scanProject minus the ErrNoRows handling (Rows.Next() controls iteration).
func scanProjectRow(rows pgx.Rows) (domain.Project, error) {
	var (
		p            domain.Project
		envStr       string
		policyBytes  []byte
		availability *float64
		latencyMs    *int
		errorRate    *float64
		installID    int64
	)
	err := rows.Scan(
		&p.ID, &p.OrgID, &p.WorkspaceID, &p.Name, &p.Slug,
		&p.Description, &envStr, &p.OwnerUserID,
		&p.Selectors.GitHubRepo, &installID, &p.Selectors.GitHubDefaultBranch,
		&p.Selectors.SentryOrganizationSlug, &p.Selectors.SentryProjectSlug,
		&p.Selectors.ArgoCDServerURL, &p.Selectors.ArgoCDAppName, &p.Selectors.ArgoCDProject,
		&p.Selectors.PagerDutyServiceID, &p.Selectors.PagerDutyEscalationPolicyID,
		&p.Selectors.DatadogServiceTag, &p.Selectors.DatadogEnvTag,
		&p.Selectors.SlackChannelID,
		&policyBytes,
		&availability, &latencyMs, &errorRate,
		&p.CreatedAt, &p.UpdatedAt, &p.ArchivedAt,
	)
	if err != nil {
		return domain.Project{}, err
	}
	p.Environment = domain.Environment(envStr)
	p.Selectors.GitHubInstallationID = installID
	if len(policyBytes) > 0 {
		if err := json.Unmarshal(policyBytes, &p.Policy); err != nil {
			return domain.Project{}, err
		}
	}
	p.SLO.AvailabilityTarget = availability
	p.SLO.LatencyP95Ms = latencyMs
	p.SLO.ErrorRatePct = errorRate
	return p, nil
}

// nullableFloat converts a *float64 into the form pgx is happy to write as
// either a number or SQL NULL.
func nullableFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// nullableInt is the *int counterpart of nullableFloat.
func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

// normalizeNumeric coerces the caller-supplied value for a nullable numeric
// column. *float64 / float64 / *int / int / nil all flow through; anything
// else passes verbatim and lets pgx fail loudly if the value's untyped.
func normalizeNumeric(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case *float64:
		if t == nil {
			return nil
		}
		return *t
	case *int:
		if t == nil {
			return nil
		}
		return *t
	default:
		return v
	}
}

// marshalPolicyArg turns either a domain.RecoveryPolicy or []byte into the
// jsonb bytes the INSERT/UPDATE needs.
func marshalPolicyArg(v any) ([]byte, error) {
	switch t := v.(type) {
	case domain.RecoveryPolicy:
		return json.Marshal(t)
	case *domain.RecoveryPolicy:
		return json.Marshal(*t)
	case []byte:
		return t, nil
	case string:
		return []byte(t), nil
	default:
		return json.Marshal(v)
	}
}
