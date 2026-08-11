package repo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// Deployment statuses — mirror the CHECK constraint on deployments.status
// (migration 0033). "building" is reserved for a future async dispatch path;
// today the deploy handler blocks on the engine call and lands directly on
// running/failed.
const (
	DeploymentStatusBuilding = "building"
	DeploymentStatusRunning  = "running"
	DeploymentStatusFailed   = "failed"
	DeploymentStatusStopped  = "stopped"
)

// Deployment is one preview-deploy round-trip against the services/
// deploy-engine sidecar, persisted in the deployments table. control-plane
// mints ID before calling the engine (it names the container + image), so
// Create expects the caller to supply it.
type Deployment struct {
	ID               string
	ProjectID        string
	OrgID            string
	Status           string // building | running | failed | stopped
	URL              string
	Port             int
	ImageTag         string
	DockerfileSource string // "repo" | "generated"
	DetectedStack    string // node | python | go | static | unknown
	BuildLog         string
	ContainerLog     string
	Error            string
	StartedAt        *time.Time
	FinishedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ErrDeploymentFieldNotUpdatable is returned by DeploymentsRepo.Update when
// the caller asks to mutate a column that isn't on the allow-list (id,
// project_id, org_id, created_at). Same drift-guard idea as the projects
// repo's ErrFieldNotUpdatable.
var ErrDeploymentFieldNotUpdatable = errors.New("deployment field not updatable")

// deploymentsColumns is the canonical SELECT projection — one string so every
// read site agrees on column order. Nullable text/int columns are absorbed
// via COALESCE; the timestamps scan into pointers.
const deploymentsColumns = `
	id::text, project_id::text, org_id::text, status,
	COALESCE(url,''), COALESCE(port,0),
	COALESCE(image_tag,''), COALESCE(dockerfile_source,''), COALESCE(detected_stack,''),
	build_log, container_log, error,
	started_at, finished_at, created_at, updated_at`

// deploymentUpdateAllowed is the whitelist of columns Update permits —
// everything the engine round-trip (or a stop) can legitimately change.
var deploymentUpdateAllowed = map[string]struct{}{
	"status":            {},
	"url":               {},
	"port":              {},
	"image_tag":         {},
	"dockerfile_source": {},
	"detected_stack":    {},
	"build_log":         {},
	"container_log":     {},
	"error":             {},
	"started_at":        {},
	"finished_at":       {},
}

// DeploymentsRepo persists the deployments table. Dual-pool, matching
// ProjectsRepo:
//
//   - pool (app role): tenant-scoped CRUD via db.FromCtx so the per-request
//     RLS tx pins app.current_org_id.
//   - adminPool (superuser): used by CreateAdmin, which the recovery
//     workflow's DeployEngineRedeploy activity calls from a Temporal worker
//     goroutine with no principal in ctx — that write must bypass RLS.
//
// adminPool may be nil in tests that don't exercise the workflow path;
// CreateAdmin returns domain.ErrUnknown then so a misconfigured boot fails
// loud instead of silently dropping rows.
type DeploymentsRepo struct {
	pool      *pgxpool.Pool
	adminPool *pgxpool.Pool
}

// NewDeploymentsRepo builds a DeploymentsRepo against the supplied app pool.
// Tests that don't exercise the workflow redeploy path can use this
// single-pool form.
func NewDeploymentsRepo(pool *pgxpool.Pool) *DeploymentsRepo {
	return &DeploymentsRepo{pool: pool}
}

// NewDeploymentsRepoWithAdmin wires both pools. Production paths construct
// via this form so the DeployEngineRedeploy activity has the RLS-bypass pool
// it needs.
func NewDeploymentsRepoWithAdmin(pool, adminPool *pgxpool.Pool) *DeploymentsRepo {
	if adminPool == nil {
		adminPool = pool
	}
	return &DeploymentsRepo{pool: pool, adminPool: adminPool}
}

// Create inserts a deployment row and returns the persisted aggregate with
// server timestamps. Runs under the per-request RLS tx via db.FromCtx; the
// tenant_isolation policy enforces org_id == current GUC. d.ID must be the
// control-plane-minted deployment id.
func (r *DeploymentsRepo) Create(ctx context.Context, d Deployment) (Deployment, error) {
	q := db.FromCtx(ctx, r.pool)
	row := q.QueryRow(ctx, `
		INSERT INTO deployments (
			id, project_id, org_id, status,
			url, port, image_tag, dockerfile_source, detected_stack,
			build_log, container_log, error,
			started_at, finished_at
		)
		VALUES (
			$1, $2, $3, $4,
			NULLIF($5,''), NULLIF($6,0), NULLIF($7,''), NULLIF($8,''), NULLIF($9,''),
			$10, $11, $12,
			$13, $14
		)
		RETURNING `+deploymentsColumns,
		d.ID, d.ProjectID, d.OrgID, d.Status,
		d.URL, d.Port, d.ImageTag, d.DockerfileSource, d.DetectedStack,
		d.BuildLog, d.ContainerLog, d.Error,
		d.StartedAt, d.FinishedAt,
	)
	out, err := scanDeployment(row)
	if err != nil {
		return Deployment{}, fmt.Errorf("DeploymentsRepo.Create: %w", err)
	}
	return out, nil
}

// CreateAdmin is the system-side twin of Create: same insert, but on the
// admin pool because the caller (the recovery workflow's redeploy activity)
// runs with no principal in ctx — the RLS-aware app pool would reject the
// row for lack of a bound org GUC. Mirrors IncidentsRepo.InsertAdmin.
func (r *DeploymentsRepo) CreateAdmin(ctx context.Context, d Deployment) (Deployment, error) {
	if r.adminPool == nil {
		return Deployment{}, fmt.Errorf("DeploymentsRepo.CreateAdmin: %w", domain.ErrUnknown)
	}
	adminView := &DeploymentsRepo{pool: r.adminPool, adminPool: r.adminPool}
	return adminView.Create(ctx, d)
}

// Get returns the row matching deploymentID under RLS. Cross-tenant ids
// return domain.ErrNotFound (the policy hides them) — same 404-shaped error
// whether the row is truly missing or simply not the caller's.
func (r *DeploymentsRepo) Get(ctx context.Context, deploymentID string) (Deployment, error) {
	q := db.FromCtx(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT `+deploymentsColumns+` FROM deployments WHERE id = $1`, deploymentID)
	return scanDeployment(row)
}

// GetLatestForProject returns the most recent deployment for the project, or
// domain.ErrNotFound when the project has never deployed. Powers the "current
// preview" tile on the project ops page.
func (r *DeploymentsRepo) GetLatestForProject(ctx context.Context, projectID string) (Deployment, error) {
	q := db.FromCtx(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT `+deploymentsColumns+`
		FROM deployments
		WHERE project_id = $1
		ORDER BY created_at DESC
		LIMIT 1`, projectID)
	return scanDeployment(row)
}

// List returns the project's deployments newest-first, capped at limit
// (default 20, max 100). The slice is never nil so JSON renders as `[]`.
func (r *DeploymentsRepo) List(ctx context.Context, projectID string, limit int) ([]Deployment, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	q := db.FromCtx(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT `+deploymentsColumns+`
		FROM deployments
		WHERE project_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("DeploymentsRepo.List: %w", err)
	}
	defer rows.Close()

	out := []Deployment{}
	for rows.Next() {
		d, err := scanDeploymentRow(rows)
		if err != nil {
			return nil, fmt.Errorf("DeploymentsRepo.List: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Update applies the supplied partial-update map. Keys not on
// deploymentUpdateAllowed return ErrDeploymentFieldNotUpdatable BEFORE any
// SQL runs. nil clears the column; url/port additionally treat ”/0 as NULL
// so callers can pass the zero value without special-casing.
//
// The trigger trg_deployments_updated_at bumps updated_at on the row — no
// need to set it explicitly.
func (r *DeploymentsRepo) Update(ctx context.Context, deploymentID string, fields map[string]any) (Deployment, error) {
	if len(fields) == 0 {
		return r.Get(ctx, deploymentID)
	}

	// Sort keys for a deterministic SQL string — matches ProjectsRepo.Update.
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if _, ok := deploymentUpdateAllowed[k]; !ok {
			return Deployment{}, fmt.Errorf("%w: %s", ErrDeploymentFieldNotUpdatable, k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)+1)
	args = append(args, deploymentID)
	idx := 2
	for _, k := range keys {
		switch k {
		case "url", "image_tag", "dockerfile_source", "detected_stack":
			parts = append(parts, fmt.Sprintf("%s = NULLIF($%d,'')", k, idx))
		case "port":
			parts = append(parts, fmt.Sprintf("port = NULLIF($%d,0)", idx))
		default:
			parts = append(parts, fmt.Sprintf("%s = $%d", k, idx))
		}
		args = append(args, fields[k])
		idx++
	}

	q := db.FromCtx(ctx, r.pool)
	sql := `UPDATE deployments SET ` + strings.Join(parts, ", ") + ` WHERE id = $1 RETURNING ` + deploymentsColumns
	row := q.QueryRow(ctx, sql, args...)
	return scanDeployment(row)
}

// scanDeployment reads a pgx.Row into a Deployment. Shared by Create / Get /
// GetLatestForProject / Update, which all use the deploymentsColumns
// projection.
func scanDeployment(row pgx.Row) (Deployment, error) {
	var d Deployment
	err := row.Scan(
		&d.ID, &d.ProjectID, &d.OrgID, &d.Status,
		&d.URL, &d.Port,
		&d.ImageTag, &d.DockerfileSource, &d.DetectedStack,
		&d.BuildLog, &d.ContainerLog, &d.Error,
		&d.StartedAt, &d.FinishedAt, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Deployment{}, domain.ErrNotFound
	}
	if err != nil {
		return Deployment{}, err
	}
	return d, nil
}

// scanDeploymentRow is the pgx.Rows counterpart used by List. Identical to
// scanDeployment minus the ErrNoRows mapping (Rows.Next controls iteration).
func scanDeploymentRow(rows pgx.Rows) (Deployment, error) {
	var d Deployment
	err := rows.Scan(
		&d.ID, &d.ProjectID, &d.OrgID, &d.Status,
		&d.URL, &d.Port,
		&d.ImageTag, &d.DockerfileSource, &d.DetectedStack,
		&d.BuildLog, &d.ContainerLog, &d.Error,
		&d.StartedAt, &d.FinishedAt, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return Deployment{}, err
	}
	return d, nil
}
