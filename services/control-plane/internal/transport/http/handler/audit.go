package handler

import (
	"encoding/json"
	"net/http"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/audit"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
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
