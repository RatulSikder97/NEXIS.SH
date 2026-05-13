// Package handler — Sentinel admin HTTP surface (Phase 8 — public beta).
//
// One endpoint: POST /v1/admin/sentinel/trigger. Owner-only escape hatch that
// bypasses the poll loop and synthesises a manual IncidentTrigger so an
// operator can demo a recovery without waiting for a real Sentry fatal.
//
// The handler delegates to sentinel.Detector.TriggerOne so the
// audit/logging machinery is identical to the rule-based path.
package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// SentinelTriggerer is the narrow port the admin-trigger handler depends on.
// *sentinel.Detector satisfies it via the Phase 8 TriggerOne method. Defining
// the interface here keeps the transport layer from importing the sentinel
// component directly, which would violate the arch contract
// (transport→sentinel is not on the allow list).
type SentinelTriggerer interface {
	TriggerOne(ctx context.Context, orgID, incidentID string) (domain.WorkflowRun, error)
}

// sentinelTriggerReq is the body of POST /v1/admin/sentinel/trigger. OrgID is
// optional — when empty we fall back to the caller's principal org. IncidentID
// is optional context that gets stamped on the synthetic trigger; it is not
// validated against incidents_raw.
type sentinelTriggerReq struct {
	OrgID      string `json:"org_id"`
	IncidentID string `json:"incident_id,omitempty"`
}

// sentinelTriggerResp is the body returned on success.
type sentinelTriggerResp struct {
	RunID       string `json:"run_id"`
	WorkspaceID string `json:"workspace_id"`
	OrgID       string `json:"org_id"`
}

// SentinelTrigger wires POST /v1/admin/sentinel/trigger. Owner-only via the
// existing RequireRole middleware on the route group.
func SentinelTrigger(det SentinelTriggerer, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		var req sentinelTriggerReq
		if !decodeBody(w, r, &req) {
			return
		}
		orgID := req.OrgID
		if orgID == "" {
			orgID = princ.OrgID
		}
		// Defence-in-depth: an owner of org X cannot trigger a sentinel run on
		// org Y. The Phase 8 spec scopes this to "owner role on any org" so
		// the only valid cross-org target is the principal's own org — i.e.
		// no cross-org targets at all.
		if orgID != princ.OrgID {
			writeError(w, http.StatusForbidden, "cannot trigger sentinel for another org")
			return
		}
		if det == nil {
			writeError(w, http.StatusServiceUnavailable, "sentinel disabled")
			return
		}
		run, err := det.TriggerOne(r.Context(), orgID, req.IncidentID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "no workspace for org")
				return
			}
			writeError(w, http.StatusInternalServerError, "trigger failed")
			return
		}
		auditWrite(r, aud, princ, "sentinel.admin_triggered", run.ID, map[string]any{
			"incident_id": req.IncidentID,
		})
		writeJSON(w, http.StatusAccepted, sentinelTriggerResp{
			RunID:       run.ID,
			WorkspaceID: run.WorkspaceID,
			OrgID:       run.OrgID,
		})
	}
}
