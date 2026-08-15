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
	"regexp"
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
//
// project_id (optional UUID) narrows the result to runs whose
// workflow_runs.project_id matches the value — used by the per-project
// Overview tab in the FE. An empty or invalid UUID disables the filter so
// a malformed query string can't silently flip the meaning of the list.
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
		// project_id is opt-in. Validate UUID shape so a malformed query
		// param degrades to the workspace-wide list rather than a 500
		// from the database when it tries to cast a non-UUID to ::uuid.
		projectID := ""
		if v := r.URL.Query().Get("project_id"); v != "" && isUUID(v) {
			projectID = v
		}
		rows, err := svc.List(r.Context(), princ, wsID, projectID, limit, before)
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

// isUUID is a cheap shape-check for the canonical 36-char hex form
// (8-4-4-4-12). The repo's SQL casts to ::uuid anyway, but checking here
// keeps a malformed query param from surfacing as a 500.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
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
// demoFixtureRepoSHA is the synthetic repo tag the fixture codegraph is
// seeded under. Must match cmd/seed-neo4j's --repo-sha default, otherwise
// Pathfinder's FindSymbolContaining misses and the causal ranking has no
// candidates to score.
const demoFixtureRepoSHA = "fixture-seed-001"

var demoScenarios = map[string]struct{}{
	"schema-drift": {},
	"null-deref":   {},
	"oom":          {},
	"synthetic":    {},
	// Fault-injection classes for the evaluation benchmark (see
	// docs/PROJECT_PLAN.md §7 — fault classes beyond the three demo
	// scenarios). Each maps to a control-plane fixture in
	// fixtures/scenarios/.
	"zero-div":               {},
	"api-contract-violation": {},
	"dependency-breakage":    {},
	"conn-pool-exhaustion":   {},
	"deadlock":               {},
	"memory-leak":            {},
	"rate-limit-cascade":     {},
	"disk-exhaustion":        {},
}

// scenarioToFixture maps a demo scenario to the fixture incident JSON file.
// Each scenario points at its OWN file so a demo run for "oom" is visibly
// distinct from "null-deref" or "schema-drift" in the events stream. When the
// file is present, the demo handler embeds its contents in the workflow input
// as `incident: {...}` so the L1 agents have real payload to reason over.
// `synthetic` aliases to the null-pointer fixture as a generic placeholder.
var scenarioToFixture = map[string]string{
	"schema-drift": "demo-schema-drift.json",
	"null-deref":   "demo-null-pointer.json",
	"synthetic":    "demo-null-pointer.json",
	"oom":          "demo-oom.json",
	// Fault-injection classes (keep in sync with demoScenarios above and
	// usecase.scenarioFixtureFiles).
	"zero-div":               "demo-zero-div.json",
	"api-contract-violation": "demo-api-contract-violation.json",
	"dependency-breakage":    "demo-dependency-breakage.json",
	"conn-pool-exhaustion":   "demo-conn-pool-exhaustion.json",
	"deadlock":               "demo-deadlock.json",
	"memory-leak":            "demo-memory-leak.json",
	"rate-limit-cascade":     "demo-rate-limit-cascade.json",
	"disk-exhaustion":        "demo-disk-exhaustion.json",
}

// scenarioIDRE bounds what may be turned into a filename by the fixture
// lookup below. Scenario ids are catalogue-authored slugs; anything outside
// this alphabet (a path separator, a dot segment) is rejected before it can
// reach filepath.Join.
var scenarioIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// fixtureFileFor resolves the fixture filename for a scenario. Explicit
// aliases in scenarioToFixture win — several legacy ids share one file — and
// anything else resolves by convention to "<scenario>.json", so dropping a
// fixture into fixtures/scenarios/ is all it takes to wire a new card in the
// Live Demo catalogue.
func fixtureFileFor(scenario string) (string, bool) {
	if file, ok := scenarioToFixture[scenario]; ok {
		return file, true
	}
	if !scenarioIDRE.MatchString(scenario) {
		return "", false
	}
	return scenario + ".json", true
}

// scenarioAllowed reports whether a scenario may start a demo run. Legacy ids
// are accepted unconditionally; every other id must resolve to a fixture file
// that actually exists on disk, which keeps arbitrary user input out of the
// workflow payload while removing the per-scenario Go edit the old static
// allowlist required.
func scenarioAllowed(scenario string) bool {
	if _, ok := demoScenarios[scenario]; ok {
		return true
	}
	return loadFixtureIncident(scenario) != nil
}

// loadFixtureIncident reads the per-scenario fixture from one of a small set
// of well-known directories. Returns nil when the file is missing — agents
// fall back to a placeholder incident in that case. The fixture base is
// configurable via the FIXTURE_INCIDENTS_DIR env var so the binary stays
// portable across compose vs. local runs.
//
// Control-plane-owned demo fixtures live under
// services/control-plane/fixtures/scenarios (copied to /app/fixtures in the
// image). Legacy validator fixtures are still searched so demos that pre-date
// the migration keep working.
func loadFixtureIncident(scenario string) map[string]any {
	file, ok := fixtureFileFor(scenario)
	if !ok {
		return nil
	}
	candidates := []string{
		os.Getenv("FIXTURE_INCIDENTS_DIR"),
		"services/control-plane/fixtures/scenarios",
		"/app/fixtures/scenarios",
		"../../services/control-plane/fixtures/scenarios",
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
		if !scenarioAllowed(req.Scenario) {
			writeError(w, http.StatusBadRequest, "invalid scenario")
			return
		}

		demoPayload := map[string]any{
			"incident_id":  "demo",
			"triggered_by": "demo",
			"scenario":     req.Scenario,
			// Scope Pathfinder's codegraph lookup to the seeded fixture
			// repo. Without this the traversal runs against repo_sha="",
			// finds no Symbol node, and the causal sidecar degrades to
			// "no_signal". Matches cmd/seed-neo4j's default --repo-sha.
			"repo_sha": demoFixtureRepoSHA,
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
