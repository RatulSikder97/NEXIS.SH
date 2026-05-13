// Package handler — NASA-TLX HTTP surface (Phase 8 — public beta).
//
// One endpoint for any authenticated principal to submit a workload survey
// after a recovery. The endpoint is intentionally untyped at the workspace
// level — the survey is per user × org, not per workspace — so it lives
// under /v1/nasa-tlx (no path scope).
package handler

import (
	"net/http"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// nasaTLXReq is the body of POST /v1/nasa-tlx. All six subscales are required;
// notes + recovery_run_id are optional. The 0..20 range is the standard NASA
// Task Load Index half-cm scale; the CHECK on the table enforces the same
// bounds as defence-in-depth.
type nasaTLXReq struct {
	MentalDemand   int    `json:"mental_demand"`
	PhysicalDemand int    `json:"physical_demand"`
	TemporalDemand int    `json:"temporal_demand"`
	Performance    int    `json:"performance"`
	Effort         int    `json:"effort"`
	Frustration    int    `json:"frustration"`
	Notes          string `json:"notes,omitempty"`
	RecoveryRunID  string `json:"recovery_run_id,omitempty"`
}

// nasaTLXResp is the body of POST /v1/nasa-tlx (201). The id lets the client
// follow up with an edit endpoint in a later phase — for now this is the only
// write surface.
type nasaTLXResp struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

// validSubscale checks the standard 0..20 NASA-TLX range. The DB CHECK
// constraint catches anything that slipped past us; this validation just
// gives the client a clearer 400.
func validSubscale(v int) bool {
	return v >= 0 && v <= 20
}

// NASATLXSubmit wires POST /v1/nasa-tlx. Any authenticated principal.
func NASATLXSubmit(repoInst *repo.NASATLXRepo, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		var req nasaTLXReq
		if !decodeBody(w, r, &req) {
			return
		}
		// Validate each subscale individually so the client gets a precise
		// failure message rather than "one of the values is out of range".
		switch {
		case !validSubscale(req.MentalDemand):
			writeError(w, http.StatusBadRequest, "mental_demand out of range (0..20)")
			return
		case !validSubscale(req.PhysicalDemand):
			writeError(w, http.StatusBadRequest, "physical_demand out of range (0..20)")
			return
		case !validSubscale(req.TemporalDemand):
			writeError(w, http.StatusBadRequest, "temporal_demand out of range (0..20)")
			return
		case !validSubscale(req.Performance):
			writeError(w, http.StatusBadRequest, "performance out of range (0..20)")
			return
		case !validSubscale(req.Effort):
			writeError(w, http.StatusBadRequest, "effort out of range (0..20)")
			return
		case !validSubscale(req.Frustration):
			writeError(w, http.StatusBadRequest, "frustration out of range (0..20)")
			return
		}

		userID := princ.UserID
		orgID := princ.OrgID
		var runID *string
		if rid := strings.TrimSpace(req.RecoveryRunID); rid != "" {
			runID = &rid
		}
		resp := domain.NASATLXResponse{
			UserID:         &userID,
			OrgID:          &orgID,
			RecoveryRunID:  runID,
			MentalDemand:   req.MentalDemand,
			PhysicalDemand: req.PhysicalDemand,
			TemporalDemand: req.TemporalDemand,
			Performance:    req.Performance,
			Effort:         req.Effort,
			Frustration:    req.Frustration,
			Notes:          strings.TrimSpace(req.Notes),
		}
		if err := repoInst.Insert(r.Context(), &resp); err != nil {
			writeError(w, http.StatusInternalServerError, "insert failed")
			return
		}
		auditWrite(r, aud, princ, "nasa_tlx.submitted", resp.ID, map[string]any{
			"recovery_run_id": req.RecoveryRunID,
		})
		writeJSON(w, http.StatusCreated, nasaTLXResp{
			ID:        resp.ID,
			CreatedAt: resp.CreatedAt.UTC().Format(timeFmtRFC3339),
		})
	}
}

// timeFmtRFC3339 mirrors time.RFC3339 — defined as a local string so we don't
// reach into time. on every handler. Kept here so other Phase 8 handlers can
// reuse without circular imports.
const timeFmtRFC3339 = "2006-01-02T15:04:05Z07:00"
