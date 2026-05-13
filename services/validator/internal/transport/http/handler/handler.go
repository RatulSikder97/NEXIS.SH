// Package handler hosts the validator HTTP surface: Healthz + Validate. The
// runner package abstracts the underlying sandbox so Phase 7 can swap Docker
// for Modal without touching this layer.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/nexis-eco/nexis/services/validator/internal/runner"
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

// ValidateReq mirrors the request body. RepoSHA + PatchDiff are the required
// inputs; Image + TimeoutMs are optional overrides.
type ValidateReq struct {
	RepoSHA   string `json:"repo_sha"`
	PatchDiff string `json:"patch_diff"`
	Image     string `json:"image,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

// Validate wires POST /v1/validate. Bearer token is the shared
// VALIDATOR_TOKEN supplied via env on both control-plane + validator.
//
// Response codes:
//   - 200 — tests ran AND all passed.
//   - 422 — tests ran but at least one failed (returns logs + summary).
//   - 500 — sandbox spawn failed before tests could run.
//   - 401 — bad / missing bearer.
func Validate(r runner.Runner, expectedToken string, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer "+expectedToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var in ValidateReq
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		res, err := r.Run(req.Context(), runner.RunRequest{
			RepoSHA:   in.RepoSHA,
			PatchDiff: in.PatchDiff,
			Image:     in.Image,
			TimeoutMs: in.TimeoutMs,
		})
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			logger.Warn("validate failed", "err", err, "logs_len", len(res.Logs))
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":      err.Error(),
				"logs":       res.Logs,
				"duration_ms": res.DurationMs,
			})
			return
		}
		if !res.TestsPassed {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(res)
			return
		}
		_ = json.NewEncoder(w).Encode(res)
	}
}
