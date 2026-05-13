// Package handler — org-stats HTTP surface (Phase 8 — public beta).
//
// Single endpoint: GET /v1/me/org-stats. Returns the trigger-maintained
// successful_recoveries_count for the current principal's org. Mirrors the
// shape of /v1/me — minimal, fast, cacheable.
package handler

import (
	"errors"
	"net/http"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// orgStatsResp is the body of GET /v1/me/org-stats. Today the only field is
// successful_recoveries_count; the struct gives us room to grow without
// breaking the API contract.
type orgStatsResp struct {
	SuccessfulRecoveriesCount int `json:"successful_recoveries_count"`
}

// OrgStats wires GET /v1/me/org-stats. RequireAuth guarantees a principal.
func OrgStats(repoInst *repo.OrgStatsRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		n, err := repoInst.SuccessfulRecoveriesCount(r.Context(), princ.OrgID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				// Stale principal — the org was removed underneath. Treat as 0.
				writeJSON(w, http.StatusOK, orgStatsResp{SuccessfulRecoveriesCount: 0})
				return
			}
			writeError(w, http.StatusInternalServerError, "org stats failed")
			return
		}
		writeJSON(w, http.StatusOK, orgStatsResp{SuccessfulRecoveriesCount: n})
	}
}
