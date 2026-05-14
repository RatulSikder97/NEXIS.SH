package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRLS_MissingPrincipalReturns401 covers the only branch of RLS that does
// not touch the pool — when there is no principal in ctx, RLS must reject the
// request with a JSON 401 BEFORE calling BeginTx. The deeper tx-lifecycle
// branches (set_config bind, commit-on-2xx, rollback-on-handler-error) are
// exercised by the integration test in tests/integration/rls_test.go because
// RLS depends on a concrete *pgxpool.Pool with no interface seam — unit-mocking
// would require changing production code, which the task prohibits.
func TestRLS_MissingPrincipalReturns401(t *testing.T) {
	// Pool is built with an unreachable URL — it never connects because the
	// missing-principal branch returns before BeginTx is called. We still
	// need a non-nil *pgxpool.Pool because RLS expects one; pgxpool.New is
	// lazy and won't dial until first use.
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/x")
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	defer pool.Close()

	called := false
	h := RLS(pool)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if called {
		t.Fatalf("handler must not run when principal is absent")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", rr.Code)
	}
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type: got %q want application/json", rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Body.String(), "unauthorized") {
		t.Errorf("body must mention unauthorized: %q", rr.Body.String())
	}
}

// TestStatusRecorder_RemembersWriteHeader exercises the inner statusRecorder
// helper — it's package-internal and is the bookkeeping that drives the
// commit-vs-rollback decision in RLS. Pure unit, no DB.
func TestStatusRecorder_RemembersWriteHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: rr, status: http.StatusOK}

	rec.WriteHeader(http.StatusBadRequest)
	if rec.status != http.StatusBadRequest {
		t.Fatalf("status not remembered: got %d want 400", rec.status)
	}

	// Second WriteHeader call must be a no-op for status tracking (mirrors
	// the net/http behaviour where a second WriteHeader is ignored).
	rec.WriteHeader(http.StatusInternalServerError)
	if rec.status != http.StatusBadRequest {
		t.Errorf("status overwritten by second WriteHeader: got %d want 400", rec.status)
	}
}

// TestStatusRecorder_WriteEmitsImplicit200 ensures an unwrapped Write does NOT
// silently leave status at zero — http.ResponseWriter's contract says an
// implicit 200 is sent on first Write; statusRecorder must mirror that so the
// RLS commit-on-2xx decision still triggers when the handler streams without
// calling WriteHeader.
func TestStatusRecorder_WriteEmitsImplicit200(t *testing.T) {
	rr := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: rr, status: http.StatusOK}

	n, err := rec.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != 5 {
		t.Errorf("byte count: got %d want 5", n)
	}
	if !rec.wroteHeader {
		t.Fatalf("wroteHeader should be set after first Write")
	}
	if rec.status != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.status)
	}
}

// TestStatusRecorder_UnwrapExposesUnderlying confirms the Unwrap method —
// SSE streaming relies on http.NewResponseController being able to reach the
// underlying Flusher through the wrapper. If Unwrap drifts to return nil or
// the wrong instance, every SSE endpoint silently breaks.
func TestStatusRecorder_UnwrapExposesUnderlying(t *testing.T) {
	rr := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: rr, status: http.StatusOK}

	unwrapped := rec.Unwrap()
	if unwrapped == nil {
		t.Fatalf("Unwrap returned nil")
	}
	if unwrapped != http.ResponseWriter(rr) {
		t.Fatalf("Unwrap returned a different ResponseWriter")
	}
}

// TestWriteJSONError_ShapeMatchesAPIConvention pins the error envelope so
// downstream handlers / tests can rely on the shape: JSON {"error": "..."},
// Content-Type application/json, the requested status code.
func TestWriteJSONError_ShapeMatchesAPIConvention(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSONError(rr, http.StatusTeapot, "i am a teapot")

	if rr.Code != http.StatusTeapot {
		t.Fatalf("status: got %d want 418", rr.Code)
	}
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type: got %q want application/json", rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Body.String(), `"error"`) {
		t.Errorf("body should be {\"error\":...}: %q", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "teapot") {
		t.Errorf("body should contain message: %q", rr.Body.String())
	}
}
