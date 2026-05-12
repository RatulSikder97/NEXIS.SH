package middleware

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// RLS opens a per-request pgx.Tx on the supplied application-role pool, binds
// app.current_org_id (tx-local) to the authenticated principal's org, threads
// the tx into ctx via db.WithTx, and commits on 2xx/3xx (rollback otherwise).
//
// Place RLS AFTER RequireAuth in the middleware chain — the principal must be
// resolved before we can pin the tenant GUC.
//
// Failure modes:
//   - No principal in ctx (defensive): 401. RequireAuth should have caught
//     this already, so this path is only hit if RLS is misconfigured upstream.
//   - BeginTx / set_config failure: 500. We can't safely run a tenant query
//     without RLS pinned, so we refuse.
//   - Commit failure: logged; the response has already been written and the
//     status emitted to the client. Treat as best-effort durability — the
//     tenant write is lost but the request is over.
func RLS(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			princ, ok := PrincipalFrom(r.Context())
			if !ok {
				// Defensive — RequireAuth should have rejected this already.
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			ctx := r.Context()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				slog.Error("rls: begin tx", "err", err)
				writeJSONError(w, http.StatusInternalServerError, "internal error")
				return
			}
			// Rollback is idempotent vs Commit (returns pgx.ErrTxClosed once the
			// tx is finalised). Deferring an unconditional Rollback is the
			// idiomatic pgx pattern.
			defer func() {
				if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
					slog.Error("rls: rollback", "err", rbErr)
				}
			}()

			// Bind the tenant GUC to the per-tx scope. We use set_config(name,
			// value, is_local) rather than the SQL `SET LOCAL …` form because
			// SET does NOT accept parameter placeholders — it requires a
			// literal. set_config does, which keeps us safe from any
			// org_id-string injection while preserving the tx-local scope. The
			// RLS policies on org_members/sessions/api_keys/audit_log read
			// `current_setting('app.current_org_id', true)`.
			if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", princ.OrgID); err != nil {
				slog.Error("rls: set local org_id", "err", err, "org_id", princ.OrgID)
				writeJSONError(w, http.StatusInternalServerError, "internal error")
				return
			}

			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r.WithContext(db.WithTx(ctx, tx)))

			// Commit only on 2xx/3xx. Anything else (incl. handler-level 4xx) is
			// rolled back so partial writes don't leak.
			if rw.status >= 200 && rw.status < 400 {
				if err := tx.Commit(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
					slog.Error("rls: commit", "err", err, "status", rw.status)
				}
			}
		})
	}
}

// statusRecorder wraps an http.ResponseWriter to remember the status code the
// handler emitted. It MUST NOT buffer body bytes — Write passes through to the
// underlying writer unchanged so streaming handlers still work.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	// http.ResponseWriter contract: an implicit 200 is emitted on the first
	// Write if WriteHeader wasn't called. Mirror that so r.status is accurate
	// without us calling WriteHeader twice.
	if !r.wroteHeader {
		r.wroteHeader = true
		// r.status was initialised to 200 in RLS — leave it.
	}
	return r.ResponseWriter.Write(b)
}

// writeJSONError keeps the RLS middleware's error responses shape-compatible
// with the rest of the surface ("error": "..." JSON object).
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
