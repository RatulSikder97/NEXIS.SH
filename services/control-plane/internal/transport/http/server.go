// Package http wires the Chi router. Handlers live in handler/, middleware in
// middleware/. Real LLM diag route is wired in TC10; for now only /healthz.
package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
)

func New(cfg config.Config, logger *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(60 * time.Second))

	r.Get("/healthz", handler.Healthz("control-plane"))
	// /v1/_diag/llm wired in TC10

	_ = cfg
	_ = logger
	return r
}
