// Package handler — preview-deployments HTTP surface (deploy-engine
// integration).
//
// Three endpoints under /v1/projects/{id}:
//
//	POST /v1/projects/{id}/deploy                              (owner|admin)
//	GET  /v1/projects/{id}/deployments                         (any member)
//	POST /v1/projects/{id}/deployments/{deployment_id}/stop    (owner|admin)
//
// The deploy call is synchronous: control-plane mints the deployment id,
// mints a short-lived GitHub installation token, calls the deploy-engine
// sidecar, persists the round-trip into the deployments table, and — on a
// failed build — inserts a RawIncident so the failure enters the exact same
// self-healing pipeline every other incident source uses.
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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
	recoverywf "github.com/nexis-eco/nexis/services/control-plane/internal/workflow/recovery"
)

// deployTimeoutMs is the per-deploy budget forwarded to the engine — the
// contract's default. The engine enforces it server-side; the adapter
// client's HTTP timeout adds slack on top.
const deployTimeoutMs = 120000

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
		resp, err := d.Engine.Deploy(r.Context(), recoverywf.DeployRequest{
			DeploymentID: deploymentID,
			ProjectID:    project.ID,
			OrgID:        princ.OrgID,
			Repo:         repoFull,
			Branch:       branch,
			CommitSHA:    "",
			GitHubToken:  instToken,
			TimeoutMs:    deployTimeoutMs,
		})
		if err != nil {
			// Transport/auth failure — the engine never ran the pipeline.
			// Persist a failed row anyway so the ops timeline shows the
			// attempt, then surface 502.
			if _, cErr := d.Deployments.Create(r.Context(), repo.Deployment{
				ID:        deploymentID,
				ProjectID: project.ID,
				OrgID:     princ.OrgID,
				Status:    repo.DeploymentStatusFailed,
				Error:     err.Error(),
			}); cErr != nil {
				slog.Default().Warn("deployments.create.persist_failed",
					"deployment_id", deploymentID, "err", cErr)
			}
			writeError(w, http.StatusBadGateway, "deploy engine: "+err.Error())
			return
		}

		row, err := d.Deployments.Create(r.Context(), deploymentRowFromEngine(project, princ.OrgID, deploymentID, resp))
		if err != nil {
			slog.Default().Error("deployments.create.persist_failed",
				"deployment_id", deploymentID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		// THE integration point with the self-healing loop: a failed deploy
		// becomes a RawIncident in the same pipeline every other source
		// feeds. GitHubRepo is the fingerprint Sentinel's router matches
		// back to this project; the stacktrace/logs payload keys are what
		// PollFatalSince projects into IncidentRow for Pathfinder.
		if resp.Status != repo.DeploymentStatusRunning && d.Incidents != nil {
			if iErr := d.Incidents.Insert(r.Context(), princ.OrgID,
				deployFailureIncident(project, deploymentID, resp)); iErr != nil {
				slog.Default().Warn("deployments.create.incident_insert_failed",
					"deployment_id", deploymentID, "err", iErr)
			}
		}

		out := toDeploymentResp(row)
		out.DockerfilePreview = preview
		code := http.StatusCreated
		if row.Status != repo.DeploymentStatusRunning {
			code = http.StatusUnprocessableEntity
		}
		httpJSON(w, code, out)
	}
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
