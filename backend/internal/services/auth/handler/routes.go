// Package handler exposes auth-service HTTP routes (Chi).
package handler

import (
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	platerrors "nexis/backend/internal/platform/errors"
	"nexis/backend/internal/platform/httpserver"
	"nexis/backend/internal/platform/probes"
	"nexis/backend/internal/services/auth/service"
)

type Handler struct {
	svc      *service.Service
	readyFns []probes.Check
}

func New(svc *service.Service, ready ...probes.Check) *Handler {
	return &Handler{svc: svc, readyFns: ready}
}

// Routes returns the mounted Chi router.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Use(httpserver.RequestID)
	r.Use(httpserver.Recoverer)
	r.Use(httpserver.SecurityHeaders)
	r.Use(httpserver.Logger)

	probes.Mount(r, h.readyFns...)

	r.Route("/v1", func(r chi.Router) {
		// Webhooks (no auth — validated by signature)
		r.Post("/webhooks/clerk", h.handleClerkWebhook)

		// Authenticated endpoints — caller is expected to have org/user already set in ctx
		// by the api-gateway. When called directly (tests / dev), use header overrides.
		r.Get("/me", h.handleMe)

		r.Route("/orgs/{org_id}/api-keys", func(r chi.Router) {
			r.Get("/", h.handleListAPIKeys)
			r.Post("/", h.handleCreateAPIKey)
		})
		r.Delete("/api-keys/{id}", h.handleRevokeAPIKey)
	})

	return r
}

// ----- handlers -----

func (h *Handler) handleClerkWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		platerrors.Write(w, r, platerrors.Wrap(platerrors.KindBadRequest, "read body", err), httpserver.RequestIDFromContext(r.Context()))
		return
	}
	if err := h.svc.ProcessClerkWebhook(r.Context(), r.Header, body); err != nil {
		platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	clerkUserID := httpserver.UserIDFromContext(r.Context())
	if clerkUserID == "" {
		// dev convenience: allow header override when api-gateway hasn't injected ctx
		clerkUserID = r.Header.Get("X-Dev-Clerk-User-Id")
	}
	orgID := httpserver.OrgIDFromContext(r.Context())
	if orgID == "" {
		orgID = r.Header.Get("X-Dev-Org-Id")
	}
	if clerkUserID == "" {
		platerrors.Write(w, r, platerrors.New(platerrors.KindUnauthorized, "no identity"), httpserver.RequestIDFromContext(r.Context()))
		return
	}
	me, err := h.svc.Me(r.Context(), clerkUserID, orgID)
	if err != nil {
		platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, me)
}

type createAPIKeyReq struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	TTLDays   int      `json:"ttl_days"`
}

type createAPIKeyResp struct {
	Key       string `json:"key"`        // shown ONCE
	APIKey    any    `json:"api_key"`    // model.APIKey
}

func (h *Handler) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "org_id")
	var req createAPIKeyReq
	if err := httpserver.DecodeJSON(r, &req); err != nil {
		platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
		return
	}
	createdBy := httpserver.UserIDFromContext(r.Context())
	ttl := time.Duration(req.TTLDays) * 24 * time.Hour
	k, plain, err := h.svc.IssueAPIKey(r.Context(), orgID, createdBy, req.Name, req.Scopes, ttl)
	if err != nil {
		platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
		return
	}
	httpserver.WriteJSON(w, http.StatusCreated, createAPIKeyResp{Key: plain, APIKey: k})
}

func (h *Handler) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "org_id")
	keys, err := h.svc.ListAPIKeys(r.Context(), orgID)
	if err != nil {
		platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": keys})
}

func (h *Handler) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := httpserver.OrgIDFromContext(r.Context())
	if orgID == "" {
		orgID = r.Header.Get("X-Dev-Org-Id")
	}
	if err := h.svc.RevokeAPIKey(r.Context(), id, orgID); err != nil {
		platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
