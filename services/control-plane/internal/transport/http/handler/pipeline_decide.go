package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/approval"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// pipelineDecideReq is the request body for the approve/reject/modify
// endpoints. All three share this shape — only the URL distinguishes them;
// modified_diff is read only by the modify endpoint.
type pipelineDecideReq struct {
	Notes        string `json:"notes,omitempty"`
	ModifiedDiff string `json:"modified_diff,omitempty"`
}

// PipelineApprove wires POST /v1/workspaces/{ws_id}/pipelines/{run_id}/approve.
// Owner|Admin only. Fires the Temporal approval signal and records the
// terminal decision row. Idempotent — a second call on the same already-
// approved row returns 409 conflict.
func PipelineApprove(signaler *approval.SignalerService) http.HandlerFunc {
	return decideHandler(signaler, domain.ApprovalApproved)
}

// PipelineReject wires POST /v1/workspaces/{ws_id}/pipelines/{run_id}/reject.
// Same shape as PipelineApprove but with rejected decision.
func PipelineReject(signaler *approval.SignalerService) http.HandlerFunc {
	return decideHandler(signaler, domain.ApprovalRejected)
}

// PipelineModify wires POST /v1/workspaces/{ws_id}/pipelines/{run_id}/modify.
// The RLHF "modify-then-approve" flow: the engineer submits an edited
// unified diff in `modified_diff`, the workflow deploys THAT diff instead of
// the agent's original, and the (original, edited) pair lands in
// feedback_examples as a correction example. Same signaler path + RBAC gate
// as approve/reject; the 400 on a missing diff comes from the signaler's
// validation.
func PipelineModify(signaler *approval.SignalerService) http.HandlerFunc {
	return decideHandler(signaler, domain.ApprovalModified)
}

func decideHandler(signaler *approval.SignalerService, decision domain.ApprovalDecisionState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		runID := chi.URLParam(r, "run_id")
		wsID := chi.URLParam(r, "ws_id")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "missing run_id")
			return
		}
		var req pipelineDecideReq
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "bad json")
				return
			}
		}
		if decision == domain.ApprovalModified && req.ModifiedDiff == "" {
			writeError(w, http.StatusBadRequest, "modified_diff required")
			return
		}
		err := signaler.Decide(r.Context(), approval.DecideInput{
			Principal:     princ,
			WorkspaceID:   wsID,
			WorkflowRunID: runID,
			Decision:      decision,
			Notes:         req.Notes,
			ModifiedDiff:  req.ModifiedDiff,
		})
		if err != nil {
			switch {
			case errors.Is(err, domain.ErrNotFound):
				writeError(w, http.StatusNotFound, "approval row not found")
			case errors.Is(err, domain.ErrConflict):
				writeError(w, http.StatusConflict, "approval already decided")
			case errors.Is(err, domain.ErrForbidden):
				writeError(w, http.StatusForbidden, "wrong workspace")
			default:
				writeError(w, http.StatusInternalServerError, "decide failed")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":   "ok",
			"decision": string(decision),
		})
	}
}
