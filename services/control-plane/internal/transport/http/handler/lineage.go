package handler

// OpenLineage read surface — the queryable view over lineage_events
// (migration 0028). Org-scoped via session principal, same posture as
// ops_activity.go: any {org_id} URL symmetry is cosmetic, the handler always
// filters by principal.OrgID so a leaked URL never leaks data.

import (
	"net/http"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// lineageEventResp is the wire shape of one lineage event. `event` carries
// the stored OpenLineage RunEvent JSON verbatim so spec-aware consumers can
// feed it straight into an OpenLineage backend; the sibling fields are the
// indexed columns for cheap client-side grouping.
type lineageEventResp struct {
	ID            string         `json:"id"`
	WorkflowRunID string         `json:"workflow_run_id"`
	EventType     string         `json:"event_type"`
	EventTime     time.Time      `json:"event_time"`
	JobNamespace  string         `json:"job_namespace"`
	JobName       string         `json:"job_name"`
	RunID         string         `json:"run_id"`
	Event         map[string]any `json:"event"`
	CreatedAt     time.Time      `json:"created_at"`
}

// lineageListResp is the paginated envelope — `{events: [...], total: N}` so
// the dashboard can render "N of M", matching the rest of the list surface.
type lineageListResp struct {
	Events []lineageEventResp `json:"events"`
	Total  int                `json:"total"`
}

// LineageEvents wires GET /v1/lineage/events. Paginated OpenLineage RunEvent
// feed for the caller's org. Filters via query params:
//
//	limit, offset                 — page cursor (limit default 50, max 200)
//	workflow_run_id=<uuid>        — narrow to one recovery run
//	job_name=<name>               — e.g. migration.propose | migration.apply
//
// 401 on missing principal; 500 on repo failure. A nil repo (dev boot
// without DATABASE_URL) degrades to an empty page rather than an error.
func LineageEvents(lineage *repo.LineageRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if lineage == nil {
			writeJSON(w, http.StatusOK, lineageListResp{Events: []lineageEventResp{}, Total: 0})
			return
		}
		f := repo.LineageFilter{
			OrgID:         princ.OrgID,
			WorkflowRunID: r.URL.Query().Get("workflow_run_id"),
			JobName:       r.URL.Query().Get("job_name"),
		}
		f.Limit, f.Offset = parseLimitOffset(r, 50, 200)
		events, total, err := lineage.ListByOrg(r.Context(), f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "lineage list failed")
			return
		}
		out := make([]lineageEventResp, 0, len(events))
		for _, e := range events {
			out = append(out, lineageEventResp{
				ID:            e.ID,
				WorkflowRunID: e.WorkflowRunID,
				EventType:     e.EventType,
				EventTime:     e.EventTime,
				JobNamespace:  e.JobNamespace,
				JobName:       e.JobName,
				RunID:         e.RunID,
				Event:         e.Event,
				CreatedAt:     e.CreatedAt,
			})
		}
		writeJSON(w, http.StatusOK, lineageListResp{Events: out, Total: total})
	}
}
