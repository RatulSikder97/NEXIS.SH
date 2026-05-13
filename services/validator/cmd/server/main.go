// Package main is the validator service entrypoint.
//
// Phase 4 implementation: POST /v1/validate accepts a patch + repo SHA and
// spawns an isolated `docker run --rm --network=none --read-only` container
// against the fixture image, returning the pytest summary + logs.
//
// WARNING: this service mounts the host docker socket. Never expose it
// outside the dev compose network. Phase 7 swaps the runner for Modal.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/nexis-eco/nexis/services/validator/internal/runner"
	httphandler "github.com/nexis-eco/nexis/services/validator/internal/transport/http/handler"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	port := envOr("PORT", "8081")
	token := envOr("VALIDATOR_TOKEN", "")
	image := envOr("NEXIS_VALIDATOR_IMAGE", "nexis/validator-fixture:latest")
	if token == "" {
		logger.Error("VALIDATOR_TOKEN not set; refusing to start")
		os.Exit(1)
	}

	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID, chiMiddleware.Recoverer)

	r.Get("/healthz", httphandler.Healthz("validator"))
	r.Post("/v1/validate", httphandler.Validate(runner.NewDocker(image, logger), token, logger))

	logger.Info("validator listening", "port", port, "image", image)
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
