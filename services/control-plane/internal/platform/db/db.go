// Package db wires the pgx connection pool used by every adapter that needs a
// real Postgres handle. Keeping the pool builder here keeps adapter packages
// free of pool-tuning trivia.
package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// New parses the libpq-style connection URL, applies sensible pool defaults,
// and returns a connected pool. Caller is responsible for calling Close().
func New(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 20
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	return pgxpool.NewWithConfig(ctx, cfg)
}
