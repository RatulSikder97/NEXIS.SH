// Package http wires the Chi router. Handlers live in handler/, middleware in
// middleware/. Phase 2 introduces Deps so the server can carry adapter
// instances (db pool, auth provider) into handlers without re-running the
// factory per request.
package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/llm"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// noopAudit is the fallback AuditWriter used when Deps.Audit is nil — keeps
// the handler signatures simple in tests that don't care about audit rows
// (memStore-backed unit tests, the healthz harness).
type noopAudit struct{}

func (noopAudit) Write(_ context.Context, _ domain.Principal, _, _ string, _ map[string]any) error {
	return nil
}

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
//
// Stage 4 adds Audit: the AuditWriter port. Every mutation handler calls it
// AFTER the AuthProvider call succeeds. Audit lives outside the AuthProvider
// because the chain-hashing concern is orthogonal to the auth provider choice
// (local vs workos) — both must record the same audit shape.
type Deps struct {
	Pool    *pgxpool.Pool
	AppPool *pgxpool.Pool
	Auth    domain.AuthProvider
	Audit   domain.AuditWriter
}

func New(cfg config.Config, logger *slog.Logger, deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(60 * time.Second))

	// CORS must run early so OPTIONS preflights short-circuit before chi
	// otherwise rejects them with 405. cfg.AppBaseURL is the only allowed
	// origin; this is locked down per request — see middleware/cors.go.
	r.Use(appmw.CORS(cfg.AppBaseURL))

	// Trace every request. otelhttp produces a server span and sets a
	// span context on the request ctx. The traceIDHeader middleware below
	// echoes the trace id so callers can correlate.
	r.Use(otelMiddleware)
	r.Use(traceIDHeader)

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
		// Audit writer defaults to a no-op when not wired so the route registry
		// here stays simple. Tests with the in-memory store pass deps.Audit=nil
		// and rely on the no-op fallback.
		audit := deps.Audit
		if audit == nil {
			audit = noopAudit{}
		}

		// Public auth routes.
		r.Post("/v1/auth/signup", handler.Signup(deps.Auth, audit, cfg))
		r.Post("/v1/auth/login", handler.Login(deps.Auth, audit, cfg))
		r.Post("/v1/auth/magic", handler.Magic(deps.Auth))
		r.Get("/v1/auth/verify", handler.Verify(deps.Auth, audit, cfg))

		// Protected routes — RequireAuth issues 401 if no principal is in ctx;
		// RLS (when an AppPool is wired) opens a per-request tx and binds
		// app.current_org_id so tenant-table queries see only the caller's org.
		r.Group(func(g chi.Router) {
			g.Use(appmw.RequireAuth)
			if deps.AppPool != nil {
				g.Use(appmw.RLS(deps.AppPool))
			}
			g.Post("/v1/auth/logout", handler.Logout(deps.Auth, audit, cfg))
			g.Post("/v1/auth/mfa/enroll", handler.MFAEnroll(deps.Auth, audit))
			g.Post("/v1/auth/mfa/verify", handler.MFAVerify(deps.Auth, audit))
			g.Delete("/v1/auth/mfa", handler.MFADisable(deps.Auth, audit))
			g.Post("/v1/apikeys", handler.APIKeyCreate(deps.Auth, audit))
			g.Get("/v1/apikeys", handler.APIKeyList(deps.Auth))
			g.Delete("/v1/apikeys/{id}", handler.APIKeyRevoke(deps.Auth, audit))
			g.Get("/v1/me", handler.Me(deps.Auth))

			// Audit chain integrity verify endpoint. Reads the admin pool
			// (deps.Pool) — see handler.AuditVerify for why it bypasses RLS.
			if deps.Pool != nil {
				g.Get("/v1/audit/verify", handler.AuditVerify([]byte(cfg.AuditSecret), deps.Pool))
			}
		})
	}

	return r
}

// otelMiddleware wraps the inner handler so chi-style middleware composes.
// otelhttp.NewHandler returns an http.Handler that opens a server span and
// puts the SpanContext on the request ctx; everything after it sees the
// active trace.
func otelMiddleware(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http.server",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
}

// traceIDHeader echoes the active span's trace id on the response. Callers
// (web app, curl, dashboards) can grep `x-trace-id` from the response and
// jump straight to the trace in Tempo without correlation queries.
func traceIDHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if sc := span.SpanContext(); sc.IsValid() {
			w.Header().Set("x-trace-id", sc.TraceID().String())
		}
		next.ServeHTTP(w, r)
	})
}
