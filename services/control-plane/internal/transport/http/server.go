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

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/audit"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/llm"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
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
//
// Stage 5 adds AuditLister: the read-side of the audit log. Concrete
// *audit.HMACWriter satisfies both AuditWriter (Write) and the Lister
// interface (List). We keep two fields for cleanliness so the write-path
// stays bound to the narrower domain.AuditWriter interface.
type Deps struct {
	Pool           *pgxpool.Pool
	AppPool        *pgxpool.Pool
	Auth           domain.AuthProvider
	Audit          domain.AuditWriter
	AuditLister    audit.Lister
	Integrations   *integration.Registry
	Workspaces     domain.WorkspaceService
	WorkspacesRepo *repo.WorkspacesRepo
	Billing        domain.BillingProvider
	BillingRepo    *repo.BillingRepo
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
		aud := deps.Audit
		if aud == nil {
			aud = noopAudit{}
		}

		// Public auth routes. WorkspacesRepo is wrapped as a WorkspaceChecker
		// so the response can include HasWorkspace for the onboarding gate.
		// nil is fine — the helper treats a missing checker as "no workspace
		// yet" so existing tests + the dev-no-pool path keep working.
		var wsChecker handler.WorkspaceChecker
		if deps.WorkspacesRepo != nil {
			wsChecker = deps.WorkspacesRepo
		}
		r.Post("/v1/auth/signup", handler.Signup(deps.Auth, aud, cfg, wsChecker))
		r.Post("/v1/auth/login", handler.Login(deps.Auth, aud, cfg, wsChecker))
		r.Post("/v1/auth/magic", handler.Magic(deps.Auth))
		r.Get("/v1/auth/verify", handler.Verify(deps.Auth, aud, cfg))

		// Public invite routes (no session yet).
		r.Get("/v1/invites/{token}", handler.InviteGet(deps.Auth))
		r.Post("/v1/invites/{token}/claim", handler.InviteClaim(deps.Auth, cfg))

		// Public webhook ingest. HMAC-authenticated inside each adapter; no
		// session cookie or bearer is involved. Pool argument is the admin
		// pool — see handler.Webhook for the RLS pinning rationale.
		if deps.Integrations != nil && deps.Pool != nil {
			r.Post("/v1/webhooks/{provider}/{org_id}", handler.Webhook(deps.Integrations, deps.Pool))
		}

		// Protected routes — RequireAuth issues 401 if no principal is in ctx;
		// RLS (when an AppPool is wired) opens a per-request tx and binds
		// app.current_org_id so tenant-table queries see only the caller's org.
		r.Group(func(g chi.Router) {
			g.Use(appmw.RequireAuth)
			if deps.AppPool != nil {
				g.Use(appmw.RLS(deps.AppPool))
			}

			// Routes open to any authenticated principal regardless of role.
			g.Post("/v1/auth/logout", handler.Logout(deps.Auth, aud, cfg))
			g.Post("/v1/auth/mfa/enroll", handler.MFAEnroll(deps.Auth, aud))
			g.Post("/v1/auth/mfa/verify", handler.MFAVerify(deps.Auth, aud))
			g.Delete("/v1/auth/mfa", handler.MFADisable(deps.Auth, aud))
			g.Get("/v1/apikeys", handler.APIKeyList(deps.Auth))
			g.Delete("/v1/apikeys/{id}", handler.APIKeyRevoke(deps.Auth, aud))
			g.Get("/v1/me", handler.Me(deps.Auth, wsChecker))
			g.Get("/v1/me/preferences", handler.GetPreferences(deps.Auth))
			g.Patch("/v1/me/preferences", handler.PatchPreferences(deps.Auth, aud))

			// Audit chain integrity verify endpoint. Reads the admin pool
			// (deps.Pool) — see handler.AuditVerify for why it bypasses RLS.
			if deps.Pool != nil {
				g.Get("/v1/audit/verify", handler.AuditVerify([]byte(cfg.AuditSecret), deps.Pool))
			}

			if deps.Integrations != nil {
				g.Get("/v1/integrations", handler.IntegrationsList(deps.Integrations))
			}

			// Workspace read paths — open to any authenticated principal.
			if deps.Workspaces != nil {
				g.Get("/v1/workspaces/regions", handler.WorkspaceRegions())
				g.Get("/v1/workspaces", handler.WorkspacesList(deps.Workspaces))
				g.Get("/v1/workspaces/{id}", handler.WorkspaceGet(deps.Workspaces))
				g.Get("/v1/workspaces/{id}/events", handler.WorkspaceEvents(deps.Workspaces))
			}

			// Owner OR admin — Stage 5 RBAC. Owners and admins can manage
			// integrations + api keys + the audit list/CSV; members are
			// read-only on their own profile.
			g.Group(func(g2 chi.Router) {
				g2.Use(appmw.RequireRole(domain.RoleOwner, domain.RoleAdmin))
				g2.Post("/v1/apikeys", handler.APIKeyCreate(deps.Auth, aud))
				if deps.AuditLister != nil {
					g2.Get("/v1/audit", handler.AuditList(deps.AuditLister))
					g2.Get("/v1/audit.csv", handler.AuditCSV(deps.AuditLister))
				}
				if deps.Integrations != nil {
					g2.Post("/v1/integrations/{provider}/connect", handler.IntegrationsConnect(deps.Integrations, aud))
					g2.Delete("/v1/integrations/{provider}", handler.IntegrationsDisconnect(deps.Integrations, aud))
					g2.Get("/v1/integrations/github/mock_install", handler.GitHubMockInstall(deps.Integrations, aud, cfg.AppBaseURL, cfg.AppEnv))
				}
				g2.Get("/v1/orgs/{id}/invites", handler.InviteList(deps.Auth))

				// Workspaces — create is owner|admin per Phase 3.5 RBAC.
				if deps.Workspaces != nil {
					g2.Post("/v1/workspaces", handler.WorkspaceCreate(deps.Workspaces, aud))
				}

				// Billing read paths + dev recompute are owner|admin.
				if deps.Billing != nil {
					g2.Get("/v1/billing/payment-method", handler.BillingGetPaymentMethod(deps.Billing))
				}
				if deps.BillingRepo != nil {
					g2.Get("/v1/billing/invoices", handler.BillingInvoices(deps.BillingRepo))
					g2.Get("/v1/billing/usage", handler.BillingUsage(deps.BillingRepo))
					if cfg.AppEnv == "dev" {
						g2.Post("/v1/billing/invoices/recompute", handler.BillingRecompute(deps.BillingRepo))
					}
				}
			})

			// Owner only — issuing + revoking invites is reserved for the
			// org's owner to keep the principal-elevation path tight.
			g.Group(func(g2 chi.Router) {
				g2.Use(appmw.RequireRole(domain.RoleOwner))
				g2.Post("/v1/orgs/{id}/invites", handler.InviteIssue(deps.Auth, aud))
				g2.Delete("/v1/orgs/{id}/invites/{token_hash}", handler.InviteRevoke(deps.Auth, aud))

				if deps.Workspaces != nil {
					g2.Delete("/v1/workspaces/{id}", handler.WorkspaceSuspend(deps.Workspaces, aud))
				}
				if deps.Billing != nil {
					g2.Post("/v1/billing/payment-method", handler.BillingAddPaymentMethod(deps.Billing, aud))
					g2.Delete("/v1/billing/payment-method", handler.BillingDeletePaymentMethod(deps.Billing, aud))
				}
			})
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
