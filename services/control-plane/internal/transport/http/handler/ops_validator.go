package handler

// Validator sandbox observability — GET /v1/validator/runs.
//
// The validator service runs out-of-band today; the control-plane only sees
// it through activity_events frames emitted by the validator_l2 agent (see
// internal/workflow/recovery/activities.go). This endpoint folds those
// frames into per-(workflow_run) summaries so the UI can render the recent
// runs without needing direct access to the validator service.
//
// When no validator data exists yet the endpoint returns 200 + an empty
// page + a `note` field so the dashboard can render the cold-start banner
// instead of treating it as an error.

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// ValidatorRuns wires GET /v1/validator/runs. Limit defaults 20, max 200.
//
// SQL shape:
//
//   The validator_l2 agent emits a started frame at run begin and one of
//   succeeded/failed/timed_out at run end. We group by workflow_run_id +
//   pick the earliest started ts (start) + latest terminal ts (end), then
//   derive duration_ms. patch_sha + stdout/stderr_head are pulled out of
//   the latest payload column when present (the agent's contract carries
//   them; in stub mode they are absent and we leave them blank).
//
// The admin pool drives this read because the operator endpoint already
// enforces tenancy via the principal's OrgID — and the activity_events
// table doesn't carry an org_id-side index that the app pool's RLS could
// use efficiently for cross-workflow scans.
func ValidatorRuns(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		limit, _ := parseLimitOffset(r, 20, 200)

		if pool == nil {
			writeJSON(w, http.StatusOK, dto.ValidatorRunsResp{
				Runs: []dto.ValidatorRunResp{},
				Note: "validator runs not yet emitted",
			})
			return
		}

		rows, err := pool.Query(r.Context(), `
			WITH val_runs AS (
				SELECT
					ae.workflow_run_id,
					MIN(ae.ts) FILTER (WHERE ae.status='started')                                  AS started_at,
					MAX(ae.ts) FILTER (WHERE ae.status IN ('succeeded','failed','timed_out'))      AS finished_at,
					BOOL_OR(ae.status='failed' OR ae.status='timed_out')                            AS any_failed,
					BOOL_OR(ae.status='succeeded')                                                  AS any_succeeded,
					BOOL_OR(ae.status='started')                                                    AS any_started,
					(ARRAY_AGG(ae.payload ORDER BY ae.ts DESC)
						FILTER (WHERE ae.payload IS NOT NULL))[1]                                  AS latest_payload
				FROM activity_events ae
				JOIN workflow_runs wr ON wr.id = ae.workflow_run_id
				WHERE wr.org_id = $1 AND ae.agent_role = 'validator_l2'
				GROUP BY ae.workflow_run_id
			)
			SELECT workflow_run_id::text, started_at, finished_at,
			       any_failed, any_started, any_succeeded,
			       COALESCE(latest_payload::text, '')
			FROM val_runs
			ORDER BY started_at DESC NULLS LAST
			LIMIT $2`, princ.OrgID, limit)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()

		out := []dto.ValidatorRunResp{}
		for rows.Next() {
			var (
				runID                            string
				startedAt, finishedAt            *time.Time
				anyFailed, anyStarted, anySucc   bool
				payloadStr                       string
			)
			if err := rows.Scan(
				&runID, &startedAt, &finishedAt,
				&anyFailed, &anyStarted, &anySucc,
				&payloadStr,
			); err != nil {
				httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			row := dto.ValidatorRunResp{
				ID:     runID,
				Status: deriveValidatorRunStatus(anyFailed, anyStarted, anySucc),
			}
			if startedAt != nil && !startedAt.IsZero() {
				row.TS = startedAt.UTC().Format(time.RFC3339Nano)
				if finishedAt != nil && !finishedAt.IsZero() {
					row.DurationMs = finishedAt.Sub(*startedAt).Milliseconds()
				}
			}
			// Latest payload (when present) carries the validator contract
			// fields. We're permissive on the shape — absent keys leave the
			// row's fields blank.
			if payloadStr != "" {
				var p map[string]any
				if jErr := json.Unmarshal([]byte(payloadStr), &p); jErr == nil {
					if v, ok := p["patch_sha"].(string); ok {
						row.PatchSHA = v
					}
					if v, ok := p["stdout_head"].(string); ok {
						row.StdoutHead = v
					}
					if v, ok := p["stderr_head"].(string); ok {
						row.StderrHead = v
					}
				}
			}
			out = append(out, row)
		}
		if err := rows.Err(); err != nil && !errors.Is(err, errors.New("EOF")) {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		resp := dto.ValidatorRunsResp{Runs: out}
		if len(out) == 0 {
			resp.Note = "validator runs not yet emitted"
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// deriveValidatorRunStatus is the failed > running > succeeded reduction.
func deriveValidatorRunStatus(anyFailed, anyStarted, anySucceeded bool) string {
	switch {
	case anyFailed:
		return "failed"
	case anyStarted && !anySucceeded:
		return "running"
	case anySucceeded:
		return "succeeded"
	default:
		return "unknown"
	}
}
