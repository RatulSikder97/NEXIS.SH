// Package main is the control-plane HTTP server entrypoint.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config.Load()
	logger.Info("control-plane starting", "port", cfg.Port, "env", cfg.AppEnv, "auth_provider", cfg.AuthProvider)

	ctx := context.Background()

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

	srv := httpserver.New(cfg, logger, httpserver.Deps{
		Pool:    adminPool,
		AppPool: appPool,
		Auth:    authProvider,
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

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	logger.Info("control-plane shutting down")

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
