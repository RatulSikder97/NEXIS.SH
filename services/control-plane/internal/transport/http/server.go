// Package http wires the Chi router. Handlers live in handler/, middleware in
// middleware/. Phase 2 introduces Deps so the server can carry adapter
// instances (db pool, auth provider) into handlers without re-running the
// factory per request.
package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/llm"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
)

// Deps carries pre-built adapter instances into the router. Auth-protected
// routes wire in Stage 2; for now only the LLM diag route reads cfg directly.
type Deps struct {
	Pool *pgxpool.Pool
	Auth domain.AuthProvider
}

func New(cfg config.Config, logger *slog.Logger, deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(60 * time.Second))

	r.Get("/healthz", handler.Healthz("control-plane"))

	if provider, err := llm.NewFromConfig(cfg); err == nil {
		r.Get("/v1/_diag/llm", handler.LLMDiag(provider))
	} else {
		logger.Warn("llm provider not configured", "err", err)
	}

	_ = deps // wired into auth routes in Stage 2

	return r
}
