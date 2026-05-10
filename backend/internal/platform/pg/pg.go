// Package pg wraps pgx with NEXIS-standard helpers:
//   - pool init from DATABASE_URL
//   - per-request RLS context (SET LOCAL app.current_org_id / current_user_id)
//   - tx helpers with deferred rollback
package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is the global connection pool wrapper.
type Pool struct {
	*pgxpool.Pool
}

// Open opens the pool with sensible defaults.
func Open(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("pg: parse dsn: %w", err)
	}
	cfg.MaxConns = 16
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pg: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pg: ping: %w", err)
	}
	return &Pool{Pool: pool}, nil
}

// HealthCheck satisfies probes.Check.
func (p *Pool) HealthCheck(ctx context.Context) error {
	if p == nil || p.Pool == nil {
		return errors.New("pg: pool not initialised")
	}
	return p.Pool.Ping(ctx)
}

// WithTenant runs fn inside a transaction with RLS context bound to the org+user.
// All NEXIS DB writes must use this OR a fresh tx with SetTenant called explicitly.
func (p *Pool) WithTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	tx, err := p.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op if tx committed

	if err := SetTenant(ctx, tx, orgID, userID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetTenant binds the RLS session vars to the given org/user.
// Use this if you're managing tx lifecycle yourself.
func SetTenant(ctx context.Context, tx pgx.Tx, orgID, userID string) error {
	if orgID != "" {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", orgID); err != nil {
			return fmt.Errorf("pg: set org_id: %w", err)
		}
	}
	if userID != "" {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.current_user_id', $1, true)", userID); err != nil {
			return fmt.Errorf("pg: set user_id: %w", err)
		}
	}
	return nil
}
