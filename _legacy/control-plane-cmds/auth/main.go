// auth — Clerk-synced identity + RBAC + API keys for NEXIS.
//
// Owns: users, orgs, org_members, sessions, api_keys, audit of auth events.
// Talks to: Clerk (webhook), Postgres.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"nexis/backend/internal/platform/pg"
	"nexis/backend/internal/platform/probes"
	"nexis/backend/internal/services/auth/config"
	"nexis/backend/internal/services/auth/handler"
	"nexis/backend/internal/services/auth/service"
	"nexis/backend/internal/services/auth/store"
	pgrepo "nexis/backend/internal/services/auth/store/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	checks := []probes.Check{}
	var svc *service.Service

	if cfg.DatabaseURL != "" {
		pool, err := pg.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			slog.Error("db connect failed", "err", err)
			os.Exit(1)
		}
		defer pool.Close()
		checks = append(checks, pool.HealthCheck)
		repo := pgrepo.New(pool)
		svc = service.New(repo, service.Options{
			ClerkWebhookSecret:      cfg.ClerkWebhookSecret,
			APIKeyDefaultExpiryDays: cfg.APIKeyDefaultExpiry,
			Environment:             cfg.Environment,
		})
	} else {
		slog.Warn("DATABASE_URL not set — running in degraded mode (DB writes will fail)")
		svc = service.New(store.Stub{}, service.Options{
			ClerkWebhookSecret:      cfg.ClerkWebhookSecret,
			APIKeyDefaultExpiryDays: cfg.APIKeyDefaultExpiry,
			Environment:             cfg.Environment,
		})
	}

	h := handler.New(svc, checks...)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("auth service listening", "addr", srv.Addr, "env", cfg.Environment)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			cancel()
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stop:
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
}
