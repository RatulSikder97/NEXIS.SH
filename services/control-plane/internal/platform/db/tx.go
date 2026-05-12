// Package db also exposes the request-scoped transaction plumbing shared by
// the RLS middleware (transport) and the pgx-backed adapters (adapter). The
// helpers live in the platform layer because both higher-level packages may
// depend on platform, but adapters must NOT import transport per .arch.yaml.
//
// Flow:
//
//  1. The RLS middleware opens a pgx.Tx for the request, binds the tenant
//     GUC via `SELECT set_config('app.current_org_id', $1, true)` (tx-local),
//     and stashes the tx in ctx via WithTx.
//  2. pgx-backed adapters call FromCtx(ctx, pool) to obtain a Querier —
//     either the in-flight tx or the supplied fallback pool. Queries against
//     tenant tables therefore see RLS enforcement automatically.
//  3. On response completion the middleware commits (2xx/3xx) or rolls back
//     (everything else).
package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is the minimal pgx surface used by adapter code that may run either
// inside a request-scoped transaction or against the bare pool. Both pgx.Tx
// and *pgxpool.Pool satisfy this interface without modification (matching the
// pgx/v5 method signatures verbatim).
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ctxKeyTx is the unexported context key used by WithTx/TxFrom. Keeping it
// unexported makes the tx un-spoofable from outside this package.
type ctxKeyTx struct{}

// WithTx returns a context carrying tx. Intended for the RLS middleware to
// thread the per-request transaction down to adapters.
func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, ctxKeyTx{}, tx)
}

// TxFrom returns the pgx.Tx attached to ctx by WithTx, plus a boolean
// indicating presence. Adapters should call Querier(ctx, pool) rather than
// using this directly unless they need the raw tx for nested operations.
func TxFrom(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(ctxKeyTx{}).(pgx.Tx)
	return tx, ok
}

// FromCtx returns the per-request tx if one is attached, otherwise the
// supplied fallback. Both implementations satisfy Querier so adapter code can
// stay agnostic. The fallback must not be nil — callers should pass the
// adapter's owning pool.
func FromCtx(ctx context.Context, fallback Querier) Querier {
	if tx, ok := TxFrom(ctx); ok {
		return tx
	}
	return fallback
}
