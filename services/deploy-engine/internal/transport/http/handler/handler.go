// Package handler hosts the deploy-engine HTTP surface: Healthz + the
// /v1/deploy trio. The deploy package abstracts the docker pipeline so
// this layer stays a thin JSON/auth shim — same layering as the
// validator's transport/http/handler.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/deploy-engine/internal/deploy"
)

// Healthz returns a tiny JSON liveness probe. Used by docker-compose
// healthchecks + local smoke tests.
func Healthz(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": service,
		})
	}
}

// DeployReq mirrors the request body of POST /v1/deploy. DeploymentID is
// minted by the control-plane; CommitSHA + TimeoutMs are optional.
type DeployReq struct {
	DeploymentID string `json:"deployment_id"`
	ProjectID    string `json:"project_id"`
	OrgID        string `json:"org_id"`
	Repo         string `json:"repo"`
	Branch       string `json:"branch"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	GitHubToken  string `json:"github_token"`
	TimeoutMs    int    `json:"timeout_ms,omitempty"`
}

// Deploy wires POST /v1/deploy. Bearer token is the shared
// DEPLOY_ENGINE_TOKEN supplied via env on both control-plane + deploy-engine.
//
// Response codes:
//   - 200 — build + run succeeded AND the container answered HTTP.
//   - 422 — pipeline ran but failed (clone/build/health); body carries the
//     structured result with logs, same convention as the validator's
//     /v1/validate.
//   - 500 — internal failure before the pipeline could produce evidence.
//   - 401 — bad / missing bearer.
//   - 400 — malformed / incomplete body.
func Deploy(d deploy.Deployer, store *deploy.Store, expectedToken string, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !authorized(req, expectedToken) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in DeployReq
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, "bad body")
			return
		}
		if in.DeploymentID == "" || in.ProjectID == "" || in.Repo == "" || in.Branch == "" {
			writeErr(w, http.StatusBadRequest, "deployment_id, project_id, repo and branch are required")
			return
		}
		res, err := d.Deploy(req.Context(), deploy.Request{
			DeploymentID: in.DeploymentID,
			ProjectID:    in.ProjectID,
			OrgID:        in.OrgID,
			Repo:         in.Repo,
			Branch:       in.Branch,
			CommitSHA:    in.CommitSHA,
			GitHubToken:  in.GitHubToken,
			TimeoutMs:    in.TimeoutMs,
		})
		if err != nil {
			logger.Error("deploy internal error", "deployment_id", in.DeploymentID, "err", err)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		store.Put(in.DeploymentID, deploy.Record{
			Result:        res,
			ContainerName: deploy.ContainerName(in.DeploymentID),
		})
		w.Header().Set("Content-Type", "application/json")
		if res.Status != deploy.StatusRunning {
			w.WriteHeader(http.StatusUnprocessableEntity)
		}
		_ = json.NewEncoder(w).Encode(res)
	}
}

// GetDeployment wires GET /v1/deploy/{deployment_id} — the in-memory
// convenience/debug view. 404 when this instance never handled the id.
func GetDeployment(store *deploy.Store, expectedToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !authorized(req, expectedToken) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(req, "deployment_id")
		rec, ok := store.Get(id)
		if !ok {
			writeErr(w, http.StatusNotFound, "unknown deployment_id")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rec.Result)
	}
}

// StopDeployment wires POST /v1/deploy/{deployment_id}/stop. Stop is
// idempotent for tracked deployments: a container that is already gone
// still yields 200 {"status":"stopped"}.
func StopDeployment(d deploy.Deployer, store *deploy.Store, expectedToken string, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !authorized(req, expectedToken) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(req, "deployment_id")
		rec, ok := store.Get(id)
		if !ok {
			writeErr(w, http.StatusNotFound, "unknown deployment_id")
			return
		}
		if err := d.Stop(req.Context(), rec.ContainerName); err != nil {
			logger.Error("stop failed", "deployment_id", id, "container", rec.ContainerName, "err", err)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		store.SetStatus(id, deploy.StatusStopped)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
	}
}

func authorized(req *http.Request, expectedToken string) bool {
	return req.Header.Get("Authorization") == "Bearer "+expectedToken
}

// writeErr emits the JSON error shape the contract requires for
// non-200/422 statuses.
func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
