package handler

// Operational activity surfaces — the cross-workflow feed + SSE companion.
//
// These endpoints are org-scoped via session principal — the {org_id} URL
// path param exists for symmetry with the rest of the v1 surface but the
// handler always filters by `principal.OrgID`, never by the URL value. That
// way a leaked URL never leaks data: the principal's org is the boundary.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// OrgActivity wires GET /v1/orgs/{org_id}/activity. Paginated cross-workflow
// activity feed for the caller's org. Filters via query params:
//
//	limit, offset                — page cursor (limit default 50, max 200)
//	since=<rfc3339>              — exclusive lower bound on event ts
//	kind=start|log|finish|error  — collapses onto activity_events.status
//	agent_role=<role>            — exact match on activity_events.agent_role
//	workspace_id=<id>            — narrow to one workspace
//
// Returns `{events: [...], total: N}` so the dashboard can render "N of M".
// 401 on missing principal; 500 on repo failure.
func OrgActivity(wfRepo *repo.WorkflowRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if wfRepo == nil {
			writeJSON(w, http.StatusOK, dto.OrgActivityResp{Events: []dto.ActivityEventResp{}, Total: 0})
			return
		}
		f := repo.OrgActivityFilter{
			OrgID:       princ.OrgID,
			Kind:        r.URL.Query().Get("kind"),
			AgentRole:   r.URL.Query().Get("agent_role"),
			WorkspaceID: r.URL.Query().Get("workspace_id"),
		}
		f.Limit, f.Offset = parseLimitOffset(r, 50, 200)
		if v := r.URL.Query().Get("since"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				f.Since = t
			}
		}
		events, total, err := wfRepo.ListOrgActivity(r.Context(), f)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]dto.ActivityEventResp, 0, len(events))
		for _, e := range events {
			out = append(out, toActivityEventResp(e))
		}
		writeJSON(w, http.StatusOK, dto.OrgActivityResp{Events: out, Total: total})
	}
}

// OrgActivityStream wires GET /v1/orgs/{org_id}/activity-stream. Streams the
// cross-workflow feed as SSE.
//
// The spec calls for "subscribe to the existing sse.Broker (or poll
// activity_events every 2s if no broker exists for cross-workflow streams)".
// The control-plane today only has per-(workflow_run) brokers, so this
// handler implements the poll path: cursor on ts, query the DB on a 2s
// ticker, advance the cursor as rows are emitted.
//
// SSE hygiene:
//   - Content-Type: text/event-stream; Cache-Control: no-store; X-Accel-
//     Buffering: no — matches the rest of the surface.
//   - Heartbeat `: keepalive\n\n` every 15s.
//   - Honours r.Context().Done() — disconnects + server shutdown both
//     terminate the loop cleanly.
func OrgActivityStream(wfRepo *repo.WorkflowRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		// Lift the response controller before any header write so we can
		// flush + clear the upstream write deadline (60s timeout middleware).
		rc := http.NewResponseController(w)
		_ = rc.SetWriteDeadline(time.Time{})

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		if err := rc.Flush(); err != nil {
			slog.Default().Error("activity sse: flush after WriteHeader", "err", err)
			return
		}

		// Cursor — start at the current wall-clock so an initial connect
		// streams only NEW events. Clients that need history first should
		// hit the paginated endpoint.
		cursor := time.Now().UTC()
		const pollInterval = 2 * time.Second
		const heartbeatInterval = 15 * time.Second

		poll := time.NewTicker(pollInterval)
		defer poll.Stop()
		heartbeat := time.NewTicker(heartbeatInterval)
		defer heartbeat.Stop()

		emit := func() bool {
			if wfRepo == nil {
				return true
			}
			events, err := wfRepo.ListOrgActivityAfter(r.Context(), princ.OrgID, cursor, 100)
			if err != nil {
				slog.Default().Warn("activity sse: poll", "err", err, "org", princ.OrgID)
				return true
			}
			for _, e := range events {
				payload, mErr := json.Marshal(toActivityEventResp(e))
				if mErr != nil {
					continue
				}
				if _, wErr := fmt.Fprintf(w, "data: %s\n\n", payload); wErr != nil {
					return false
				}
				if e.TS.After(cursor) {
					cursor = e.TS
				}
			}
			if len(events) > 0 {
				_ = rc.Flush()
			}
			return true
		}

		// First tick — emit any rows that landed between the cursor's
		// initialisation and now without waiting a full poll interval.
		if !emit() {
			return
		}

		for {
			select {
			case <-r.Context().Done():
				return
			case <-poll.C:
				if !emit() {
					return
				}
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
					return
				}
				_ = rc.Flush()
			}
		}
	}
}

// parseLimitOffsetFromStrings is the helper used by /v1/integrations/webhooks
// before the chi router is invoked — kept out of agents.go's path so the
// reuse stays explicit.
func parseLimitOffsetFromStrings(limitStr, offsetStr string, defaultLimit, maxLimit int) (int, int) {
	limit := defaultLimit
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			if n > maxLimit {
				n = maxLimit
			}
			limit = n
		}
	}
	offset := 0
	if offsetStr != "" {
		if n, err := strconv.Atoi(offsetStr); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}
