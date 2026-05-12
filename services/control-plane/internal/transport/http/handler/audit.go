package handler

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/audit"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// AuditVerify wires GET /v1/audit/verify. The endpoint walks every audit_log
// row across every org and recomputes the per-org HMAC chain — any mutation
// of an existing row (action change, metadata edit, delete-and-reinsert) is
// detected.
//
// Although the route sits inside the protected group (RequireAuth + RLS),
// Verify takes the admin pool directly and does NOT use the per-request RLS
// tx. The reason is integrity, not tenancy: the chain is a global invariant
// and the recomputation must see every row. The RLS-bound query would only
// surface the caller's org and would silently report OK on an untouched
// neighbour-org chain whose contents the caller can't see anyway.
//
// For Phase 2 we accept that any authenticated user can call Verify. A future
// phase will gate this behind an admin scope.
func AuditVerify(secret []byte, pool db.Querier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := audit.Verify(r.Context(), pool, secret)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(audit.VerifyResult{OK: false, Error: err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(res)
	}
}

// parseFilter builds an AuditFilter from the request's query string. Unknown
// parameters are ignored; bad timestamp/integer values fall back silently to
// the zero default so a malformed filter never produces a 500.
func parseFilter(r *http.Request) audit.AuditFilter {
	q := r.URL.Query()
	f := audit.AuditFilter{
		Actor:  q.Get("actor"),
		Action: q.Get("action"),
	}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Since = &t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Until = &t
		}
	}
	if v := q.Get("limit"); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &f.Limit)
	}
	if v := q.Get("offset"); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &f.Offset)
	}
	return f
}

// AuditList wires GET /v1/audit. Returns the caller's org's rows in
// created_at DESC order, paginated. The body shape is {rows, total} so the UI
// can drive a paginated table without a second count query.
func AuditList(lister audit.Lister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		rows, total, err := lister.List(r.Context(), princ.OrgID, parseFilter(r))
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"rows": rows, "total": total})
	}
}

// AuditCSV wires GET /v1/audit.csv. Same filter semantics as AuditList but
// caps the page at 10_000 rows and emits text/csv with a downloadable
// disposition. Columns: id, actor, action, target, created_at.
func AuditCSV(lister audit.Lister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		f := parseFilter(r)
		f.Limit = 10000
		rows, _, err := lister.List(r.Context(), princ.OrgID, f)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="audit.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "actor", "action", "target", "created_at"})
		for _, row := range rows {
			_ = cw.Write([]string{row.ID, row.Actor, row.Action, row.Target, row.CreatedAt.UTC().Format(time.RFC3339)})
		}
		cw.Flush()
	}
}
