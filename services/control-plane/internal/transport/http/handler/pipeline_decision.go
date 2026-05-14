package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// PipelineDecisionResp is the wire shape returned by
// GET /v1/workspaces/{ws_id}/pipelines/{run_id}/decision. Mirrors
// domain.ApprovalDecision but with ISO timestamps.
type PipelineDecisionResp struct {
	ID            string  `json:"id"`
	WorkflowRunID string  `json:"workflow_run_id"`
	WorkspaceID   string  `json:"workspace_id"`
	Severity      string  `json:"severity"`
	Decision      string  `json:"decision"`
	DecidedBy     string  `json:"decided_by,omitempty"`
	DecidedAt     string  `json:"decided_at,omitempty"`
	Notes         string  `json:"notes,omitempty"`
	Scenario      string  `json:"scenario,omitempty"`
	RiskScore     float64 `json:"risk_score"`
	CreatedAt     string  `json:"created_at"`
}

// PipelineDecisionGet wires GET /v1/workspaces/{ws_id}/pipelines/{run_id}/decision.
// Returns the approval_decisions row for the workflow run so the FE can
// render the pending-approval card with severity, countdown, and a decide
// button. 404 when the run has no decision yet (e.g. low-severity scenarios
// that auto-approve and never insert a row).
func PipelineDecisionGet(repo domain.ApprovalRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		runID := chi.URLParam(r, "run_id")
		wsID := chi.URLParam(r, "ws_id")
		d, err := repo.GetByRun(r.Context(), runID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "no decision row")
				return
			}
			writeError(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		// Tenancy + workspace gate — the repo is RLS-scoped but
		// double-check the workspace_id matches the URL so we don't
		// leak rows across workspaces inside the same org.
		if d.OrgID != princ.OrgID || (wsID != "" && d.WorkspaceID != wsID) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		resp := PipelineDecisionResp{
			ID:            d.ID,
			WorkflowRunID: d.WorkflowRunID,
			WorkspaceID:   d.WorkspaceID,
			Severity:      string(d.Severity),
			Decision:      string(d.Decision),
			DecidedBy:     d.DecidedBy,
			Notes:         d.Notes,
			Scenario:      d.Scenario,
			RiskScore:     d.RiskScore,
			CreatedAt:     d.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if !d.DecidedAt.IsZero() {
			resp.DecidedAt = d.DecidedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
