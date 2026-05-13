// Package main is the control-plane HTTP server entrypoint.
package main

import (
	"context"
	"encoding/base64"
	"errors"
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

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config.Load()
	logger.Info("control-plane starting", "port", cfg.Port, "env", cfg.AppEnv, "auth_provider", cfg.AuthProvider)

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

	// Phase 4 — Temporal worker + WorkflowService. The worker registers the
	// 9-stub RecoveryPipeline + the Activities struct (10 methods). Both
	// pools must exist (worker uses adminPool; HTTP handlers use appPool via
	// the request tx). Temporal-dial failures are tolerated in dev so a
	// missing temporal service doesn't block the rest of the surface.
	var wfService domain.WorkflowService
	var wfRepo *repo.WorkflowRepo
	var temporalClient temporalsdk.Client // hoisted so SystemHealth can probe.
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

			wfService = adapterworkflow.New(adapterworkflow.Config{
				Repo:       wfRepo,
				Workspaces: wsRepo,
				Temporal:   tc,
				Broker:     wfBroker,
				TaskQueue:  cfg.TemporalTaskQueue,
				Logger:     logger,
			})
			go func() {
				if err := temporalplatform.Start(context.Background(), tc,
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

	// Phase 7 — Projects (self-healing targets). Repo runs dual-pool so
	// Sentinel's cross-tenant MatchByFingerprint sweeps via admin; the
	// usecase enforces per-tier caps + the GitHub binding rule and audits
	// every mutation under project.*. Wired only when both pools exist
	// (the dev LLM-only path keeps booting without it).
	//
	// projectsHandlerSvc is the handler-facing interface form. We keep the
	// concrete pointer + the interface separate so a nil concrete value
	// becomes a clean nil interface (avoiding the "non-nil interface
	// containing nil pointer" Go gotcha that would defeat the
	// deps.Projects != nil guard inside server.go).
	var projectsHandlerSvc handler.ProjectsService
	if appPool != nil && adminPool != nil {
		projectsRepo := repo.NewProjectsRepoWithAdmin(appPool, adminPool)
		projectsHandlerSvc = usecase.NewProjectsService(projectsRepo, intRepo, auditWriter, nil)
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

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()
	logger.Info("control-plane listening", "addr", httpServer.Addr)

	// Cron jobs — Phase 3.5 usage ticker + invoice roller. Run only when both
	// repos exist. Cancelling cronCtx (on SIGTERM below) terminates the
	// per-job goroutines cleanly.
	cronCtx, cancelCrons := context.WithCancel(context.Background())
	defer cancelCrons()
	if wsRepo != nil && billingRepo != nil {
		usageTicker := &usecase.UsageTicker{
			Workspaces: wsRepo,
			Billing:    billingRepo,
			Interval:   time.Duration(cfg.UsageTickSeconds) * time.Second,
			PriceCents: 10.0,
		}
		invoiceRoller := &usecase.InvoiceRoller{Billing: billingRepo}
		cron.Start(cronCtx, logger, []cron.Job{
			{Name: "usage_ticker", Interval: usageTicker.Interval, Run: usageTicker.Run},
			{Name: "invoice_roller", Interval: 24 * time.Hour, Run: invoiceRoller.Run},
		})
		logger.Info("cron started", "usage_tick_seconds", cfg.UsageTickSeconds)
	}

	// Phase 6 — Sentinel detector goroutine. Polls incidents_raw + triggers a
	// RecoveryPipeline run per detected fatal/spike row. Shares cronCtx so a
	// SIGTERM cancels it alongside the cron jobs.
	//
	// All deps must be available — the dev fallback path (LLM-only boot) has
	// no workflowService / incidents repo, so the goroutine simply doesn't
	// start. Toggle via SENTINEL_ENABLED=0 to skip even when deps exist.
	if cfg.SentinelEnabled && wfService != nil && incRepo != nil && wsRepo != nil && intRepo != nil {
		det := sentinel.New(sentinel.Config{
			Incidents:    incRepo,
			Workflows:    wfService,
			Workspaces:   wsRepo,
			Integrations: intRepo,
			Audit:        auditWriter,
			WorkflowType: "RecoveryPipeline",
			Interval:     time.Duration(cfg.SentinelPollIntervalMs) * time.Millisecond,
			Logger:       logger,
		})
		go det.Run(cronCtx)
		logger.Info("sentinel started", "interval_ms", cfg.SentinelPollIntervalMs)
	} else if cfg.SentinelEnabled {
		logger.Warn("sentinel disabled — required deps missing",
			"wfService", wfService != nil,
			"incRepo", incRepo != nil,
			"wsRepo", wsRepo != nil,
			"intRepo", intRepo != nil,
		)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	logger.Info("control-plane shutting down")
	cancelCrons()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
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
