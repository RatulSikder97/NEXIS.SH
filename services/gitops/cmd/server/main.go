// Package main is the gitops service entrypoint. Loads config, dials
// Postgres + the GitHub App, wires the chi router, and serves until
// SIGTERM.
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

	ghAdapter "github.com/nexis-eco/nexis/services/gitops/internal/adapter/github"
	"github.com/nexis-eco/nexis/services/gitops/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/gitops/internal/platform/config"
	httpsrv "github.com/nexis-eco/nexis/services/gitops/internal/transport/http"
	"github.com/nexis-eco/nexis/services/gitops/internal/usecase"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	cfg := config.Load()

	if cfg.GitOpsToken == "" {
		logger.Error("GITOPS_AUTH_TOKEN missing — refusing to start")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Postgres pool — gitops runs under nexis_gitops (see migration 0018).
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("postgres dial failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	integrations := repo.NewIntegrationsRepo(pool)
	auditSecret := []byte(os.Getenv("AUDIT_SECRET"))
	if len(auditSecret) == 0 {
		logger.Warn("AUDIT_SECRET empty — audit rows will be rejected by AppendPROpened")
	}
	auditRepo := repo.NewAuditRepo(pool, auditSecret)

	builder, err := ghAdapter.NewBuilder(cfg.GitHubAppID, cfg.GitHubAppPrivateKeyPath, integrations)
	if err != nil {
		logger.Error("github builder", "err", err)
		os.Exit(1)
	}
	clientFactory := &ghClientFactory{builder: builder}

	uc := usecase.NewOpenPRUsecase(clientFactory, auditRepo)

	handler := httpsrv.New(httpsrv.Deps{
		OpenPR:    uc,
		AuthToken: cfg.GitOpsToken,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("gitops listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server crashed", "err", err)
			os.Exit(1)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	logger.Info("gitops shutdown signal received")
	shutdownCtx, sCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "err", err)
	}
}

// ghClientFactory adapts ClientBuilder.ForOrg → the gh.Client interface the
// usecase depends on. Keeps the cmd-side wiring tiny and avoids pushing the
// go-github type into the usecase package.
type ghClientFactory struct {
	builder *ghAdapter.ClientBuilder
}

func (f *ghClientFactory) ForOrg(ctx context.Context, orgID string) (ghAdapter.Client, error) {
	c, err := f.builder.ForOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return ghAdapter.NewClient(c), nil
}
