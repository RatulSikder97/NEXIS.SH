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

	// Initialise the application-role pool. Missing URL is tolerated in dev
	// so `go run ./cmd/server` still boots for the LLM-only diag surface
	// from Phase 1; real connection errors are fatal in non-dev envs.
	pool := mustPool(ctx, cfg, logger)
	if pool != nil {
		defer pool.Close()
	}

	authProvider, err := auth.NewFromConfig(cfg, pool)
	if err != nil {
		logger.Error("auth provider", "err", err)
		os.Exit(1)
	}
	logger.Info("auth provider initialised", "name", authProvider.Name())

	srv := httpserver.New(cfg, logger, httpserver.Deps{Pool: pool, Auth: authProvider})
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

// mustPool returns a connected pgx pool, or nil + a warning if no URL is set
// (dev convenience). On a real connection error in non-dev we fail fast.
func mustPool(ctx context.Context, cfg config.Config, logger *slog.Logger) *pgxpool.Pool {
	if cfg.DatabaseURLApp == "" {
		logger.Warn("DATABASE_URL_APP not set — auth routes will fail at runtime")
		return nil
	}
	p, err := db.New(ctx, cfg.DatabaseURLApp)
	if err != nil {
		if cfg.AppEnv == "dev" {
			logger.Warn("db pool init failed in dev — continuing without it", "err", err)
			return nil
		}
		logger.Error("db pool", "err", err)
		os.Exit(1)
	}
	return p
}
