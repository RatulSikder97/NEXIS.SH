// Package main is the gitops service entrypoint. Loads config, dials
// Postgres + the GitHub App, wires the chi router, and serves until
// SIGTERM.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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

// runHealthcheck is the body of the --healthcheck subcommand. It dials
// http://127.0.0.1:$PORT/healthz with a short timeout and exits 0 on a 2xx
// response, 1 otherwise. Designed for `HEALTHCHECK CMD ["/app/server",
// "--healthcheck"]` on the distroless image (which has no curl/wget). The
// function never returns — it always calls os.Exit. Mirrors the same shape
// as services/control-plane/cmd/server/main.go::runHealthcheck.
func runHealthcheck() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
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
	hcFlags := flag.NewFlagSet("gitops", flag.ContinueOnError)
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
