package db

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ----- Test doubles -----------------------------------------------------

// fakeTx is a minimal pgx.Tx stub. Tests inject it into ctx via WithTx and
// observe whether FromCtx returns the tx (rather than the fallback). Method
// bodies that aren't exercised return errors to avoid masking accidental
// dependencies — if a code path under test starts calling a new method, the
// test fails loudly instead of returning a misleading zero value.
type fakeTx struct {
	id string
}

// compile-time conformance for pgx.Tx
var _ pgx.Tx = (*fakeTx)(nil)

func (f *fakeTx) Begin(_ context.Context) (pgx.Tx, error) {
	return nil, errors.New("fakeTx.Begin not used in these tests")
}
func (f *fakeTx) Commit(_ context.Context) error   { return nil }
func (f *fakeTx) Rollback(_ context.Context) error { return nil }
func (f *fakeTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("fakeTx.CopyFrom not used")
}
func (f *fakeTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	return nil
}
func (f *fakeTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (f *fakeTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("fakeTx.Prepare not used")
}
func (f *fakeTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (f *fakeTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, errors.New("fakeTx.Query not used")
}
func (f *fakeTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return nil
}
func (f *fakeTx) Conn() *pgx.Conn { return nil }

// fakeQuerier is the non-tx fallback returned by FromCtx when no tx is bound.
// Its identity is what the tests compare against — they don't actually call
// Exec/Query, they just verify that FromCtx returns this instance when ctx has
// no tx attached.
type fakeQuerier struct{ id string }

func (q *fakeQuerier) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (q *fakeQuerier) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, errors.New("Query not used")
}
func (q *fakeQuerier) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return nil
}

// compile-time conformance for db.Querier
var _ Querier = (*fakeQuerier)(nil)

// ----- Tests -------------------------------------------------------------

// TestWithTx_TxFrom_RoundTrip confirms a tx stashed via WithTx is retrievable
// via TxFrom. This is the contract every adapter relies on — break it and
// every RLS-aware repo silently falls back to the admin pool.
func TestWithTx_TxFrom_RoundTrip(t *testing.T) {
	tx := &fakeTx{id: "tx-1"}
	ctx := WithTx(context.Background(), tx)

	got, ok := TxFrom(ctx)
	if !ok {
		t.Fatalf("TxFrom: expected ok=true after WithTx")
	}
	if got != tx {
		t.Fatalf("TxFrom: returned a different tx; want %p got %p", tx, got)
	}
}

// TestTxFrom_EmptyContextReportsAbsent — TxFrom on a plain ctx must return
// (nil, false). This is the signal adapters use to decide between "use the
// per-request tx" and "fall back to the pool".
func TestTxFrom_EmptyContextReportsAbsent(t *testing.T) {
	tx, ok := TxFrom(context.Background())
	if ok {
		t.Fatalf("TxFrom on empty ctx: expected ok=false, got tx=%+v", tx)
	}
	if tx != nil {
		t.Errorf("TxFrom on empty ctx: expected nil tx, got %+v", tx)
	}
}

// TestFromCtx_ReturnsTxWhenPresent — the core dispatch helper every
// pgx-backed adapter uses. When a tx is in ctx, FromCtx returns it; the
// fallback Querier is never consulted (its identity differs from the tx).
func TestFromCtx_ReturnsTxWhenPresent(t *testing.T) {
	tx := &fakeTx{id: "in-tx"}
	fallback := &fakeQuerier{id: "fallback"}
	ctx := WithTx(context.Background(), tx)

	got := FromCtx(ctx, fallback)
	if got == Querier(fallback) {
		t.Fatalf("FromCtx returned fallback when tx was present")
	}
	// The returned Querier should be the tx itself (pgx.Tx satisfies Querier).
	if got != Querier(tx) {
		t.Errorf("FromCtx: expected the bound tx, got %T (%v)", got, got)
	}
}

// TestFromCtx_ReturnsFallbackWhenNoTx — when no tx is bound, FromCtx returns
// the provided fallback verbatim. Adapters pass their owning pool as fallback,
// so a missing tx means "run against the pool" — which is the correct
// behaviour for system paths (cron, signup, etc.) that don't have a principal.
func TestFromCtx_ReturnsFallbackWhenNoTx(t *testing.T) {
	fallback := &fakeQuerier{id: "fallback"}
	got := FromCtx(context.Background(), fallback)
	if got != Querier(fallback) {
		t.Fatalf("FromCtx without tx must return fallback; got %T (%v)", got, got)
	}
}

// TestWithTx_HonorsContextCancellation — WithTx itself is a value-store; it
// does not cancel anything. But the ctx it returns must still carry the parent
// ctx's Done channel and cancellation. This test confirms that a parent ctx
// cancellation propagates through WithTx so adapters can observe it.
func TestWithTx_HonorsContextCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	tx := &fakeTx{id: "tx"}
	ctx := WithTx(parent, tx)

	// Before cancel: Done channel should not be closed.
	select {
	case <-ctx.Done():
		t.Fatalf("ctx.Done closed before parent was cancelled")
	default:
	}

	cancel()

	// After cancel: Done channel must close and Err must report Cancelled.
	select {
	case <-ctx.Done():
	default:
		t.Fatalf("ctx.Done did not close after parent cancellation")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("ctx.Err: got %v want context.Canceled", ctx.Err())
	}

	// The tx is still retrievable even after the ctx is cancelled — WithTx is
	// a pure value store, cancellation does not strip the tx from ctx. This
	// matters because adapters check TxFrom early and then do their own ctx
	// observation downstream.
	got, ok := TxFrom(ctx)
	if !ok {
		t.Errorf("tx should still be retrievable after cancellation")
	}
	if got != tx {
		t.Errorf("retrieved tx mismatch after cancellation")
	}
}

// TestFromCtx_FallbackNilIsCallerError — the docstring warns "fallback must
// not be nil" but the implementation does not actively guard. We don't want
// to lock down panic-on-nil as part of the contract (that would be a behavior
// change), but we do want a regression test that asserts the documented
// contract is observed by callers: when a tx IS in ctx, FromCtx does NOT
// consult the fallback — so nil-fallback callers are safe IF and ONLY IF they
// always pass a ctx with a tx.
func TestFromCtx_FallbackNilSafeWhenTxBound(t *testing.T) {
	tx := &fakeTx{id: "tx-only"}
	ctx := WithTx(context.Background(), tx)

	// Pass nil fallback. Because the tx is in ctx, FromCtx returns it and
	// never touches the fallback — so no panic.
	got := FromCtx(ctx, nil)
	if got != Querier(tx) {
		t.Fatalf("FromCtx must return the tx; got %T", got)
	}
}
