// Package auth wires the configured AuthProvider implementation. Selecting the
// provider lives here so cmd/server stays thin and the local/workos packages
// don't need to know about each other.
package auth

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// NewFromConfig returns the AuthProvider matching cfg.AuthProvider. The pool
// is only consumed by local; the workos stub ignores it.
//
// Phase 2 fail-fast (SQA §9): AUTH_PROVIDER=workos must not silently fall
// back to the stub in production. When ALLOW_STUB_WORKOS=1 we keep the
// dev-convenience fallback (with a loud warning); otherwise we currently
// return an error because a real WorkOS provider isn't wired yet — better
// to refuse to boot than to start an "auth=workos" server that's actually
// running the local provider.
func NewFromConfig(cfg config.Config, pool *pgxpool.Pool) (domain.AuthProvider, error) {
	switch cfg.AuthProvider {
	case "local", "":
		return buildLocal(cfg, pool), nil
	case "workos":
		if cfg.AllowStubWorkOS {
			slog.Warn("auth: AUTH_PROVIDER=workos with ALLOW_STUB_WORKOS=1 — substituting local provider")
			return buildLocal(cfg, pool), nil
		}
		return nil, fmt.Errorf("AUTH_PROVIDER=workos requires ALLOW_STUB_WORKOS=1 in this build (real WorkOS provider not yet wired); refuse to boot rather than silently fall back to local")
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
