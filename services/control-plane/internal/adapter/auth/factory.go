// Package auth wires the configured AuthProvider implementation. Selecting the
// provider lives here so cmd/server stays thin and the local/workos packages
// don't need to know about each other.
package auth

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/workos"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// NewFromConfig returns the AuthProvider matching cfg.AuthProvider. The pool
// is only consumed by local; the workos stub ignores it. Errors are returned
// only for unknown provider names — provider construction itself is
// infallible today.
func NewFromConfig(cfg config.Config, pool *pgxpool.Pool) (domain.AuthProvider, error) {
	switch cfg.AuthProvider {
	case "local", "":
		return buildLocal(cfg, pool), nil
	case "workos":
		if cfg.AllowStubWorkOS {
			slog.Warn("auth: AUTH_PROVIDER=workos with ALLOW_STUB_WORKOS=1 — substituting local provider")
			return buildLocal(cfg, pool), nil
		}
		return workos.New(workos.Config{}), nil
	default:
		return nil, fmt.Errorf("unknown AUTH_PROVIDER %q (want local|workos)", cfg.AuthProvider)
	}
}

func buildLocal(cfg config.Config, pool *pgxpool.Pool) *local.Provider {
	var store local.Store
	if pool != nil {
		store = local.NewPGStore(pool)
	}
	return local.New(local.Config{
		Store:         store,
		SessionSecret: []byte(cfg.SessionSecret),
		Mailer: &local.SMTPMailer{
			Host: cfg.SMTPHost,
			Port: cfg.SMTPPort,
			From: cfg.SMTPFrom,
		},
		BaseURL: cfg.AppBaseURL,
	})
}
