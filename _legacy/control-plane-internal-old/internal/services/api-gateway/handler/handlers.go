package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	platerrors "nexis/backend/internal/platform/errors"
	"nexis/backend/internal/platform/httpserver"
)

// ----- Read endpoints -----

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":    httpserver.UserIDFromContext(r.Context()),
			"name":  "Ratul Sikder",
			"email": "ratul@nexis.local",
		},
		"org": map[string]any{
			"id":   httpserver.OrgIDFromContext(r.Context()),
			"name": "Acme Engineering",
			"slug": "acme",
			"plan": "design_partner",
		},
		"role":        httpserver.RoleFromContext(r.Context()),
		"permissions": []string{"*"},
		"memberships": []map[string]any{
			{"id": "dev-org-1", "name": "Acme Engineering", "slug": "acme", "role": "admin"},
		},
	})
}

func (h *Handler) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	incidents, err := h.svc.ListIncidents(r.Context())
	if err != nil {
		platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": incidents})
}

func (h *Handler) handleGetIncident(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	inc, err := h.svc.GetIncident(r.Context(), id)
	if err != nil {
		platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, inc)
}

func (h *Handler) handleListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := h.svc.ListAgents(r.Context())
	if err != nil {
		platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": agents})
}

func (h *Handler) handleListAudit(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}})
}

func (h *Handler) handleListIntegrations(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"items": []map[string]any{
			{"id": "github", "kind": "github", "name": "GitHub", "health": "unknown", "connected": false},
			{"id": "sentry", "kind": "sentry", "name": "Sentry", "health": "unknown", "connected": false},
			{"id": "argocd", "kind": "argocd", "name": "ArgoCD", "health": "unknown", "connected": false},
		},
	})
}

func (h *Handler) handleHomeMetrics(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"mttr_minutes":          14,
		"mttr_delta_pct":        -38,
		"auto_recovery_pct":     72,
		"auto_recovery_delta":   6,
		"validation_pass_pct":   91,
		"validation_pass_delta": 0,
		"open_incidents":        3,
		"awaiting_approval":     1,
		"updated_at":            time.Now().UTC(),
	})
}

// ----- Mutations -----

type approvalReq struct {
	Reason string `json:"reason"`
}

func (h *Handler) handleApprove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req approvalReq
	if err := httpserver.DecodeJSON(r, &req); err != nil {
		// allow empty body
	}
	if err := h.svc.ApproveIncident(r.Context(), id, req.Reason); err != nil {
		platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleReject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req approvalReq
	if err := httpserver.DecodeJSON(r, &req); err != nil {
		// allow empty body
	}
	if err := h.svc.RejectIncident(r.Context(), id, req.Reason); err != nil {
		platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleRunDemo(w http.ResponseWriter, r *http.Request) {
	scenarioID := chi.URLParam(r, "id")
	run, err := h.svc.RunDemoScenario(r.Context(), scenarioID)
	if err != nil {
		platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
		return
	}
	httpserver.WriteJSON(w, http.StatusAccepted, run)
}
