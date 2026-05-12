// Package http wires the Chi router. Handlers live in handler/, middleware in
// middleware/. Phase 2 introduces Deps so the server can carry adapter
// instances (db pool, auth provider) into handlers without re-running the
// factory per request.
package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/llm"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// Deps carries pre-built adapter instances into the router. Auth-protected
// routes are wired here in Stage 2; the LLM diag route still reads cfg directly
// through the factory.
//
// Two pools live here for Stage 3:
//
//   - Pool: the admin/superuser pool (DATABASE_URL). Used by the AuthProvider
//     for privileged ops that must work BEFORE a session exists — signup
//     (organizations, users, org_members INSERT), magic-token issuance,
//     session creation, session verification. These ops cannot run under RLS
//     because there's no principal yet to pin app.current_org_id to.
//   - AppPool: the application-role pool (DATABASE_URL_APP, nexis_app non-
//     superuser). Used by the RLS middleware to open the per-request tx that
//     binds the tenant GUC. Tenant queries (api_keys CRUD today, audit_log in
//     Stage 4) go through this pool via db.WithTx → db.FromCtx in the pgstore.
//
// In tests we pass nil for both pools and rely on memStore — RLS is skipped
// because the wiring guards on AppPool != nil.
type Deps struct {
	Pool    *pgxpool.Pool
	AppPool *pgxpool.Pool
	Auth    domain.AuthProvider
}

func New(cfg config.Config, logger *slog.Logger, deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(60 * time.Second))

	// Auth decorates every request with an optional Principal. RequireAuth
	// downstream enforces presence on protected routes. Wired only when an
	// AuthProvider is supplied so the healthz-only test harness keeps working.
	if deps.Auth != nil {
		r.Use(appmw.Auth(deps.Auth))
	}

	r.Get("/healthz", handler.Healthz("control-plane"))

	if provider, err := llm.NewFromConfig(cfg); err == nil {
		r.Get("/v1/_diag/llm", handler.LLMDiag(provider))
	} else {
		logger.Warn("llm provider not configured", "err", err)
	}

	if deps.Auth != nil {
		// Public auth routes.
		r.Post("/v1/auth/signup", handler.Signup(deps.Auth, cfg))
		r.Post("/v1/auth/login", handler.Login(deps.Auth, cfg))
		r.Post("/v1/auth/magic", handler.Magic(deps.Auth))
		r.Get("/v1/auth/verify", handler.Verify(deps.Auth, cfg))

		// Protected routes — RequireAuth issues 401 if no principal is in ctx;
		// RLS (when an AppPool is wired) opens a per-request tx and binds
		// app.current_org_id so tenant-table queries see only the caller's org.
		r.Group(func(g chi.Router) {
			g.Use(appmw.RequireAuth)
			if deps.AppPool != nil {
				g.Use(appmw.RLS(deps.AppPool))
			}
			g.Post("/v1/auth/logout", handler.Logout(deps.Auth, cfg))
			g.Post("/v1/auth/mfa/enroll", handler.MFAEnroll(deps.Auth))
			g.Post("/v1/auth/mfa/verify", handler.MFAVerify(deps.Auth))
			g.Delete("/v1/auth/mfa", handler.MFADisable(deps.Auth))
			g.Post("/v1/apikeys", handler.APIKeyCreate(deps.Auth))
			g.Get("/v1/apikeys", handler.APIKeyList(deps.Auth))
			g.Delete("/v1/apikeys/{id}", handler.APIKeyRevoke(deps.Auth))
			g.Get("/v1/me", handler.Me(deps.Auth))
		})
	}

	return r
}
