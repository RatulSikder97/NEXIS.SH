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
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/audit"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/billing"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/keyvault"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/workspace"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/cron"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
	otelplatform "github.com/nexis-eco/nexis/services/control-plane/internal/platform/otel"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/sse"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
	"github.com/nexis-eco/nexis/services/control-plane/internal/usecase"
)

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
	var registry *integration.Registry
	if appPool != nil {
		intRepo := repo.NewIntegrationsRepo(appPool)
		incRepo := repo.NewIncidentsRepo(appPool)
		registry = integration.NewRegistry(integration.Deps{
			Repo:                intRepo,
			KV:                  kv,
			IncidentSink:        incRepo,
			GitHubDefaultSecret: []byte(cfg.GitHubDefaultWebhookSecret),
		})
	} else {
		logger.Warn("integration registry disabled — DATABASE_URL_APP missing")
	}

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

	srv := httpserver.New(cfg, logger, httpserver.Deps{
		Pool:           adminPool,
		AppPool:        appPool,
		Auth:           authProvider,
		Audit:          auditWriter,
		AuditLister:    auditLister,
		Integrations:   registry,
		Workspaces:     wsService,
		WorkspacesRepo: wsRepo,
		Billing:        billingProvider,
		BillingRepo:    billingRepo,
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
