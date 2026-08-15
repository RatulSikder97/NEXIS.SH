// Package handler — preview-deployments HTTP surface (deploy-engine
// integration).
//
// Three endpoints under /v1/projects/{id}:
//
//	POST /v1/projects/{id}/deploy                              (owner|admin)
//	GET  /v1/projects/{id}/deployments                         (any member)
//	POST /v1/projects/{id}/deployments/{deployment_id}/stop    (owner|admin)
//
// The deploy call is ASYNCHRONOUS. Only the fast part — minting the
// deployment id, resolving the GitHub installation, minting a short-lived
// token, and a cheap Dockerfile preflight probe — runs inline; the response
// carries a "building" row the instant that's done. The actual clone + image
// build + container run + health check runs in a detached goroutine and
// updates the same row when it finishes, then — on a failed build — inserts
// a RawIncident so the failure enters the exact same self-healing pipeline
// every other incident source uses.
//
// It was NOT always this shape. The whole request used to block on the
// engine round-trip, wrapped in the router's 60-second global Timeout
// middleware. A cold build (no cached base-image layers, a large `npm ci`)
// routinely takes longer than that, and when it did, two things went wrong
// at once: the client saw a bare timeout with no explanation, and the
// handler's own attempt to persist a "failed" row afterward used r.Context(),
// which the Timeout middleware had already canceled — so
// DeploymentsRepo.Create failed too, and NOTHING was recorded. The Ops tab
// showed no history at all for an attempt that very much happened. This
// package's `DeploymentStatusBuilding` constant existed, unused, before this
// change — the async shape was the original intent; the handler that used it
// was never written.
package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
	recoverywf "github.com/nexis-eco/nexis/services/control-plane/internal/workflow/recovery"
)

// deployTimeoutMs is the per-deploy budget forwarded to the engine — the
// contract's default. The engine enforces it server-side; the adapter
// client's HTTP timeout (services/control-plane/internal/adapter/deployengine)
// and deployAsyncBudget below both allow slack on top so the engine's own
// timeout fires cleanly instead of being preempted by ours.
//
// 8 minutes, not 2: a cold build (no cached base-image layers, a real
// `npm ci`/`pip install`) has been observed taking several minutes end to
// end. This budget was only ever survivable at 120000ms because it used to
// run inside a 60-second HTTP request anyway, so raising it here is what
// running it in a detached goroutine (see DeploymentsCreate) actually buys.
const deployTimeoutMs = 480000

// deployAsyncBudget bounds the detached goroutine DeploymentsCreate starts.
// Deliberately looser than deployTimeoutMs so the engine's own budget is
// what actually fires on a hung build, producing a real error message
// instead of a bare "context canceled".
const deployAsyncBudget = 10 * time.Minute

// deployLogTailBytes bounds how much of the build/container logs ride into
// the RawIncident payload. Failures surface at the END of a build log, so we
// keep the tail — same 4KB budget qa_loop uses for its regression logs.
const deployLogTailBytes = 4000

// DeploymentsStore is the narrow slice of *repo.DeploymentsRepo the handlers
// consume. Declaring the interface here keeps the handlers trivially
// fakeable, matching the ProjectsService convention above.
type DeploymentsStore interface {
	Create(ctx context.Context, d repo.Deployment) (repo.Deployment, error)
	Get(ctx context.Context, deploymentID string) (repo.Deployment, error)
	List(ctx context.Context, projectID string, limit int) ([]repo.Deployment, error)
	Update(ctx context.Context, deploymentID string, fields map[string]any) (repo.Deployment, error)
}

// DeployEngineService is the deploy-engine client port. The concrete
// *deployengine.Client satisfies it (it is the same method set as
// recoverywf.DeployEngineClient); tests substitute a fake.
type DeployEngineService interface {
	Deploy(ctx context.Context, in recoverywf.DeployRequest) (recoverywf.DeployResponse, error)
	Stop(ctx context.Context, deploymentID string) error
}

// GitHubTokenMinter is the slice of the GitHub integration provider the
// deploy path needs: a short-lived installation token for clone auth, plus
// the contents-API preview read behind the "detected: has Dockerfile"
// indicator. *github.Provider satisfies both methods.
type GitHubTokenMinter interface {
	MintInstallationToken(ctx context.Context, installationID int64) (string, error)
	GetFileContents(ctx context.Context, installationID int64, owner, repoName, path, ref string) ([]byte, error)
}

// DeploymentsDeps bundles the deployment handlers' dependencies, mirroring
// SystemHealthDeps. Incidents may be nil (dev boot without Postgres) — the
// failed-deploy incident insert is then skipped with a WARN.
type DeploymentsDeps struct {
	Projects    ProjectsService
	Deployments DeploymentsStore
	Engine      DeployEngineService
	GitHub      GitHubTokenMinter
	Incidents   domain.IncidentSink

	// Integrations resolves the org's GitHub App installation when a project
	// row predates installation-id inheritance. Optional: nil just means no
	// fallback, and the request fails with the same message as before.
	Integrations OrgIntegrationReader

	// AppPool is the RLS-bound application pool. runDeploy is a detached
	// goroutine with no request-scoped principal in ctx, so Deployments.Update
	// and Incidents.Insert — both RLS-enforced — need app.current_org_id
	// pinned by hand via runWithTenantTx (integration_oauth.go), the same
	// fix Sentinel's detector needed for the identical reason: a background
	// goroutine's plain context.Background() satisfies no tenant policy, so
	// every write inside it either silently no-ops ("not found" on an
	// UPDATE whose WHERE clause RLS narrowed to nothing) or is flatly
	// refused. Nil is tolerated (dev boot without Postgres, or unit tests
	// using an in-memory store); runWithTenantTx degrades to calling
	// straight through when AppPool is nil.
	AppPool *pgxpool.Pool
}

// OrgIntegrationReader is the narrow read the deploy path needs to fall back
// to the org-level GitHub installation. *repo.IntegrationsRepo satisfies it.
type OrgIntegrationReader interface {
	Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
}

// deploymentResp is the wire shape of one deployments row. Timestamps are
// RFC3339; zero-valued optionals are omitted.
type deploymentResp struct {
	ID               string `json:"id"`
	ProjectID        string `json:"project_id"`
	Status           string `json:"status"`
	URL              string `json:"url,omitempty"`
	Port             int    `json:"port,omitempty"`
	ImageTag         string `json:"image_tag,omitempty"`
	DockerfileSource string `json:"dockerfile_source,omitempty"`
	DetectedStack    string `json:"detected_stack,omitempty"`
	BuildLog         string `json:"build_log,omitempty"`
	ContainerLog     string `json:"container_log,omitempty"`
	Error            string `json:"error,omitempty"`
	StartedAt        string `json:"started_at,omitempty"`
	FinishedAt       string `json:"finished_at,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
	// DockerfilePreview is the control-plane-side preflight guess ("repo"
	// when a root Dockerfile exists on the target ref, "generated"
	// otherwise) — filled only on the POST /deploy response. The engine's
	// own post-clone detection (DockerfileSource) is authoritative.
	DockerfilePreview string `json:"dockerfile_preview,omitempty"`
}

// DeploymentsCreate wires POST /v1/projects/{id}/deploy. Owner|Admin.
//
// The response mirrors the deploy-engine's own convention: 201 with the
// persisted row when the container is up, 422 with the same shape (status
// "failed" + logs) when the build or health check failed. Only transport /
// auth problems produce a bare error status.
func DeploymentsCreate(d DeploymentsDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if d.Engine == nil {
			writeError(w, http.StatusServiceUnavailable, "deploy engine not configured")
			return
		}
		if d.GitHub == nil {
			writeError(w, http.StatusServiceUnavailable, "github app not configured")
			return
		}
		project, err := d.Projects.Get(r.Context(), princ, id)
		if err != nil {
			mapProjectError(w, err)
			return
		}
		repoFull := project.Selectors.GitHubRepo
		if repoFull == "" {
			writeError(w, http.StatusBadRequest, "project has no github repository bound")
			return
		}
		instID := project.Selectors.GitHubInstallationID
		if instID <= 0 {
			// Projects bound through the console before installation-id
			// inheritance landed carry 0 here even though the org has the
			// App installed. Resolve it from the org connection rather than
			// making the operator re-bind the repo.
			instID = orgInstallationID(r.Context(), d.Integrations, princ.OrgID)
		}
		if instID <= 0 {
			writeError(w, http.StatusBadRequest, "project has no github installation bound")
			return
		}
		owner, repoName, found := strings.Cut(repoFull, "/")
		if !found || owner == "" || repoName == "" {
			writeError(w, http.StatusBadRequest, "project github_repo must be owner/name")
			return
		}

		instToken, err := d.GitHub.MintInstallationToken(r.Context(), instID)
		if err != nil {
			writeError(w, http.StatusBadGateway, "github: "+err.Error())
			return
		}

		branch := project.Selectors.GitHubDefaultBranch
		if branch == "" {
			branch = "main"
		}

		// Preflight Dockerfile probe — a cheap contents-API read so the UI
		// can show "detected: has Dockerfile" without waiting for the clone.
		// Best-effort: any error (404 = no Dockerfile, transport hiccup)
		// falls back to "generated"; the engine's post-clone check is the
		// authority either way.
		preview := "generated"
		if contents, pErr := d.GitHub.GetFileContents(r.Context(), instID, owner, repoName, "Dockerfile", branch); pErr == nil && len(contents) > 0 {
			preview = "repo"
		}

		deploymentID := uuid.NewString()
		startedAt := time.Now().UTC()
		row, err := d.Deployments.Create(r.Context(), repo.Deployment{
			ID:               deploymentID,
			ProjectID:        project.ID,
			OrgID:            princ.OrgID,
			Status:           repo.DeploymentStatusBuilding,
			DockerfileSource: preview,
			StartedAt:        &startedAt,
		})
		if err != nil {
			slog.Default().Error("deployments.create.persist_failed",
				"deployment_id", deploymentID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		// The slow part — clone, build, run, health-check — happens off the
		// request. context.Background() deliberately: r.Context() dies with
		// this response, and the build must outlive it. project/princ.OrgID
		// are copied by value into the closure so nothing here reads r after
		// the handler returns.
		go d.runDeploy(deploymentID, project, princ.OrgID, repoFull, branch, instToken)

		out := toDeploymentResp(row)
		out.DockerfilePreview = preview
		httpJSON(w, http.StatusAccepted, out)
	}
}

// runDeploy is the detached second half of DeploymentsCreate: it calls the
// engine, then updates the row that was already returned to the client.
// Every exit path updates that row — a transport failure, an engine-reported
// build/health-check failure, and success all resolve it to a terminal
// state, because a "building" row that never resolves is worse than a
// "failed" one: the console would show a spinner forever with no way to
// tell a slow build from an abandoned one.
func (d DeploymentsDeps) runDeploy(deploymentID string, project domain.Project, orgID, repoFull, branch, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), deployAsyncBudget)
	defer cancel()

	resp, engineErr := d.Engine.Deploy(ctx, recoverywf.DeployRequest{
		DeploymentID: deploymentID,
		ProjectID:    project.ID,
		OrgID:        orgID,
		Repo:         repoFull,
		Branch:       branch,
		CommitSHA:    "",
		GitHubToken:  token,
		TimeoutMs:    deployTimeoutMs,
	})
	if engineErr != nil {
		// Transport/auth failure — the engine never ran the pipeline. The row
		// already exists (created synchronously before this goroutine
		// started), so this is an Update, not a Create — the fix for the bug
		// where a slow build vanished from history entirely.
		//
		// runWithTenantTx pins app.current_org_id for the duration of the
		// Update. Without it this call ran under a bare, unbound ctx (the
		// preceding Engine.Deploy call needed no such binding — it's an HTTP
		// request to a different service, not a DB write) and the RLS policy
		// on `deployments` hid every row from it, so Update's WHERE id=$1
		// matched nothing and returned "not found" — the exact same defect
		// class Sentinel's detector had before AppPool was wired there.
		fields := map[string]any{
			"status":      repo.DeploymentStatusFailed,
			"error":       engineErr.Error(),
			"finished_at": time.Now().UTC(),
		}
		if uErr := runWithTenantTx(ctx, d.AppPool, domain.Principal{OrgID: orgID}, func(txCtx context.Context) error {
			_, err := d.Deployments.Update(txCtx, deploymentID, fields)
			return err
		}); uErr != nil {
			slog.Default().Warn("deployments.run.update_failed",
				"deployment_id", deploymentID, "err", uErr)
		}
		return
	}

	finalRow := deploymentRowFromEngine(project, orgID, deploymentID, resp)
	if uErr := runWithTenantTx(ctx, d.AppPool, domain.Principal{OrgID: orgID}, func(txCtx context.Context) error {
		_, err := d.Deployments.Update(txCtx, deploymentID, deploymentUpdateFields(finalRow))
		return err
	}); uErr != nil {
		slog.Default().Warn("deployments.run.update_failed",
			"deployment_id", deploymentID, "err", uErr)
	}

	// THE integration point with the self-healing loop: a failed deploy
	// becomes a RawIncident in the same pipeline every other source feeds.
	// GitHubRepo is the fingerprint Sentinel's router matches back to this
	// project; the stacktrace/logs payload keys are what PollFatalSince
	// projects into IncidentRow for Pathfinder. Same RLS binding as above —
	// incidents_raw has the identical tenant_isolation policy.
	if resp.Status != repo.DeploymentStatusRunning && d.Incidents != nil {
		iErr := runWithTenantTx(ctx, d.AppPool, domain.Principal{OrgID: orgID}, func(txCtx context.Context) error {
			return d.Incidents.Insert(txCtx, orgID, deployFailureIncident(project, deploymentID, resp))
		})
		if iErr != nil {
			slog.Default().Warn("deployments.run.incident_insert_failed",
				"deployment_id", deploymentID, "err", iErr)
		}
	}
}

// deploymentUpdateFields projects a repo.Deployment onto the column
// allow-list DeploymentsRepo.Update accepts. started_at is deliberately
// excluded: the row already carries the time the user clicked Deploy, which
// is the more useful "when did this happen" for the console than whatever
// instant the engine itself began building.
func deploymentUpdateFields(row repo.Deployment) map[string]any {
	fields := map[string]any{
		"status":            row.Status,
		"url":               row.URL,
		"port":              row.Port,
		"image_tag":         row.ImageTag,
		"dockerfile_source": row.DockerfileSource,
		"detected_stack":    row.DetectedStack,
		"build_log":         row.BuildLog,
		"container_log":     row.ContainerLog,
		"error":             row.Error,
	}
	if row.FinishedAt != nil {
		fields["finished_at"] = *row.FinishedAt
	} else {
		fields["finished_at"] = time.Now().UTC()
	}
	return fields
}

// DeploymentsList wires GET /v1/projects/{id}/deployments. Any authenticated
// member of the org may read — same posture as ProjectsGet (reads are open,
// mutations are owner|admin). Returns the 20 most recent rows, newest first.
func DeploymentsList(d DeploymentsDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		project, err := d.Projects.Get(r.Context(), princ, id)
		if err != nil {
			mapProjectError(w, err)
			return
		}
		rows, err := d.Deployments.List(r.Context(), project.ID, 20)
		if err != nil {
			slog.Default().Error("deployments.list_failed", "project_id", project.ID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out := make([]deploymentResp, 0, len(rows))
		for _, row := range rows {
			out = append(out, toDeploymentResp(row))
		}
		httpJSON(w, http.StatusOK, out)
	}
}

// DeploymentsStop wires POST /v1/projects/{id}/deployments/{deployment_id}/stop.
// Owner|Admin. The engine's 404 (instance restarted, container already gone)
// is treated as idempotent success — the row still flips to stopped so the
// UI converges on reality.
func DeploymentsStop(d DeploymentsDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		deploymentID := chi.URLParam(r, "deployment_id")
		if id == "" || deploymentID == "" {
			writeError(w, http.StatusBadRequest, "id and deployment_id required")
			return
		}
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if d.Engine == nil {
			writeError(w, http.StatusServiceUnavailable, "deploy engine not configured")
			return
		}
		project, err := d.Projects.Get(r.Context(), princ, id)
		if err != nil {
			mapProjectError(w, err)
			return
		}
		dep, err := d.Deployments.Get(r.Context(), deploymentID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			slog.Default().Error("deployments.stop.get_failed", "deployment_id", deploymentID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if dep.ProjectID != project.ID {
			// The row exists but belongs to a sibling project — hide it
			// behind the same uniform 404 the projects surface uses.
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		if err := d.Engine.Stop(r.Context(), deploymentID); err != nil && !errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusBadGateway, "deploy engine: "+err.Error())
			return
		}
		updated, err := d.Deployments.Update(r.Context(), deploymentID, map[string]any{
			"status": repo.DeploymentStatusStopped,
		})
		if err != nil {
			slog.Default().Error("deployments.stop.update_failed", "deployment_id", deploymentID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		httpJSON(w, http.StatusOK, toDeploymentResp(updated))
	}
}

// deploymentRowFromEngine folds an engine DeployResponse into the row we
// persist. Unknown engine statuses collapse to "failed" so the CHECK
// constraint on deployments.status never trips.
func deploymentRowFromEngine(p domain.Project, orgID, deploymentID string, resp recoverywf.DeployResponse) repo.Deployment {
	status := repo.DeploymentStatusFailed
	if resp.Status == repo.DeploymentStatusRunning {
		status = repo.DeploymentStatusRunning
	}
	d := repo.Deployment{
		ID:               deploymentID,
		ProjectID:        p.ID,
		OrgID:            orgID,
		Status:           status,
		URL:              resp.URL,
		Port:             resp.Port,
		ImageTag:         resp.ImageTag,
		DockerfileSource: resp.DockerfileSource,
		DetectedStack:    resp.DetectedStack,
		BuildLog:         resp.BuildLog,
		ContainerLog:     resp.ContainerLog,
		Error:            resp.Error,
	}
	if !resp.StartedAt.IsZero() {
		t := resp.StartedAt
		d.StartedAt = &t
	}
	if !resp.FinishedAt.IsZero() {
		t := resp.FinishedAt
		d.FinishedAt = &t
	}
	return d
}

// deployFailureIncident builds the RawIncident for a failed preview deploy.
// Modelled on data_engineer's driftIncident: the "stacktrace" payload key
// carries the error + build-log tail (what PollFatalSince projects into
// IncidentRow.Stacktrace for Pathfinder's evidence chain) and "logs" carries
// the container-log tail. SourceEventID is a deterministic hash of the
// deployment id so a retried request dedupes onto one row.
func deployFailureIncident(p domain.Project, deploymentID string, resp recoverywf.DeployResponse) domain.RawIncident {
	errText := resp.Error
	if errText == "" {
		errText = "deploy failed"
	}
	stack := errText
	if tail := logTail(resp.BuildLog, deployLogTailBytes); tail != "" {
		stack += "\n" + tail
	}
	return domain.RawIncident{
		Source:        "deploy_engine",
		SourceEventID: deployEventID(deploymentID),
		Title:         fmt.Sprintf("Deploy failed: %s — %s", p.Selectors.GitHubRepo, logTail(errText, 140)),
		Level:         "fatal",
		Service:       p.Slug,
		Environment:   string(p.Environment),
		Payload: map[string]any{
			"stacktrace":        stack,
			"logs":              logTail(resp.ContainerLog, deployLogTailBytes),
			"deployment_id":     deploymentID,
			"project_id":        p.ID,
			"detected_stack":    resp.DetectedStack,
			"dockerfile_source": resp.DockerfileSource,
			"error":             errText,
		},
		// Fingerprint — Sentinel's router resolves the project via
		// github_repo, exactly like the github webhook adapter does.
		GitHubRepo: p.Selectors.GitHubRepo,
	}
}

// deployEventID is the deterministic source_event_id for a deployment's
// failure incident — sha256 of the deployment id, truncated like the
// schema-drift fingerprint, so idempotent inserts collapse retries.
func deployEventID(deploymentID string) string {
	sum := sha256.Sum256([]byte(deploymentID))
	return "deploy-" + hex.EncodeToString(sum[:])[:16]
}

// logTail returns the LAST max bytes of s — build failures report at the end
// of the log, so the tail carries the signal.
func logTail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}

// toDeploymentResp converts a repo.Deployment to its wire shape.
func toDeploymentResp(d repo.Deployment) deploymentResp {
	out := deploymentResp{
		ID:               d.ID,
		ProjectID:        d.ProjectID,
		Status:           d.Status,
		URL:              d.URL,
		Port:             d.Port,
		ImageTag:         d.ImageTag,
		DockerfileSource: d.DockerfileSource,
		DetectedStack:    d.DetectedStack,
		BuildLog:         d.BuildLog,
		ContainerLog:     d.ContainerLog,
		Error:            d.Error,
		StartedAt:        nullableTimeStr(d.StartedAt),
		FinishedAt:       nullableTimeStr(d.FinishedAt),
	}
	if !d.CreatedAt.IsZero() {
		out.CreatedAt = d.CreatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

// orgInstallationID reads the org's connected GitHub App installation id.
// Returns 0 when the reader is unwired, the org has no connected GitHub
// integration, or the stored id is not a positive integer — every one of
// which leaves the caller's original "not bound" error intact.
func orgInstallationID(ctx context.Context, reader OrgIntegrationReader, orgID string) int64 {
	if reader == nil {
		return 0
	}
	conn, _, err := reader.Get(ctx, orgID, domain.IntegrationGitHub)
	if err != nil || conn.Status != domain.StatusConnected || conn.InstallationID == "" {
		return 0
	}
	id, err := strconv.ParseInt(conn.InstallationID, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}
