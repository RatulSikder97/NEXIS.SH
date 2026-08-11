// Package main is the deploy-engine service entrypoint.
//
// POST /v1/deploy clones a GitHub repo, detects (or reuses) a Dockerfile,
// builds a preview image, runs it with resource limits, and health-polls it
// until the app answers HTTP. The control-plane is the system of record —
// this service keeps only an in-memory map of what it started.
//
// WARNING: this service drives the host docker socket. Never expose it
// outside the dev compose network.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/nexis-eco/nexis/services/deploy-engine/internal/deploy"
	httphandler "github.com/nexis-eco/nexis/services/deploy-engine/internal/transport/http/handler"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	port := envOr("PORT", "8091")
	token := envOr("DEPLOY_ENGINE_TOKEN", "")
	// Native runs poll localhost; inside compose (published ports land on
	// the host, not this container's netns) set
	// DEPLOY_HEALTH_HOST=host.docker.internal.
	healthHost := envOr("DEPLOY_HEALTH_HOST", "localhost")
	if token == "" {
		logger.Error("DEPLOY_ENGINE_TOKEN not set; refusing to start")
		os.Exit(1)
	}

	engine := deploy.NewEngine(healthHost, logger)
	store := deploy.NewStore()

	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID, chiMiddleware.Recoverer)

	r.Get("/healthz", httphandler.Healthz("deploy-engine"))
	r.Post("/v1/deploy", httphandler.Deploy(engine, store, token, logger))
	r.Get("/v1/deploy/{deployment_id}", httphandler.GetDeployment(store, token))
	r.Post("/v1/deploy/{deployment_id}/stop", httphandler.StopDeployment(engine, store, token, logger))

	logger.Info("deploy-engine listening", "port", port, "health_host", healthHost)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		logger.Error("listen", "err", err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
