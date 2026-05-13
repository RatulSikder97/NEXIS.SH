// Package handler — sample-seeding HTTP surface (Phase 8 — public beta).
//
// One endpoint: POST /v1/workspaces/{ws}/seed-sample. Lets a freshly-onboarded
// org kick a recovery without standing up a Sentry integration first. The
// handler:
//
//   1. Loads the validator's null-pointer fixture (services/validator/fixtures/
//      incidents/demo-null-pointer.json).
//   2. Builds a synthetic incident JSON payload from it.
//   3. Calls WorkflowService.Start with workflowType="RecoveryPipeline" so
//      the existing demo path is reused exactly.
//
// The fixture path is the same set of candidates PipelineDemo searches so the
// in-container vs. local-dev contract stays consistent.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// seedSampleResp is the body of POST /v1/workspaces/{ws}/seed-sample. The
// run_id lets the client jump straight to the pipelines SSE stream to watch
// the recovery unfold.
type seedSampleResp struct {
	RunID string `json:"run_id"`
}

// SeedSample wires POST /v1/workspaces/{ws_id}/seed-sample. Owner|Admin.
func SeedSample(svc domain.WorkflowService, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		wsID := chi.URLParam(r, "ws_id")
		if wsID == "" {
			writeError(w, http.StatusBadRequest, "workspace id required")
			return
		}
		// Reuse the same fixture loader as PipelineDemo so the seed-sample
		// path emits identical incident JSON to the dev demo button. We don't
		// fail when the fixture is missing — the workflow accepts a minimal
		// payload and the agents fall back to placeholder content.
		inc := loadFixtureIncident("null-deref")
		payload := map[string]any{
			"incident_id":  "sample",
			"triggered_by": "seed_sample",
			"scenario":     "null-deref",
		}
		if inc != nil {
			payload["incident"] = inc
		}
		inputBytes, _ := json.Marshal(payload)
		run, err := svc.Start(r.Context(), princ, wsID, "RecoveryPipeline", inputBytes)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "workspace not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "start failed")
			return
		}
		auditWrite(r, aud, princ, "pipelines.seed_sample", run.ID, map[string]any{
			"workspace_id": wsID,
		})
		writeJSON(w, http.StatusAccepted, seedSampleResp{RunID: run.ID})
	}
}
