// Package main is the control-plane HTTP server entrypoint.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	temporalsdk "go.temporal.io/sdk/client"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/architect"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/backend"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/data_engineer"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/devops"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/qa"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/approval"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/audit"
	neo4jstore "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/graphstore/neo4j"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/billing"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/keyvault"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/llm"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/patchstore"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/retrieval"
	validatorclient "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/validator"
	adapterworkflow "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/workflow"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/workspace"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/cron"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
	platformneo4j "github.com/nexis-eco/nexis/services/control-plane/internal/platform/neo4j"
	otelplatform "github.com/nexis-eco/nexis/services/control-plane/internal/platform/otel"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/sse"
	temporalplatform "github.com/nexis-eco/nexis/services/control-plane/internal/platform/temporal"
	"github.com/nexis-eco/nexis/services/control-plane/internal/sentinel"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
	"github.com/nexis-eco/nexis/services/control-plane/internal/usecase"
	recoverywf "github.com/nexis-eco/nexis/services/control-plane/internal/workflow/recovery"
)

// redisAddrOrDefault reads REDIS_ADDR, falling back to the docker-compose
// service name on the same internal network so /v1/system-health can probe
// the local redis without any extra config.
func redisAddrOrDefault() string {
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		return v
	}
	return "redis:6379"
}

// runHealthcheck is the body of the --healthcheck subcommand. It dials
// http://localhost:$PORT/healthz with a short timeout and exits 0 on a 2xx
// response, 1 otherwise. Designed for `HEALTHCHECK CMD ["/app/server",
// "--healthcheck"]` on the distroless image (which has no curl/wget). The
// function never returns — it always calls os.Exit.
func runHealthcheck() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	url := "http://127.0.0.1:" + port + "/healthz"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: GET %s: %v\n", url, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "healthcheck: GET %s: status %d\n", url, resp.StatusCode)
		os.Exit(1)
	}
	os.Exit(0)
}

func main() {
	// --healthcheck short-circuits the boot path so the same binary can
	// serve as the in-container liveness probe on distroless images.
	// Parsed off a dedicated FlagSet so it does not interfere with the
	// rest of main's argv assumptions (none today).
	hcFlags := flag.NewFlagSet("control-plane", flag.ContinueOnError)
	hcFlags.SetOutput(io.Discard)
	healthcheck := hcFlags.Bool("healthcheck", false, "probe http://localhost:$PORT/healthz and exit 0/1")
	_ = hcFlags.Parse(os.Args[1:])
	if *healthcheck {
		runHealthcheck()
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config.Load()
	// Phase 7 cutover guard (Risk 16.14): refuse to boot a staging/prod
	// env that still has local providers wired. No-op in dev.
	if err := cfg.FatalIfLocalInCloud(); err != nil {
		logger.Error("cloud env misconfigured", "err", err)
		os.Exit(2)
	}
	logger.Info("control-plane starting", "port", cfg.Port, "env", cfg.AppEnv, "auth_provider", cfg.AuthProvider)

	// appCtx is the lifetime context shared by every long-running goroutine
	// in the process: the Temporal worker, the Sentinel detector, the cron
	// loop, and any future stream consumers. We cancel it from the SIGTERM
	// handler so all background work unwinds on shutdown before the HTTP
	// server is drained. Bootstrap-only work (db dial, otel init) continues
	// to use `ctx` below — those calls have their own timeouts.
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	ctx := context.Background()

	// OpenTelemetry — wire the global tracer provider before anything else
	// so handlers, db code, and middleware can rely on otel.Tracer/Span APIs
	// without panicking. If the collector is unreachable the SDK keeps
	// retrying in the background; the server stays healthy.
	shutdownOTel, err := otelplatform.Init(ctx, cfg.OTelServiceName, cfg.OTelEndpoint)
	if err != nil {
		logger.Warn("otel init failed", "err", err)
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdownOTel(shutdownCtx)
		}()
	}

	// Two pools — see httpserver.Deps for the rationale.
	//
	//   - adminPool (DATABASE_URL): privileged role used by the AuthProvider
	//     for signup/login bootstrap that must work without an RLS principal.
	//   - appPool (DATABASE_URL_APP, nexis_app): used by the RLS middleware
	//     for per-request tx-bound tenant queries.
	//
	// Missing URLs are tolerated in dev so `go run ./cmd/server` still boots
	// for the LLM-only diag surface from Phase 1.
	adminPool := mustPool(ctx, "DATABASE_URL", cfg.DatabaseURL, cfg, logger)
	if adminPool != nil {
		defer adminPool.Close()
	}
	appPool := mustPool(ctx, "DATABASE_URL_APP", cfg.DatabaseURLApp, cfg, logger)
	if appPool != nil {
		defer appPool.Close()
	}

	authProvider, err := auth.NewFromConfig(cfg, adminPool)
	if err != nil {
		logger.Error("auth provider", "err", err)
		os.Exit(1)
	}
	logger.Info("auth provider initialised", "name", authProvider.Name())

	// LocalKeyVault — decodes MASTER_KEY (base64, 32 bytes) and constructs an
	// AES-256-GCM cipher used by every integration adapter to seal webhook
	// secrets / OAuth tokens at rest in integrations.secret_ciphertext. In dev
	// we fall back to a zero key so the server boots without ceremony.
	masterKey, err := base64.StdEncoding.DecodeString(cfg.MasterKey)
	if err != nil || len(masterKey) != 32 {
		if cfg.AppEnv == "dev" {
			logger.Warn("MASTER_KEY invalid; using zero key for dev")
			masterKey = make([]byte, 32)
		} else {
			logger.Error("MASTER_KEY missing or not 32 bytes (base64)")
			os.Exit(1)
		}
	}
	kv, err := keyvault.NewLocal(masterKey)
	if err != nil {
		logger.Error("keyvault", "err", err)
		os.Exit(1)
	}

	// Audit writer wires the HMAC chain. We pass the admin pool as the
	// Querier fallback — the writer uses the per-request RLS tx when one is
	// in ctx (protected handlers) and the admin pool otherwise (signup,
	// login, magic-link consume — none of which run inside RLS). When
	// DATABASE_URL is unset in dev we wire a no-op so the server still boots
	// for LLM-only diag work.
	var auditWriter domain.AuditWriter
	var auditLister audit.Lister
	if adminPool != nil && cfg.AuditSecret != "" {
		hw := audit.New([]byte(cfg.AuditSecret), adminPool)
		auditWriter = hw
		auditLister = hw
	} else {
		logger.Warn("audit writer disabled — DATABASE_URL or AUDIT_SECRET missing")
	}

	// Integration registry — three adapters today (github, sentry, argocd),
	// all routed through the same integrations / incidents_raw repos. We pin
	// the repos to the application pool so RLS-scoped queries inside the
	// per-request tx see app.current_org_id; the pool itself is only the
	// Querier fallback for code paths that don't carry a tx.
	//
	// Phase 6 widens the repos to dual-pool — the Sentinel detector goroutine
	// (below) reads incidents_raw + integrations across every org via the
	// admin pool, bypassing RLS.
	var registry *integration.Registry
	var intRepo *repo.IntegrationsRepo
	var incRepo *repo.IncidentsRepo
	if appPool != nil {
		intRepo = repo.NewIntegrationsRepoWithAdmin(appPool, adminPool)
		incRepo = repo.NewIncidentsRepoWithAdmin(appPool, adminPool)
		registry = integration.NewRegistry(integration.Deps{
			Repo:                 intRepo,
			KV:                   kv,
			IncidentSink:         incRepo,
			GitHubDefaultSecret:  []byte(cfg.GitHubDefaultWebhookSecret),
			PagerDutyFromEmail:   cfg.PagerDutyFromEmail,
			DatadogSigningSecret: cfg.DatadogWebhookSigningSecret,
		})

		// Wire real-API clients onto the adapters when credentials are
		// present. Each adapter degrades to stub mode if its config is empty.
		stopSentryCron := registry.EnableRealAPIs(ctx, integration.RealConfig{
			AppBaseURL:               cfg.AppBaseURL,
			GitHubAppID:              cfg.GitHubAppID,
			GitHubAppPrivateKeyPEM:   cfg.GitHubAppPrivateKeyPEM,
			GitHubAppSlug:            cfg.GitHubAppSlug,
			GitHubWebhookSecret:      cfg.GitHubWebhookSecret,
			SentryBaseURL:            cfg.SentryBaseURL,
			SentryListConnectedOrgs:  intRepo.ConnectedSentryOrgs,
			SlackClientID:            cfg.SlackClientID,
			SlackClientSecret:        cfg.SlackClientSecret,
			SlackAppRedirectURI:      cfg.SlackAppRedirectURI,
			PagerDutyWebhookSecrets:  cfg.PagerDutyWebhookSecrets,
			IncidentSink:             incRepo,
		})
		if stopSentryCron != nil {
			defer stopSentryCron()
		}
		_ = strings.TrimRight // keep import in case of future use
	} else {
		logger.Warn("integration registry disabled — DATABASE_URL_APP missing")
	}

	// Phase 6 — Neo4j codegraph driver. The graphStore variable is consumed by
	// the Pathfinder L2 agent (Stage 4). A nil graphStore is tolerated — the
	// Pathfinder activity short-circuits to the canned-evidence path when the
	// driver is unreachable. We log the init outcome at boot so operators can
	// tell whether Pathfinder will have real graph evidence.
	var graphStore domain.Graph
	var neo4jDriver neo4jdriver.DriverWithContext // hoisted so SystemHealth can probe it.
	if cfg.Neo4jURI != "" {
		drv, nerr := platformneo4j.New(ctx, platformneo4j.Config{
			URI: cfg.Neo4jURI, User: cfg.Neo4jUser, Password: cfg.Neo4jPass,
		})
		if nerr != nil {
			logger.Warn("neo4j init failed; pathfinder graph evidence disabled", "err", nerr)
		} else {
			defer func() { _ = drv.Close(context.Background()) }()
			if vErr := platformneo4j.Verify(ctx, drv); vErr != nil {
				logger.Warn("neo4j verify failed; pathfinder graph evidence disabled", "err", vErr)
			} else {
				graphStore = neo4jstore.New(drv)
				neo4jDriver = drv
				logger.Info("neo4j initialised", "uri", cfg.Neo4jURI)
			}
		}
	} else {
		logger.Warn("NEO4J_URI unset — pathfinder graph evidence disabled")
	}
	_ = graphStore // consumed by Pathfinder agent in Phase 6 Stage 4

	// Phase 3.5 — workspaces + billing.
	//
	// Workspaces: the repo is pinned to the application pool so RLS-scoped
	// reads inside the per-request tx see app.current_org_id. The service
	// owns an in-process SSE broker; HTTP handlers subscribe to it.
	//
	// Billing: same shape — repo on the app pool, provider built from cfg.
	// Cron jobs (usage_ticker, invoice_roller) share the same repo; they call
	// *Admin methods that bypass RLS since there's no principal in ctx at
	// cron time.
	//
	// Both adapters are constructed only when the application pool is wired —
	// otherwise the dev (LLM-only) path keeps booting without them.
	var wsRepo *repo.WorkspacesRepo
	var billingRepo *repo.BillingRepo
	var wsService domain.WorkspaceService
	var billingProvider domain.BillingProvider
	if appPool != nil {
		wsRepo = repo.NewWorkspacesRepo(appPool, adminPool)
		billingRepo = repo.NewBillingRepo(appPool, adminPool)

		wsService = workspace.New(workspace.Config{
			Repo:     wsRepo,
			Broker:   sse.New[domain.ProvisioningStep](),
			FailRate: cfg.WorkspaceProvisionFail,
		})

		var err error
		billingProvider, err = billing.NewFromConfig(cfg, billingRepo)
		if err != nil {
			logger.Error("billing provider", "err", err)
			os.Exit(1)
		}
		logger.Info("billing provider initialised", "name", billingProvider.Name())
	} else {
		logger.Warn("workspaces + billing disabled — DATABASE_URL_APP missing")
	}

	// Phase 7 — Projects (self-healing targets). Repo runs dual-pool so
	// Sentinel's cross-tenant MatchByFingerprint sweeps via admin; the
	// usecase enforces per-tier caps + the GitHub binding rule and audits
	// every mutation under project.*. Wired only when both pools exist
	// (the dev LLM-only path keeps booting without it).
	//
	// Hoisted above the Temporal worker so the same repo can satisfy
	// BOTH the Sentinel router's ProjectMatcher port AND the activity-side
	// ProjectsReader port consumed by Activities.LoadProject. Without
	// this hoist, the Activities struct would be constructed before the
	// projects repo exists and a.Projects would stay nil — the kill-switch
	// + auto-merge branches in ApprovalGateRoute would all silently fall
	// back to the fixture path.
	//
	// projectsHandlerSvc is the handler-facing interface form. We keep the
	// concrete pointer + the interface separate so a nil concrete value
	// becomes a clean nil interface (avoiding the "non-nil interface
	// containing nil pointer" Go gotcha that would defeat the
	// deps.Projects != nil guard inside server.go).
	var projectsRepo *repo.ProjectsRepo
	var projectsHandlerSvc handler.ProjectsService
	if appPool != nil && adminPool != nil {
		projectsRepo = repo.NewProjectsRepoWithAdmin(appPool, adminPool)
		projectsHandlerSvc = usecase.NewProjectsService(projectsRepo, intRepo, auditWriter, nil)
	}

	// Phase 4 — Temporal worker + WorkflowService. The worker registers the
	// 9-stub RecoveryPipeline + the Activities struct (10 methods). Both
	// pools must exist (worker uses adminPool; HTTP handlers use appPool via
	// the request tx). Temporal-dial failures are tolerated in dev so a
	// missing temporal service doesn't block the rest of the surface.
	//
	// approvalSvc + approvalSignaler are exported back out of the temporal
	// init block so the HTTP server can wire the Slack interactivity decider
	// alongside the existing approval API. Both stay nil when the admin pool
	// or the temporal client is missing.
	var wfService domain.WorkflowService
	var wfRepo *repo.WorkflowRepo
	var temporalClient temporalsdk.Client // hoisted so SystemHealth can probe.
	var approvalSvc *approval.Service
	var slackDecider *approval.SlackDecider
	var approvalRepo *repo.ApprovalRepo
	var approvalSignaler *approval.SignalerService
	if appPool != nil && adminPool != nil {
		wfRepo = repo.NewWorkflowRepo(appPool, adminPool)
		tc, err := temporalplatform.Dial(temporalplatform.Config{
			HostPort:  cfg.TemporalHostPort,
			Namespace: cfg.TemporalNamespace,
		}, logger)
		if err != nil {
			if cfg.AppEnv != "dev" {
				logger.Error("temporal dial", "err", err)
				os.Exit(1)
			}
			logger.Warn("temporal disabled (dev fallback)", "err", err)
		} else {
			temporalClient = tc
			defer tc.Close()
			wfBroker := sse.New[domain.ActivityEvent]()
			stubSleep := time.Duration(cfg.WorkflowStubDurationMs) * time.Millisecond

			// Phase 5 — patch store wiring. The activity dependency tolerates
			// nil (test path), so a misconfigured PATCH_STORE in dev logs a
			// warning and keeps the worker running without object storage.
			var ps domain.PatchStore
			if got, perr := patchstore.NewFromConfig(cfg, kv); perr != nil {
				logger.Warn("patchstore disabled", "err", perr)
			} else {
				ps = got
				logger.Info("patchstore initialised", "kind", cfg.PatchStore, "endpoint", cfg.MinIOEndpoint)
			}

			// Phase 6 — validator client. Empty BaseURL is tolerated in dev
			// — the activity falls back to patch-only when no validator is
			// reachable.
			var vc recoverywf.ValidatorClient
			if cfg.ValidatorURL != "" && cfg.ValidatorToken != "" {
				vc = validatorclient.New(validatorclient.Config{
					BaseURL: cfg.ValidatorURL,
					Token:   cfg.ValidatorToken,
				})
				logger.Info("validator client initialised", "url", cfg.ValidatorURL)
			} else {
				logger.Warn("validator client disabled — VALIDATOR_URL/TOKEN missing")
			}
			// Phase 5 — agents registry + token ledger + retrieval.
			// `agents.Registry` may be nil; activities fall back to the Phase 4
			// stubs in that case (test-friendly).
			var agentRegistry *agents.Registry
			var ledger domain.TokenLedger
			if appPool != nil {
				ledgerRepo := repo.NewTokenLedgerRepo(appPool, adminPool, repo.TokenLedgerConfig{
					AllowedTokensIn:  cfg.TokenBudgetTokensIn,
					AllowedTokensOut: cfg.TokenBudgetTokensOut,
					PeriodDays:       cfg.TokenBudgetPeriodDays,
				})
				ledger = ledgerRepo

				providers, perr := llm.NewProviders(cfg, logger)
				if perr != nil {
					logger.Warn("llm providers unavailable for agents", "err", perr)
				} else {
					retStore := retrieval.New(adminPool)
					llmClient := &agents.LLMClient{
						Provider:       providers.LLM,
						Embedding:      providers.Embedding,
						Ledger:         ledger,
						Audit:          auditWriter,
						Logger:         logger,
						SchemaRetryMax: cfg.AgentSchemaRetryMax,
					}
					retClient := &agents.RetrievalClient{
						Store:      retStore,
						Embed:      providers.Embedding,
						EmbedModel: cfg.OpenAIEmbedModel,
						K:          5,
					}
					synthModel := cfg.OpenAIModelSyn
					cheapModel := cfg.OpenAIModelCheap
					if cfg.LLMProvider == "ollama" {
						synthModel = cfg.OllamaModelGen
						cheapModel = cfg.OllamaModelCode
					}
					agentRegistry = agents.NewRegistry(map[domain.AgentName]domain.Agent{
						domain.AgentNameArchitect:    architect.New(llmClient, retClient, synthModel),
						domain.AgentNameBackend:      backend.New(llmClient, retClient, cheapModel),
						domain.AgentNameQA:           qa.New(llmClient, retClient, cheapModel),
						domain.AgentNameDevOps:       devops.New(llmClient, retClient, cheapModel),
						domain.AgentNameDataEngineer: data_engineer.New(llmClient, retClient, synthModel),
					})
					logger.Info("agents registry initialised", "agents", agentRegistry.Names())
				}
			}
			acts := recoverywf.NewActivitiesFull(wfRepo, wfBroker, ps, vc, agentRegistry, ledger, stubSleep)
			// Phase 6 — Sentinel.Detect ack body needs admin-pool reads of
			// incidents_raw. Setting it post-construction keeps the existing
			// NewActivitiesFull signature stable.
			if incRepo != nil {
				acts.IncidentsAdmin = incRepo
			}

			// Phase 6 — Approval Gate wiring. ApprovalGateRoute checks
			// a.Approval != nil before running the real flow (and falls back
			// to the stub when unwired). Repo runs dual-pool because the
			// activity body executes outside any request tx; HTTP reads use
			// the per-request RLS tx via db.FromCtx.
			//
			// Notifier left nil here — Notify() becomes a no-op when no
			// channels are configured. Wiring Slack/email fan-out is the
			// notifier package's job; we keep the boot path tight.
			if adminPool != nil {
				approvalRepo = repo.NewApprovalRepo(appPool, adminPool)
				approvalSvc = approval.New(approvalRepo, nil, auditWriter)
				acts.Approval = approvalSvc

				// SlackDecider lets the Slack interactivity webhook reuse
				// the existing SignalerService for approve/reject decisions
				// — same Temporal signal, same audit chain, same row state
				// transitions. Stays nil if no signing secret is set; the
				// handler-mount guard in server.go also requires the secret.
				// SignalerService is always built (HTTP approve/reject endpoint
				// depends on it). SlackDecider wrapper only fires when Slack
				// signing secret is configured.
				approvalSignaler = approval.NewSignalerService(
					approvalRepo,
					approval.NewClientSignaler(tc),
					approvalSvc,
				)
				if len(cfg.SlackSigningSecret) > 0 {
					slackDecider = approval.NewSlackDecider(approvalRepo, approvalSignaler)
				}
			}

			// Phase 7 — Projects + Slack defaults on the activity bag. The
			// activity body short-circuits to the fixture path when Projects
			// is nil; SlackDefaultChannel is the workspace-wide fallback for
			// runs without a bound project (or projects with an empty channel).
			if projectsRepo != nil {
				acts.Projects = projectsRepo
			}
			acts.SlackDefaultChannel = cfg.SlackDefaultChannel

			wfService = adapterworkflow.New(adapterworkflow.Config{
				Repo:       wfRepo,
				Workspaces: wsRepo,
				Temporal:   tc,
				Broker:     wfBroker,
				TaskQueue:  cfg.TemporalTaskQueue,
				Logger:     logger,
			})
			go func() {
				// appCtx cancellation drives a clean worker drain on SIGTERM
				// — Start blocks until the context fires or the worker
				// fails. Without this the worker would keep polling Temporal
				// after the HTTP server has been shut down.
				if err := temporalplatform.Start(appCtx, tc,
					temporalplatform.WorkerSpec{
						TaskQueue:  cfg.TemporalTaskQueue,
						Workflows:  []any{recoverywf.RecoveryPipeline},
						Activities: []any{acts},
					}, logger); err != nil {
					logger.Error("temporal worker", "err", err)
				}
			}()
		}
	}

	// Phase 5 Stage 7 — eval harness wiring. EvalRepo + LedgerRepo always
	// constructed when the app pool is available so the HTTP surface
	// (list + budget pill) works even before any run has executed. The
	// runner is only constructed when both pools + the audit writer are
	// available — without them the POST endpoint stays unmounted.
	var evalRepo *repo.EvalRepo
	var evalRunner *usecase.EvalRunner
	var tokenLedgerRepoForHTTP *repo.TokenLedgerRepo
	if appPool != nil && adminPool != nil {
		evalRepo = repo.NewEvalRepo(appPool, adminPool)
		tokenLedgerRepoForHTTP = repo.NewTokenLedgerRepo(appPool, adminPool, repo.TokenLedgerConfig{
			AllowedTokensIn:  cfg.TokenBudgetTokensIn,
			AllowedTokensOut: cfg.TokenBudgetTokensOut,
			PeriodDays:       cfg.TokenBudgetPeriodDays,
		})
		evalRunner = &usecase.EvalRunner{
			Cfg:          cfg,
			AppPool:      appPool,
			AdminPool:    adminPool,
			EvalRepo:     evalRepo,
			LedgerRepo:   tokenLedgerRepoForHTTP,
			WorkflowRepo: wfRepo,
			AuditWriter:  auditWriter,
			Logger:       logger,
			Providers:    []string{"openai", "ollama"},
		}
		logger.Info("eval runner initialised", "providers", evalRunner.Providers)
	}

	// Operational-surfaces stage — webhook_deliveries audit log + the
	// dependency probe bundle the /v1/system-health endpoint reads. Both
	// are tolerant of missing wiring: a nil repo just causes the operator
	// log to render empty, and disabled probes surface as "disabled" in
	// the health response rather than "down".
	var webhookDeliveriesRepo *repo.WebhookDeliveriesRepo
	if appPool != nil {
		webhookDeliveriesRepo = repo.NewWebhookDeliveriesRepo(appPool, adminPool)
	}

	// Phase 8 — public-beta repos. All admin-pool-only because the read
	// paths don't carry a request tx (org_stats is /v1/me/org-stats, which
	// runs before workspace selection; invite_codes is owner-only and
	// org-scoped via the principal); nasa_tlx writes outside any RLS tx.
	// Nil-tolerant — the handler-mount guards in server.go skip wiring
	// when the repos aren't constructed (LLM-only dev path).
	var inviteCodesRepo *repo.InviteCodesRepo
	var nasaTLXRepo *repo.NASATLXRepo
	var orgStatsRepo *repo.OrgStatsRepo
	if adminPool != nil {
		inviteCodesRepo = repo.NewInviteCodesRepo(adminPool)
		orgStatsRepo = repo.NewOrgStatsRepo(adminPool)
	}
	if appPool != nil && adminPool != nil {
		nasaTLXRepo = repo.NewNASATLXRepo(appPool, adminPool)
	}

	// Phase 6 — Sentinel detector. Constructed BEFORE the HTTP server so the
	// /v1/admin/sentinel/trigger handler can call TriggerOne via the
	// SentinelTriggerer port. The Run goroutine starts further down once
	// cronCtx exists, so this only wires the in-memory struct + its router.
	//
	// Router: Phase 7 self-healing target resolution. For every emitted
	// trigger with a Sentry/Datadog/PagerDuty/GitHub fingerprint, the router
	// asks the projects repo for a project_id and stamps trigger.ProjectID
	// in-place. Nil-tolerant — when projectsRepo is unwired the router
	// becomes a no-op and every trigger fires with ProjectID="".
	var sentinelDetector *sentinel.Detector
	if cfg.SentinelEnabled && wfService != nil && incRepo != nil && wsRepo != nil && intRepo != nil {
		var sentinelRouter *sentinel.Router
		if projectsRepo != nil {
			sentinelRouter = sentinel.NewRouter(sentinel.RouterConfig{
				Matcher:  projectsRepo,
				Incident: incRepo,
				Logger:   logger,
			})
			logger.Info("sentinel router initialised", "matcher", "projects_repo")
		} else {
			logger.Warn("sentinel router disabled — projects repo unwired")
		}
		sentinelDetector = sentinel.New(sentinel.Config{
			Incidents:    incRepo,
			Workflows:    wfService,
			Workspaces:   wsRepo,
			Integrations: intRepo,
			Audit:        auditWriter,
			Router:       sentinelRouter,
			WorkflowType: "RecoveryPipeline",
			Interval:     time.Duration(cfg.SentinelPollIntervalMs) * time.Millisecond,
			Logger:       logger,
		})
	} else if cfg.SentinelEnabled {
		logger.Warn("sentinel disabled — required deps missing",
			"wfService", wfService != nil,
			"incRepo", incRepo != nil,
			"wsRepo", wsRepo != nil,
			"intRepo", intRepo != nil,
		)
	}

	// SentinelTriggerer port for the admin trigger handler. A nil concrete
	// detector becomes a clean nil interface here so the server.go guard
	// (`deps.Sentinel != nil`) actually short-circuits.
	var sentinelHandlerSvc handler.SentinelTriggerer
	if sentinelDetector != nil {
		sentinelHandlerSvc = sentinelDetector
	}

	// SlackApprovalsService port — same nil-interface dance as Sentinel.
	var slackDeciderSvc handler.SlackApprovalsService
	if slackDecider != nil {
		slackDeciderSvc = slackDecider
	}

	srv := httpserver.New(cfg, logger, httpserver.Deps{
		Pool:              adminPool,
		AppPool:           appPool,
		Auth:              authProvider,
		Audit:             auditWriter,
		AuditLister:       auditLister,
		Integrations:      registry,
		Workspaces:        wsService,
		WorkspacesRepo:    wsRepo,
		Billing:           billingProvider,
		BillingRepo:       billingRepo,
		Workflows:         wfService,
		WorkflowsRepo:     wfRepo,
		EvalRepo:          evalRepo,
		EvalRunner:        evalRunner,
		TokenLedgerRepo:   tokenLedgerRepoForHTTP,
		WebhookDeliveries: webhookDeliveriesRepo,
		Projects:          projectsHandlerSvc,
		Approvals:         approvalRepo,
		ApprovalSignaler:  approvalSignaler,
		InviteCodes:       inviteCodesRepo,
		NASATLX:           nasaTLXRepo,
		OrgStats:          orgStatsRepo,
		Sentinel:          sentinelHandlerSvc,
		SlackDecider:      slackDeciderSvc,
		SystemHealth: handler.SystemHealthDeps{
			AdminPool: adminPool,
			RedisAddr: redisAddrOrDefault(),
			Neo4j:     neo4jDriver,
			MinIO: integration.MinIOConfig{
				Endpoint:  cfg.MinIOEndpoint,
				AccessKey: cfg.MinIOAccessKey,
				SecretKey: cfg.MinIOSecretKey,
				UseSSL:    cfg.MinIOUseSSL,
			},
			Temporal: temporalClient,
		},
	})
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Cron jobs — Phase 3.5 usage ticker + invoice roller. Bound to appCtx
	// so SIGTERM cancels them in lockstep with the rest of the long-running
	// goroutines (Temporal worker, Sentinel detector).
	if wsRepo != nil && billingRepo != nil {
		usageTicker := &usecase.UsageTicker{
			Workspaces: wsRepo,
			Billing:    billingRepo,
			Interval:   time.Duration(cfg.UsageTickSeconds) * time.Second,
			PriceCents: 10.0,
		}
		invoiceRoller := &usecase.InvoiceRoller{Billing: billingRepo}
		cron.Start(appCtx, logger, []cron.Job{
			{Name: "usage_ticker", Interval: usageTicker.Interval, Run: usageTicker.Run},
			{Name: "invoice_roller", Interval: 24 * time.Hour, Run: invoiceRoller.Run},
		})
		logger.Info("cron started", "usage_tick_seconds", cfg.UsageTickSeconds)
	}

	// Phase 6 — Sentinel detector goroutine. Polls incidents_raw + triggers a
	// RecoveryPipeline run per detected fatal/spike row. The struct itself
	// is constructed above (before the HTTP server) so the admin trigger
	// handler can reuse the same instance; here we just start the polling
	// goroutine on appCtx so a SIGTERM cancels it cleanly.
	if sentinelDetector != nil {
		go sentinelDetector.Run(appCtx)
		logger.Info("sentinel started", "interval_ms", cfg.SentinelPollIntervalMs)
	}

	// Signal-driven graceful shutdown.
	//
	// On SIGINT/SIGTERM:
	//   1. Cancel appCtx so the Temporal worker, Sentinel detector, and
	//      cron loop unwind. The Temporal SDK drains in-flight activities
	//      bounded by its own WorkerOptions timeouts; the polling loops
	//      observe ctx.Done() at the top of each tick.
	//   2. Call httpServer.Shutdown with a 30s deadline. New connections
	//      are refused immediately; in-flight requests are given that
	//      window to complete before we return. http.ErrServerClosed from
	//      the ListenAndServe call below is treated as a clean exit.
	//
	// All defers (db pool Close, otel shutdown, temporal client Close,
	// neo4j driver Close) fire after the shutdown returns, so the order
	// is: signal → background goroutines drain → HTTP drain → deferred
	// resource cleanup → process exit.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-quit
		logger.Info("shutdown signal received", "signal", sig.String())
		// Cancel long-running goroutines first so they stop scheduling
		// new work while the HTTP layer drains.
		appCancel()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("http shutdown failed", "err", err)
		}
	}()

	logger.Info("control-plane listening", "addr", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http listen failed", "err", err)
		os.Exit(1)
	}
	logger.Info("control-plane shutdown complete")
}

// mustPool returns a connected pgx pool, or nil + a warning if the URL is
// empty (dev convenience). On a real connection error in non-dev we fail
// fast. envName is used only for logging so operators can tell which pool
// failed at boot.
func mustPool(ctx context.Context, envName, url string, cfg config.Config, logger *slog.Logger) *pgxpool.Pool {
	if url == "" {
		logger.Warn(envName + " not set — auth/RLS features depending on this pool will fail at runtime")
		return nil
	}
	p, err := db.New(ctx, url)
	if err != nil {
		if cfg.AppEnv == "dev" {
			logger.Warn("db pool init failed in dev — continuing without it", "pool", envName, "err", err)
			return nil
		}
		logger.Error("db pool", "pool", envName, "err", err)
		os.Exit(1)
	}
	return p
}
