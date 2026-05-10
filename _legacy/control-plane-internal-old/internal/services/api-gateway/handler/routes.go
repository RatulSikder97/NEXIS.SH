// Package handler exposes the api-gateway BFF routes.
package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"nexis/backend/internal/platform/httpserver"
	"nexis/backend/internal/platform/probes"
	"nexis/backend/internal/services/api-gateway/config"
	"nexis/backend/internal/services/api-gateway/service"
)

type Handler struct {
	cfg config.Config
	svc *service.Service
}

func New(cfg config.Config, svc *service.Service) *Handler {
	return &Handler{cfg: cfg, svc: svc}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Use(httpserver.RequestID)
	r.Use(httpserver.Recoverer)
	r.Use(httpserver.SecurityHeaders)
	r.Use(httpserver.Logger)
	r.Use(httpserver.CORS(h.cfg.AllowOrigins))

	probes.Mount(r)

	r.Route("/v1", func(r chi.Router) {
		// In dev, allow X-Dev-* header overrides instead of full Clerk JWT verification.
		// In prod, this middleware would call Clerk JWKS verifier.
		r.Use(h.devOrClerkAuth)

		// Read endpoints powering the dashboard Home + surfaces.
		r.Get("/me", h.handleMe)
		r.Get("/incidents", h.handleListIncidents)
		r.Get("/incidents/{id}", h.handleGetIncident)
		r.Get("/agents", h.handleListAgents)
		r.Get("/audit", h.handleListAudit)
		r.Get("/integrations", h.handleListIntegrations)
		r.Get("/metrics/home", h.handleHomeMetrics)

		// Mutation endpoints.
		r.Post("/incidents/{id}/approve", h.handleApprove)
		r.Post("/incidents/{id}/reject", h.handleReject)
		r.Post("/demo/scenarios/{id}/run", h.handleRunDemo)
	})

	return r
}

// devOrClerkAuth: in dev mode, accept X-Dev-Org-Id / X-Dev-User-Id / X-Dev-Role headers
// for fast local iteration. In prod, this is replaced by the platform/authn Clerk verifier.
func (h *Handler) devOrClerkAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.cfg.DevBypassAuth {
			ctx := r.Context()
			if v := r.Header.Get("X-Dev-User-Id"); v != "" {
				ctx = httpserver.WithUser(ctx, v)
			} else {
				ctx = httpserver.WithUser(ctx, "dev-user-1")
			}
			if v := r.Header.Get("X-Dev-Org-Id"); v != "" {
				ctx = httpserver.WithOrg(ctx, v)
			} else {
				ctx = httpserver.WithOrg(ctx, "dev-org-1")
			}
			if v := r.Header.Get("X-Dev-Role"); v != "" {
				ctx = httpserver.WithRole(ctx, v)
			} else {
				ctx = httpserver.WithRole(ctx, "admin")
			}
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		// Production path: delegate to platform/authn (wired by main.go via middleware injection).
		next.ServeHTTP(w, r)
	})
}
