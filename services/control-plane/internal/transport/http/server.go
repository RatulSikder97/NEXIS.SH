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

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/approval"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/audit"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/llm"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	platformtemporal "github.com/nexis-eco/nexis/services/control-plane/internal/platform/temporal"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
	"github.com/nexis-eco/nexis/services/control-plane/internal/usecase"
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

	// Phase 4 — Temporal-backed pipeline runs.
	Workflows     domain.WorkflowService
	WorkflowsRepo *repo.WorkflowRepo

	// Phase 5 Stage 7 — eval harness + token-budget pill.
	//
	// EvalRepo persists eval_runs + eval_transcripts; TokenLedgerRepo
	// powers the topbar budget pill. EvalRunner is the side-by-side
	// OpenAI vs Ollama orchestrator kicked off by the POST handler.
	EvalRepo        *repo.EvalRepo
	EvalRunner      *usecase.EvalRunner
	TokenLedgerRepo *repo.TokenLedgerRepo

	// Phase 8 — public-beta surface.
	//
	//   InviteCodes — repo for system-wide invite-code mint/list/revoke +
	//     atomic signup redemption.
	//   NASATLX — repo for the workload-survey POST /v1/nasa-tlx endpoint.
	//   OrgStats — repo for GET /v1/me/org-stats successful_recoveries_count.
	//   Sentinel — narrow port satisfied by *sentinel.Detector via TriggerOne;
	//     null when the detector is disabled / its deps are missing.
	//   TemporalHB — heartbeat tracker the /v1/healthz/temporal endpoint reads.
	InviteCodes *repo.InviteCodesRepo
	NASATLX     *repo.NASATLXRepo
	OrgStats    *repo.OrgStatsRepo
	Sentinel    handler.SentinelTriggerer
	TemporalHB  *platformtemporal.Heartbeat

	// Task 8 — Slack interactivity approvals.
	//
	// SlackDecider is the adapter that resolves a workflow run id (decoded
	// from the Slack button payload) to a workspace + org and forwards to
	// the existing SignalerService. Optional — when nil, the
	// POST /v1/integrations/slack/interactivity route is not mounted.
	SlackDecider handler.SlackApprovalsService

	// Projects — Phase 7 self-healing targets.
	//
	// Projects bundles integration selector mappings, recovery policy, and
	// SLOs into one persisted aggregate. Sentinel uses it to route incoming
	// incidents to the right repo/app/channel. Optional — when nil the
	// /v1/(workspaces|projects)/* routes are not mounted.
	Projects handler.ProjectsService

	// Approvals — Phase 6 approval-decisions read API.
	//
	// Powers GET /v1/workspaces/{ws}/pipelines/{run}/decision so the FE can
	// poll for the pending/decided state of a paused recovery. Optional —
	// when nil the route is not mounted.
	Approvals domain.ApprovalRepository

	// ApprovalSignaler — Phase 6 approve/reject write API.
	//
	// Powers POST /v1/workspaces/{ws}/pipelines/{run}/approve|reject. Fires
	// the Temporal signal so the paused workflow advances + persists the
	// terminal decision row. Optional — when nil the routes are not mounted.
	ApprovalSignaler *approval.SignalerService

	// Operational-surfaces stage — repos + probe deps for the read-only
	// operator endpoints (activity feed, system-health, integration log,
	// validator runs, cost rollup, knowledge status, system-status pill).
	//
	//   WebhookDeliveries — append-only audit log written by the webhook
	//     handlers, read by /v1/integrations/webhooks. Optional.
	//
	//   SystemHealth — bundle of dependency handles the /v1/system-health
	//     fanout probes. Empty fields surface as "disabled" rather than
	//     "down" so the dashboard can render a mixed-deploy correctly.
	WebhookDeliveries *repo.WebhookDeliveriesRepo
	SystemHealth      handler.SystemHealthDeps
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
		r.Post("/v1/auth/signup", handler.Signup(deps.Auth, aud, cfg, wsChecker, nil, false))
		r.Post("/v1/auth/login", handler.Login(deps.Auth, aud, cfg, wsChecker))
		r.Post("/v1/auth/magic", handler.Magic(deps.Auth))
		r.Get("/v1/auth/verify", handler.Verify(deps.Auth, aud, cfg))

		// Phase 7 — WorkOS OAuth callback. Public (no session yet); the
		// handler validates the one-shot nexis_oauth_state cookie against the
		// state query param before exchanging the code via the AuthProvider.
		r.Get("/v1/auth/workos/callback", handler.WorkOSCallback(deps.Auth, aud, cfg))

		// Public invite routes (no session yet).
		r.Get("/v1/invites/{token}", handler.InviteGet(deps.Auth))
		r.Post("/v1/invites/{token}/claim", handler.InviteClaim(deps.Auth, cfg))

		// Sentry probe — Phase 8 public-beta synthetic monitor. The
		// signature IS the auth; no session is involved. Wires only when
		// a secret is configured (env SENTRY_PROBE_SECRET or the legacy
		// dev fallback GitHub default).
		probeSecret := []byte(cfg.SentryProbeSecret)
		if len(probeSecret) == 0 {
			probeSecret = []byte(cfg.GitHubDefaultWebhookSecret)
		}
		if len(probeSecret) > 0 {
			r.Post("/v1/integrations/sentry/probe", handler.SentryProbe(probeSecret))
		}

		// Public webhook ingest. HMAC-authenticated inside each adapter; no
		// session cookie or bearer is involved. Pool argument is the admin
		// pool — see handler.Webhook for the RLS pinning rationale.
		//
		// WebhookDeliveries (optional) records every attempt for the
		// operator-side /v1/integrations/webhooks audit log.
		//
		// BodyLimit: webhook payloads can run larger than the protected /v1
		// surface (Datadog/Sentry routinely ship multi-MB events). We cap at
		// 5 MiB per delivery so a runaway upstream can't OOM the process;
		// bodies bigger than the cap return 413 before the handler runs
		// (SQA F-1).
		if deps.Integrations != nil && deps.Pool != nil {
			r.With(appmw.BodyLimit(5*1024*1024)).Post(
				"/v1/webhooks/{provider}/{org_id}",
				handler.Webhook(deps.Integrations, deps.Pool, deps.WebhookDeliveries, cfg),
			)

			// Task 8 — Datadog + PagerDuty webhook URLs are configured by the
			// customer inside their own provider dashboard, so they cannot
			// embed our control-plane URL pattern. We accept the org id as a
			// `?org=<org_id>` query string instead. The handler is
			// otherwise identical to Webhook above (same HMAC verify, same
			// RLS pinning).
			r.With(appmw.BodyLimit(5*1024*1024)).Post(
				"/v1/integrations/datadog/webhook",
				handler.WebhookByQuery(domain.IntegrationDatadog, deps.Integrations, deps.Pool, deps.WebhookDeliveries, cfg),
			)
			r.With(appmw.BodyLimit(5*1024*1024)).Post(
				"/v1/integrations/pagerduty/webhook",
				handler.WebhookByQuery(domain.IntegrationPagerDuty, deps.Integrations, deps.Pool, deps.WebhookDeliveries, cfg),
			)
		}

		// Task 8 — Slack interactivity. Slack-signed POST; no session
		// involved. The signature IS auth — the handler verifies
		// X-Slack-Signature over the raw body before parsing the payload.
		if deps.SlackDecider != nil && len(cfg.SlackSigningSecret) > 0 {
			r.Post("/v1/integrations/slack/interactivity",
				handler.SlackInteractivity(deps.SlackDecider, cfg.SlackSigningSecret))
		}

		// Task 1 — OAuth install callbacks. Public routes (the user arrives
		// here via a 302 from GitHub / Slack with no session cookie set yet);
		// the handler re-reads the session cookie if present, but a missing
		// session lands the user on /console/integrations?install_error=
		// unauthorized so the dashboard can render a banner without crashing.
		//
		// The install-START routes are session-gated and mounted further down
		// inside the owner|admin group.
		if deps.Integrations != nil {
			r.Get("/v1/integrations/github/install/callback",
				handler.GitHubInstallCallback(deps.Integrations, aud, cfg, deps.AppPool))
			r.Get("/v1/integrations/slack/callback",
				handler.SlackInstallCallback(deps.Integrations, aud, cfg))
		}

		// Protected routes — RequireAuth issues 401 if no principal is in ctx;
		// RLS (when an AppPool is wired) opens a per-request tx and binds
		// app.current_org_id so tenant-table queries see only the caller's org.
		//
		// BodyLimit caps every protected request body at 1 MiB. The /v1
		// surface is JSON-only with bounded shapes (auth bodies, project
		// configs, eval payloads) so any caller pushing more than 1 MiB is
		// either misconfigured or hostile. SSE streams and file-upload
		// endpoints would need their own carve-outs; today none of the
		// protected routes accept binary uploads. SQA F-1.
		r.Group(func(g chi.Router) {
			g.Use(appmw.BodyLimit(1 * 1024 * 1024))
			g.Use(appmw.RequireAuth)
			// Per-org rate limit (SQA F-7). Mounted after RequireAuth so the
			// principal is in ctx; the middleware keys by principal.OrgID and
			// is a no-op when cfg.RateLimitEnabled is false or the params are
			// zero-shaped (test-friendly).
			g.Use(appmw.RateLimit(appmw.RateLimitConfig{
				RPS:     cfg.RateLimitPerOrgRPS,
				Burst:   cfg.RateLimitBurst,
				Enabled: cfg.RateLimitEnabled,
			}))
			if deps.AppPool != nil {
				g.Use(appmw.RLS(deps.AppPool))
			}

			// Routes open to any authenticated principal regardless of role.
			g.Post("/v1/auth/logout", handler.Logout(deps.Auth, aud, cfg))
			g.Post("/v1/auth/mfa/enroll", handler.MFAEnroll(deps.Auth, aud, cfg))
			g.Post("/v1/auth/mfa/verify", handler.MFAVerify(deps.Auth, aud))
			g.Delete("/v1/auth/mfa", handler.MFADisable(deps.Auth, aud))
			g.Get("/v1/apikeys", handler.APIKeyList(deps.Auth))
			g.Delete("/v1/apikeys/{id}", handler.APIKeyRevoke(deps.Auth, aud))
			g.Get("/v1/me", handler.Me(deps.Auth, wsChecker))
			g.Get("/v1/me/preferences", handler.GetPreferences(deps.Auth))
			g.Patch("/v1/me/preferences", handler.PatchPreferences(deps.Auth, aud))

			// Phase 8 — org stats. successful_recoveries_count for the
			// current principal's org. Open to any authenticated principal
			// so the dashboard topline tile renders for members.
			if deps.OrgStats != nil {
				g.Get("/v1/me/org-stats", handler.OrgStats(deps.OrgStats))
			}

			// Phase 8 — NASA-TLX workload survey. Per user × org (not per
			// workspace). Any authenticated principal can submit after a
			// recovery; the response body has the row id for follow-up edits.
			if deps.NASATLX != nil {
				g.Post("/v1/nasa-tlx", handler.NASATLXSubmit(deps.NASATLX, aud))
			}

			// Audit chain integrity verify endpoint. Reads the admin pool
			// (deps.Pool) — see handler.AuditVerify for why it bypasses RLS.
			if deps.Pool != nil {
				g.Get("/v1/audit/verify", handler.AuditVerify([]byte(cfg.AuditSecret), deps.Pool))
			}

			if deps.Integrations != nil {
				g.Get("/v1/integrations", handler.IntegrationsList(deps.Integrations))
			}

			// Phase 5+6 — agents fleet. Read-only catalog + per-org observability
			// rolled up from activity_events. Open to any authenticated principal
			// in the workspace.
			//
			// Drill-down endpoints (per-agent run log + per-run event timeline)
			// share the same auth + tenancy gate as the catalog: any
			// authenticated principal whose (org_id, workspace_id) match the
			// query parameters. The handlers filter at SQL time so the RLS
			// boundary is enforced regardless of which pool is used.
			if deps.Pool != nil {
				g.Get("/v1/workspaces/{ws_id}/agents", handler.AgentsList(deps.Pool))
				g.Get("/v1/workspaces/{ws_id}/agents/{name}/runs", handler.AgentRunsList(deps.Pool))
				g.Get("/v1/workspaces/{ws_id}/agents/{name}/runs/{run_id}/events", handler.AgentRunEvents(deps.Pool))
			}

			// Workspace read paths — open to any authenticated principal.
			if deps.Workspaces != nil {
				g.Get("/v1/workspaces/regions", handler.WorkspaceRegions())
				g.Get("/v1/workspaces", handler.WorkspacesList(deps.Workspaces))
				g.Get("/v1/workspaces/{id}", handler.WorkspaceGet(deps.Workspaces))
				g.Get("/v1/workspaces/{id}/events", handler.WorkspaceEvents(deps.Workspaces))
			}

			// Projects — read paths open to any authenticated principal.
			// Mutations live in the owner|admin sub-group below.
			if deps.Projects != nil {
				g.Get("/v1/workspaces/{ws_id}/projects", handler.ProjectsListByWorkspace(deps.Projects))
				g.Get("/v1/projects/{id}", handler.ProjectsGet(deps.Projects))
				g.Get("/v1/projects/{id}/recovery-policy", handler.ProjectsGetPolicy(deps.Projects))
			}

			// Phase 4 — pipeline read paths. Any authenticated principal can
			// list / read runs + tail the SSE stream for their org's
			// workspaces. The handler verifies workspace ownership before
			// streaming.
			if deps.Workflows != nil {
				g.Get("/v1/workspaces/{ws_id}/pipelines", handler.PipelinesList(deps.Workflows))
				g.Get("/v1/workspaces/{ws_id}/pipelines/{run_id}", handler.PipelineGet(deps.Workflows))
				g.Get("/v1/workspaces/{ws_id}/pipelines/{run_id}/events",
					handler.PipelineEvents(deps.Workflows, deps.WorkflowsRepo, deps.WorkspacesRepo))
				if deps.Approvals != nil {
					g.Get("/v1/workspaces/{ws_id}/pipelines/{run_id}/decision",
						handler.PipelineDecisionGet(deps.Approvals))
				}
			}

			// Phase 5 Stage 7 — eval read paths + token-budget pill. Open
			// to any authenticated principal so members can watch the
			// matrix without owner-elevation. The POST (kick off run)
			// lives in the owner|admin sub-group below.
			if deps.EvalRepo != nil {
				g.Get("/v1/workspaces/{ws_id}/eval", handler.EvalRunsList(deps.EvalRepo))
				g.Get("/v1/workspaces/{ws_id}/eval/{run_id}", handler.EvalRunGet(deps.EvalRepo))
			}
			if deps.TokenLedgerRepo != nil {
				g.Get("/v1/workspaces/{ws_id}/agents/budget", handler.BudgetStatus(deps.TokenLedgerRepo))
			}

			// Operational surfaces — eight read-only endpoints + one
			// integration probe. All session-gated; everything except the
			// /probe is open to any authenticated principal.
			//
			//   /v1/orgs/{org_id}/activity                 — paginated cross-workflow feed
			//   /v1/orgs/{org_id}/activity-stream          — SSE companion (2s poll)
			//   /v1/system-health                          — dependency probe (15s cache)
			//   /v1/integrations/webhooks                  — webhook delivery audit log
			//   /v1/validator/runs                         — recent validator sandbox runs
			//   /v1/orgs/{org_id}/cost                     — per-org token-ledger rollup
			//   /v1/knowledge/status                       — pgvector retrieval state
			//   /v1/system-status                          — sidebar pill (30s per-org cache)
			//
			// The {org_id} URL value is informational — handlers always
			// filter on the principal's OrgID, so a leaked URL doesn't leak
			// data.
			if deps.WorkflowsRepo != nil {
				g.Get("/v1/orgs/{org_id}/activity", handler.OrgActivity(deps.WorkflowsRepo))
				g.Get("/v1/orgs/{org_id}/activity-stream", handler.OrgActivityStream(deps.WorkflowsRepo))
			}
			g.Get("/v1/system-health", handler.SystemHealth(deps.SystemHealth))
			g.Get("/v1/integrations/webhooks", handler.IntegrationsWebhooks(deps.WebhookDeliveries))
			if deps.Pool != nil {
				g.Get("/v1/validator/runs", handler.ValidatorRuns(deps.Pool))
				g.Get("/v1/orgs/{org_id}/cost", handler.OrgCost(deps.Pool))
				g.Get("/v1/knowledge/status", handler.KnowledgeStatus(deps.Pool))
			}
			g.Get("/v1/system-status", handler.SystemStatus(handler.SystemStatusDeps{
				AdminPool: deps.Pool,
				Health:    deps.SystemHealth,
			}))

			// Owner OR admin — Stage 5 RBAC. Owners and admins can manage
			// integrations + api keys + the audit list/CSV; members are
			// read-only on their own profile.
			g.Group(func(g2 chi.Router) {
				g2.Use(appmw.RequireRole(domain.RoleOwner, domain.RoleAdmin))
				if deps.ApprovalSignaler != nil {
					g2.Post("/v1/workspaces/{ws_id}/pipelines/{run_id}/approve",
						handler.PipelineApprove(deps.ApprovalSignaler))
					g2.Post("/v1/workspaces/{ws_id}/pipelines/{run_id}/reject",
						handler.PipelineReject(deps.ApprovalSignaler))
				}
				g2.Post("/v1/apikeys", handler.APIKeyCreate(deps.Auth, aud))
				if deps.AuditLister != nil {
					g2.Get("/v1/audit", handler.AuditList(deps.AuditLister))
					g2.Get("/v1/audit.csv", handler.AuditCSV(deps.AuditLister))
				}
				if deps.Integrations != nil {
					g2.Post("/v1/integrations/{provider}/connect", handler.IntegrationsConnect(deps.Integrations, aud, cfg))
					g2.Delete("/v1/integrations/{provider}", handler.IntegrationsDisconnect(deps.Integrations, aud, cfg))
					g2.Get("/v1/integrations/github/mock_install", handler.GitHubMockInstall(deps.Integrations, aud, cfg.AppBaseURL, cfg.AppEnv))
					g2.Get("/v1/integrations/github/repos", handler.GitHubReposList(deps.Integrations))

					// Manual probe — operator forces a Status refresh on a
					// single integration. Owner|Admin only so members can't
					// hammer an upstream API. Audited as integration.probed.
					g2.Post("/v1/integrations/{provider}/probe", handler.IntegrationProbe(deps.Integrations, aud))

					// Task 1 — install-START routes. Session-gated so the
					// callback can attribute the resulting connection to the
					// caller's org. The matching CALLBACK routes are public
					// (mounted above) — see integration_oauth.go for the CSRF
					// handshake.
					g2.Get("/v1/integrations/github/install", handler.GitHubInstallStart(cfg))
					g2.Get("/v1/integrations/slack/install", handler.SlackInstallStart(deps.Integrations, cfg))
				}
				g2.Get("/v1/orgs/{id}/invites", handler.InviteList(deps.Auth))

				// Workspaces — create is owner|admin per Phase 3.5 RBAC.
				if deps.Workspaces != nil {
					g2.Post("/v1/workspaces", handler.WorkspaceCreate(deps.Workspaces, aud))
				}

				// Projects — create/update/archive + policy mutations.
				// Audit emission lives in the usecase layer so non-HTTP
				// callers (future SDK / CLI / sentinel-triggered apply)
				// pick up the same project.* audit actions.
				if deps.Projects != nil {
					g2.Post("/v1/workspaces/{ws_id}/projects", handler.ProjectsCreate(deps.Projects, cfg))
					g2.Patch("/v1/projects/{id}", handler.ProjectsPatch(deps.Projects))
					g2.Delete("/v1/projects/{id}", handler.ProjectsArchive(deps.Projects))
					g2.Put("/v1/projects/{id}/recovery-policy", handler.ProjectsPutPolicy(deps.Projects))
				}

				// Phase 4 — pipeline mutations are owner|admin. The /demo
				// route is gated to dev so prod-shaped clusters don't
				// accidentally accept synthetic-incident triggers.
				if deps.Workflows != nil {
					g2.Post("/v1/workspaces/{ws_id}/pipelines", handler.PipelineCreate(deps.Workflows, aud))
					if cfg.AppEnv == "dev" {
						g2.Post("/v1/workspaces/{ws_id}/pipelines/demo", handler.PipelineDemo(deps.Workflows, aud, cfg))
					}
				}

				// Phase 5 Stage 7 — POST /v1/workspaces/{ws}/eval is
				// owner|admin. The runner is detached so the handler
				// returns 202 immediately; the client polls the list
				// endpoint to watch openai_status + ollama_status.
				if deps.EvalRunner != nil {
					g2.Post("/v1/workspaces/{ws_id}/eval", handler.EvalRunCreate(deps.EvalRunner, aud))
				}

				// Phase 8 — seed-sample lets a freshly-onboarded org kick a
				// recovery against the validator fixture without standing up
				// a Sentry integration first. Owner|Admin so members can't
				// fabricate runs against another tenant's workspace.
				if deps.Workflows != nil {
					g2.Post("/v1/workspaces/{ws_id}/seed-sample", handler.SeedSample(deps.Workflows, aud))
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
					// Phase 7 — Stripe SetupIntent flow used by Stripe Elements
					// in the dashboard. Local provider serves synthetic secrets
					// so the same wire surface works without STRIPE_SECRET_KEY.
					g2.Post("/v1/billing/payment-method/intent", handler.BillingCreateSetupIntent(deps.Billing, aud, cfg))
					g2.Post("/v1/billing/payment-method/confirm", handler.BillingConfirmSetupIntent(deps.Billing, aud, cfg))
				}

				// Phase 8 — owner-only admin surfaces.
				//
				//   /v1/invite-codes (POST/GET/DELETE) — system-wide invite
				//     codes for the signup gate. Mint/list/revoke are owner-
				//     elevated; redemption is wired into POST /v1/auth/signup
				//     via the ?invite=<code> query param (public route).
				//   /v1/admin/eval-export — CSV dump of the eval matrix.
				//     Carries token + cost data not exposed to members.
				//   /v1/admin/sentinel/trigger — manual sentinel escape hatch
				//     so operators can demo a recovery without waiting for a
				//     real Sentry fatal. Cannot target another org (the
				//     handler also enforces the principal-org check).
				if deps.InviteCodes != nil {
					g2.Post("/v1/invite-codes", handler.InviteCodesCreate(deps.InviteCodes, aud))
					g2.Get("/v1/invite-codes", handler.InviteCodesList(deps.InviteCodes))
					g2.Delete("/v1/invite-codes/{code}", handler.InviteCodesRevoke(deps.InviteCodes, aud))
				}
				if deps.EvalRepo != nil {
					g2.Get("/v1/admin/eval-export", handler.AdminEvalExport(deps.EvalRepo))
				}
				if deps.Sentinel != nil {
					g2.Post("/v1/admin/sentinel/trigger", handler.SentinelTrigger(deps.Sentinel, aud))
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
