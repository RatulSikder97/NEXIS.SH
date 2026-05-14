package handler

// Phase 4 pipeline HTTP surface.
//
// Five endpoints under /v1/workspaces/{ws_id}/pipelines, all behind
// RequireAuth + RLS. Streaming uses the SSE pattern from workspaces.go —
// replay-from-DB then tail-the-broker, with X-Accel-Buffering: no for proxy
// hygiene.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// toWorkflowRunResp converts a domain.WorkflowRun to the wire shape. Zero
// times collapse to the empty string so the client never has to handle
// "0001-01-01..." or null.
func toWorkflowRunResp(w domain.WorkflowRun) dto.WorkflowRunResp {
	resp := dto.WorkflowRunResp{
		ID:           w.ID,
		OrgID:        w.OrgID,
		WorkspaceID:  w.WorkspaceID,
		WorkflowType: w.WorkflowType,
		Status:       string(w.Status),
		CurrentStep:  w.CurrentStep,
		StartedAt:    w.StartedAt.UTC().Format(time.RFC3339),
		Error:        w.Error,
	}
	if w.CompletedAt != nil {
		resp.CompletedAt = w.CompletedAt.UTC().Format(time.RFC3339)
	}
	if w.DurationMs != nil {
		resp.DurationMs = *w.DurationMs
	}
	// Surface the Temporal-assigned WorkflowID + RunID so the dashboard can
	// deep-link to the Temporal Web UI. The service stamps "pending" on the
	// run row before ExecuteWorkflow returns; filter that placeholder out so
	// the wire shape stays clean.
	if w.TemporalWfID != "" && w.TemporalWfID != "pending" {
		resp.TemporalWorkflowID = w.TemporalWfID
	}
	if w.TemporalRunID != "" && w.TemporalRunID != "pending" {
		resp.TemporalRunID = w.TemporalRunID
	}
	return resp
}

// toActivityEventResp converts an activity_events row to the wire shape.
// Payload is JSON-decoded so the client gets structured fields instead of an
// opaque blob.
func toActivityEventResp(e domain.ActivityEvent) dto.ActivityEventResp {
	resp := dto.ActivityEventResp{
		WorkflowRunID: e.WorkflowRunID,
		Seq:           e.Seq,
		AgentRole:     string(e.AgentRole),
		ActivityName:  e.ActivityName,
		Status:        string(e.Status),
		Attempt:       e.Attempt,
		Message:       e.Message,
		TS:            e.TS.UTC().Format(time.RFC3339Nano),
	}
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &resp.Payload)
	}
	return resp
}

// PipelinesList wires GET /v1/workspaces/{ws_id}/pipelines. Always emits an
// array (never null). `limit` defaults to 50, max 200. `before` is an RFC3339
// timestamp cursor.
func PipelinesList(svc domain.WorkflowService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		wsID := chi.URLParam(r, "ws_id")
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		var before time.Time
		if v := r.URL.Query().Get("before"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				before = t
			}
		}
		rows, err := svc.List(r.Context(), princ, wsID, limit, before)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]dto.WorkflowRunResp, 0, len(rows))
		for _, row := range rows {
			out = append(out, toWorkflowRunResp(row))
		}
		httpJSON(w, http.StatusOK, out)
	}
}

// PipelineGet wires GET /v1/workspaces/{ws_id}/pipelines/{run_id}. Returns
// 404 on cross-org access or missing run.
func PipelineGet(svc domain.WorkflowService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		runID := chi.URLParam(r, "run_id")
		run, events, err := svc.Get(r.Context(), princ, runID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		evResp := make([]dto.ActivityEventResp, 0, len(events))
		for _, e := range events {
			evResp = append(evResp, toActivityEventResp(e))
		}
		httpJSON(w, http.StatusOK, dto.PipelineGetResp{
			Run:    toWorkflowRunResp(run),
			Events: evResp,
		})
	}
}

// PipelineCreate wires POST /v1/workspaces/{ws_id}/pipelines. Returns 202
// because the workflow is still running when we respond; clients follow the
// SSE stream at /v1/workspaces/{ws}/pipelines/{run}/events for live frames.
func PipelineCreate(svc domain.WorkflowService, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req dto.CreatePipelineReq
		if !decodeBody(w, r, &req) {
			return
		}
		if req.WorkflowType == "" {
			req.WorkflowType = "RecoveryPipeline"
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		wsID := chi.URLParam(r, "ws_id")
		var inputBytes []byte
		if req.Input != nil {
			inputBytes, _ = json.Marshal(req.Input)
		}
		run, err := svc.Start(r.Context(), princ, wsID, req.WorkflowType, inputBytes)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		auditWrite(r, aud, princ, "pipelines.create", run.ID, map[string]any{
			"workflow_type": req.WorkflowType,
			"workspace_id":  wsID,
		})
		writeJSON(w, http.StatusAccepted, toWorkflowRunResp(run))
	}
}

// demoScenarios is the whitelist of accepted `scenario` values. Anything else
// is rejected with 400 so we never forward arbitrary user input into the
// workflow payload.
var demoScenarios = map[string]struct{}{
	"schema-drift": {},
	"null-deref":   {},
	"oom":          {},
	"synthetic":    {},
}

// scenarioToFixture maps a demo scenario to the fixture incident JSON file.
// When the file is present, the demo handler embeds its contents in the
// workflow input as `incident: {...}` so the L1 agents have real payload.
var scenarioToFixture = map[string]string{
	"schema-drift": "schema-drift.json",
	"null-deref":   "demo-null-pointer.json",
	"synthetic":    "demo-null-pointer.json",
	"oom":          "demo-null-pointer.json",
}

// loadFixtureIncident reads services/validator/fixtures/incidents/<file>.
// Returns nil when the file is missing — agents fall back to a placeholder
// incident in that case. The fixture base is configurable via the
// FIXTURE_INCIDENTS_DIR env var so the binary stays portable across compose
// vs. local runs.
func loadFixtureIncident(scenario string) map[string]any {
	file, ok := scenarioToFixture[scenario]
	if !ok {
		return nil
	}
	candidates := []string{
		os.Getenv("FIXTURE_INCIDENTS_DIR"),
		"services/validator/fixtures/incidents",
		"/app/fixtures/incidents",
		"../../services/validator/fixtures/incidents",
	}
	for _, base := range candidates {
		if base == "" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(base, file))
		if err != nil {
			continue
		}
		var inc map[string]any
		if err := json.Unmarshal(body, &inc); err == nil {
			return inc
		}
	}
	return nil
}

// PipelineDemo wires POST /v1/workspaces/{ws_id}/pipelines/demo. Identical
// to PipelineCreate but stamps triggered_by=demo so the dashboard can filter
// demo runs from real ones. Gated to cfg.AppEnv == "dev" by the router.
//
// Body is optional. When present it may carry a `scenario` field which is
// whitelisted (see demoScenarios) and stamped onto the workflow input so the
// recovery DAG can branch on the seeded failure type. An empty/absent body
// defaults to "synthetic".
//
// cfg is used to route the 500-arm error through safeErrorMessage so prod
// returns a generic message even though this route is dev-gated today — keeps
// the behaviour consistent with the rest of the surface.
func PipelineDemo(svc domain.WorkflowService, aud domain.AuditWriter, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		wsID := chi.URLParam(r, "ws_id")

		// Decode best-effort: empty body / no body should default the scenario
		// instead of erroring. Reject only when the body is present but
		// malformed or carries unknown fields.
		var req dto.PipelineDemoReq
		if r.Body != nil && r.ContentLength != 0 {
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		}
		if req.Scenario == "" {
			req.Scenario = "synthetic"
		}
		if _, ok := demoScenarios[req.Scenario]; !ok {
			writeError(w, http.StatusBadRequest, "invalid scenario")
			return
		}

		demoPayload := map[string]any{
			"incident_id":  "demo",
			"triggered_by": "demo",
			"scenario":     req.Scenario,
		}
		if inc := loadFixtureIncident(req.Scenario); inc != nil {
			demoPayload["incident"] = inc
		}
		if req.ProjectID != "" {
			demoPayload["project_id"] = req.ProjectID
		}
		inputBytes, _ := json.Marshal(demoPayload)
		run, err := svc.Start(r.Context(), princ, wsID, "RecoveryPipeline", inputBytes)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, safeErrorMessage(err, cfg, "pipelines.demo", "workspace_id", wsID, "scenario", req.Scenario))
			return
		}
		auditWrite(r, aud, princ, "pipelines.demo", run.ID, map[string]any{
			"workspace_id": wsID,
			"scenario":     req.Scenario,
		})
		writeJSON(w, http.StatusAccepted, toWorkflowRunResp(run))
	}
}

// PipelineEvents wires GET /v1/workspaces/{ws_id}/pipelines/{run_id}/events.
// Streams activity_events as SSE until the terminal Pipeline.Complete event
// or client disconnect. Ownership is verified BEFORE upgrading to
// text/event-stream so unauthorised callers get a clean 404.
//
// The handler replays everything in the DB first (so refresh-mid-run shows
// correct cumulative state), then tails the in-process broker. Clients dedup
// on (run_id, seq).
func PipelineEvents(svc domain.WorkflowService, wfRepo *repo.WorkflowRepo, wsRepo *repo.WorkspacesRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		wsID := chi.URLParam(r, "ws_id")
		runID := chi.URLParam(r, "run_id")
		if wsRepo != nil {
			ok, err := wsRepo.OwnsWorkspace(r.Context(), princ.OrgID, wsID)
			if err != nil || !ok {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
		}
		run, _, err := svc.Get(r.Context(), princ, runID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if run.WorkspaceID != wsID {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		// Lift the response controller before any header write so we can
		// flush + clear the upstream write deadline (60s timeout middleware).
		rc := http.NewResponseController(w)
		_ = rc.SetWriteDeadline(time.Time{})

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		if err := rc.Flush(); err != nil {
			slog.Default().Error("pipeline sse: flush after WriteHeader", "err", err)
			return
		}

		// 1) Replay existing events.
		lastSeq := 0
		if wfRepo != nil {
			existing, err := wfRepo.ListEvents(r.Context(), runID, 0, 500)
			if err != nil {
				slog.Default().Warn("pipeline sse: list existing events", "err", err, "run", runID)
			}
			for _, e := range existing {
				if !writeEventFrame(w, rc, e) {
					return
				}
				if e.Seq > lastSeq {
					lastSeq = e.Seq
				}
				if e.AgentRole == domain.AgentPipeline {
					closeEventStream(w, rc)
					return
				}
			}
		}

		// 2) Tail the broker for new frames.
		ch := svc.Subscribe(r.Context(), runID)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				// Skip frames we already replayed (race between Subscribe
				// and ListEvents).
				if ev.Seq <= lastSeq {
					continue
				}
				if !writeEventFrame(w, rc, ev) {
					return
				}
				lastSeq = ev.Seq
				if ev.AgentRole == domain.AgentPipeline {
					closeEventStream(w, rc)
					return
				}
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				_ = rc.Flush()
			}
		}
	}
}

// writeEventFrame serialises e + writes one SSE frame. Returns false when
// the underlying writer errors (client disconnect) so the caller can bail.
func writeEventFrame(w http.ResponseWriter, rc *http.ResponseController, e domain.ActivityEvent) bool {
	payload, err := json.Marshal(toActivityEventResp(e))
	if err != nil {
		slog.Default().Error("pipeline sse: marshal", "err", err)
		return true // keep going
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return false
	}
	_ = rc.Flush()
	return true
}

// closeEventStream emits an explicit "close" SSE event so the client can
// distinguish a terminal stream from a network blip.
func closeEventStream(w http.ResponseWriter, rc *http.ResponseController) {
	_, _ = fmt.Fprint(w, "event: close\ndata: {}\n\n")
	_ = rc.Flush()
}
