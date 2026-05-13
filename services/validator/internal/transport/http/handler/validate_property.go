// Phase 6 property-test surface — POST /v1/validate/property.
//
// Distinct from POST /v1/validate (which spawns a sandboxed pytest run via
// the Docker runner), this endpoint dispatches a Hypothesis property
// suite via the sidecar at `internal/runner/hypothesis.go`. The request
// body intentionally mirrors the standard validate shape so the
// control-plane's validator client can reuse most of its plumbing.
//
// Auth: same shared bearer (`VALIDATOR_TOKEN`) as POST /v1/validate.
//
// Response codes:
//   - 200 — sidecar ran AND every property held.
//   - 422 — sidecar ran but at least one property failed.
//   - 500 — sidecar transport / dial failure.
//   - 401 — bad / missing bearer.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/nexis-eco/nexis/services/validator/internal/runner"
)

// ValidatePropertyReq mirrors the wire format the control-plane sends.
// MaxRuntimeSeconds is capped at 30s by the sidecar regardless of the
// inbound value (Phase 6 constraint).
type ValidatePropertyReq struct {
	RepoSHA           string `json:"repo_sha"`
	PatchDiff         string `json:"patch_diff"`
	RepoPath          string `json:"repo_path,omitempty"`
	MaxExamples       int    `json:"max_examples,omitempty"`
	MaxRuntimeSeconds int    `json:"max_runtime_seconds,omitempty"`
}

// ValidatePropertyResp is the response shape callers depend on.
// HypothesisFailures is always non-nil — empty `[]` when every property
// held — so the JSON shape is stable.
type ValidatePropertyResp struct {
	TestsPassed        bool                         `json:"tests_passed"`
	HypothesisFailures []runner.HypothesisFailure   `json:"hypothesis_failures"`
	DurationMs         int64                        `json:"duration_ms"`
	RepoSHA            string                       `json:"repo_sha,omitempty"`
	Error              string                       `json:"error,omitempty"`
}

// ValidateProperty wires POST /v1/validate/property. It dispatches the
// request to the Hypothesis sidecar through ``hyp`` and returns the
// parsed response on the wire.
func ValidateProperty(hyp runner.HypothesisRunner, expectedToken string, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer "+expectedToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var in ValidatePropertyReq
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		hReq := runner.HypothesisRequest{
			PatchDiff:         in.PatchDiff,
			RepoPath:          in.RepoPath,
			MaxExamples:       in.MaxExamples,
			MaxRuntimeSeconds: in.MaxRuntimeSeconds,
		}
		hRes, err := hyp.Run(req.Context(), hReq)

		w.Header().Set("Content-Type", "application/json")
		out := ValidatePropertyResp{
			TestsPassed:        hRes.TestsPassed,
			HypothesisFailures: hRes.Failures,
			DurationMs:         hRes.DurationMs,
			RepoSHA:            in.RepoSHA,
			Error:              hRes.Error,
		}
		// Sidecar always returns Failures as []HypothesisFailure but a
		// transport-level error can leave hRes zero-valued — pin the
		// nil-safety so JSON callers always see `[]`.
		if out.HypothesisFailures == nil {
			out.HypothesisFailures = []runner.HypothesisFailure{}
		}

		if err != nil {
			logger.Warn("validate-property sidecar dial failed",
				"err", err,
				"repo_sha", in.RepoSHA,
				"duration_ms", out.DurationMs,
			)
			out.Error = err.Error()
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(out)
			return
		}

		if !out.TestsPassed {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}
