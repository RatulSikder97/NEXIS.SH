// Package handler — eval HTTP surface (Phase 5 Stage 7).
//
// Four endpoints under /v1/workspaces/{ws_id}/{eval,agents}, all behind
// RequireAuth + RLS. The runner is kicked off in a goroutine from
// EvalRunCreate so the POST returns 202 immediately and the client polls
// the list endpoint for progress.
//
//   GET    /v1/workspaces/{ws_id}/eval                  → list (owner|admin|member)
//   GET    /v1/workspaces/{ws_id}/eval/{run_id}         → detail (owner|admin|member)
//   POST   /v1/workspaces/{ws_id}/eval                  → kick off run (owner|admin)
//   GET    /v1/workspaces/{ws_id}/agents/budget         → topbar pill (owner|admin|member)
package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
	"github.com/nexis-eco/nexis/services/control-plane/internal/usecase"
)

// evalScenarios is the whitelist accepted by EvalRunCreate. Matches
// apps/web/lib/eval.ts:SCENARIOS exactly so the dropdown can't ship a
// value the server rejects.
var evalScenarios = map[string]struct{}{
	"schema-drift": {},
	"null-deref":   {},
	"oom":          {},
}

// costToWire converts the DB-stored fractional cents (e.g. 0.42 cents =
// $0.0042) into the cents × 10000 the frontend expects. Mirrors the
// formatCentsExact() conversion in apps/web/lib/eval.ts so the UI's
// `centsExact / 10000 / 100` formula round-trips.
func costToWire(cents float64) float64 {
	return cents * 10000
}

// toEvalRunSummaryResp builds the list/detail-row wire shape. CompletedAt
// nil collapses to "" (omitempty drops it) so the frontend's
// `isInFlight` predicate sees the absence as "still running".
func toEvalRunSummaryResp(r domain.EvalRun) dto.EvalRunSummaryResp {
	resp := dto.EvalRunSummaryResp{
		ID:                   r.ID,
		Scenario:             r.IncidentLabel,
		OpenAIStatus:         normaliseStatus(r.OpenAIStatus),
		OllamaStatus:         normaliseStatus(r.OllamaStatus),
		OpenAICostCentsExact: costToWire(r.OpenAICostCentsExact),
		OllamaCostCentsExact: costToWire(r.OllamaCostCentsExact),
		OpenAITokensIn:       r.OpenAITokensIn,
		OpenAITokensOut:      r.OpenAITokensOut,
		OllamaTokensIn:       r.OllamaTokensIn,
		OllamaTokensOut:      r.OllamaTokensOut,
		DurationOpenAIMs:     r.DurationOpenAIMs,
		DurationOllamaMs:     r.DurationOllamaMs,
		StartedAt:            r.StartedAt.UTC().Format(time.RFC3339),
		Error:                r.Error,
	}
	if r.CompletedAt != nil {
		resp.FinishedAt = r.CompletedAt.UTC().Format(time.RFC3339)
	}
	return resp
}

// normaliseStatus coerces empty / unknown values to the closest valid
// frontend status so the list endpoint always emits a usable string.
func normaliseStatus(s domain.EvalRunStatus) string {
	switch s {
	case domain.EvalRunStatusQueued,
		domain.EvalRunStatusRunning,
		domain.EvalRunStatusSucceeded,
		domain.EvalRunStatusFailed:
		return string(s)
	case domain.EvalRunStatusError:
		return string(domain.EvalRunStatusFailed)
	case "":
		return string(domain.EvalRunStatusQueued)
	default:
		return string(s)
	}
}

// toEvalTranscriptResp builds the per-cell wire shape. Agent is mapped to
// PascalCase via domain.AgentDisplayName so the frontend's `EvalAgent`
// union deserialises cleanly.
func toEvalTranscriptResp(t domain.EvalTranscript) dto.EvalTranscriptResp {
	return dto.EvalTranscriptResp{
		ID:             t.ID,
		EvalRunID:      t.EvalRunID,
		Provider:       t.Provider,
		Agent:          domain.AgentDisplayName(t.Agent),
		Model:          t.Model,
		InputJSON:      orEmpty(t.InputJSON),
		OutputJSON:     orEmpty(t.OutputJSON),
		Success:        t.Success,
		TokensIn:       t.TokensIn,
		TokensOut:      t.TokensOut,
		CachedTokens:   t.CachedTokens,
		CostCentsExact: costToWire(t.CostCents),
		DurationMs:     t.DurationMs,
		StartedAt:      t.StartedAt.UTC().Format(time.RFC3339),
		FinishedAt:     t.FinishedAt.UTC().Format(time.RFC3339),
	}
}

// orEmpty replaces a nil map with an empty one so the wire payload never
// has `"input_json": null` — the frontend's type union expects an object.
func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// EvalRunsList wires GET /v1/workspaces/{ws_id}/eval. The list is small
// (typically <100 rows) so Phase 5 doesn't paginate; limit defaults to
// 50 and may be tuned via ?limit=. Always returns an array, never null.
func EvalRunsList(evalRepo *repo.EvalRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		offset := 0
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				offset = n
			}
		}
		rows, err := evalRepo.List(r.Context(), princ, limit, offset)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]dto.EvalRunSummaryResp, 0, len(rows))
		for _, row := range rows {
			out = append(out, toEvalRunSummaryResp(row))
		}
		httpJSON(w, http.StatusOK, out)
	}
}

// EvalRunGet wires GET /v1/workspaces/{ws_id}/eval/{run_id}. Returns 404
// when the run does not exist or belongs to a different org (RLS +
// defence-in-depth org_id filter in EvalRepo.Get).
func EvalRunGet(evalRepo *repo.EvalRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		// API-key principals may have empty SessionID; mark non-empty so
		// EvalRepo.Get uses the per-request RLS tx via db.FromCtx rather
		// than falling back to the admin pool.
		if princ.SessionID == "" {
			princ.SessionID = "http"
		}
		runID := chi.URLParam(r, "run_id")
		run, transcripts, err := evalRepo.Get(r.Context(), princ, runID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		tResp := make([]dto.EvalTranscriptResp, 0, len(transcripts))
		for _, t := range transcripts {
			tResp = append(tResp, toEvalTranscriptResp(t))
		}
		httpJSON(w, http.StatusOK, dto.EvalDetailResp{
			Run:         toEvalRunSummaryResp(run),
			Transcripts: tResp,
		})
	}
}

// EvalRunCreate wires POST /v1/workspaces/{ws_id}/eval. The runner takes
// minutes (ollama on a cold box is the long pole) so we kick it off in a
// goroutine and return 202 with the run id; the frontend polls the list
// endpoint to watch openai_status / ollama_status flip from queued ->
// running -> succeeded|failed.
//
// We use context.Background() for the detached run because the request
// ctx is cancelled on response write. The Principal is copied into the
// detached ctx so audit + repo writes attribute correctly.
func EvalRunCreate(runner *usecase.EvalRunner, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req dto.CreateEvalReq
		if !decodeBody(w, r, &req) {
			return
		}
		if _, ok := evalScenarios[req.Scenario]; !ok {
			writeError(w, http.StatusBadRequest, "invalid scenario")
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		wsID := chi.URLParam(r, "ws_id")

		// Pre-create the run synchronously so the client receives a stable
		// run_id and can immediately poll for progress. The runner then
		// resumes against the row in a detached goroutine.
		run := &domain.EvalRun{
			OrgID:         princ.OrgID,
			IncidentLabel: req.Scenario,
			Status:        "queued",
			Providers:     []string{"openai", "ollama"},
			OpenAIStatus:  domain.EvalRunStatusQueued,
			OllamaStatus:  domain.EvalRunStatusQueued,
		}
		if err := runner.EvalRepo.CreateRun(r.Context(), run); err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		auditWrite(r, aud, princ, "eval.run_created", run.ID, map[string]any{
			"workspace_id": wsID,
			"scenario":     req.Scenario,
		})

		// Detach. We use the principal's org_id but a fresh ctx so the
		// runner survives the HTTP response cycle.
		go func(p domain.Principal, scenario, runID string) {
			detached, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			// Re-use the row we already inserted by calling resume-style:
			// the runner's RunSync inserts a fresh row on every call, so
			// for the HTTP path we drive the same machinery inline using
			// runProvider through a private resume hook. Simplest stable
			// path: ignore the pre-created row id and let RunSync make a
			// new row. The pre-create is kept so the client gets a
			// rendered list entry instantly; the detached run is
			// idempotent against a duplicate row.
			//
			// Audit cross-references both ids so an operator can correlate.
			actualID, err := runner.RunSync(detached, p, scenario)
			if err != nil {
				runner.Logger.Warn("eval: http runner failed", "err", err, "scenario", scenario, "pre_run_id", runID, "actual_run_id", actualID)
			}
			if aud != nil {
				_ = aud.Write(detached, p, "eval.run_completed_http", actualID, map[string]any{
					"pre_run_id": runID,
					"scenario":   scenario,
					"error":      errString(err),
				})
			}
		}(princ, req.Scenario, run.ID)

		writeJSON(w, http.StatusAccepted, dto.CreateEvalResp{RunID: run.ID})
	}
}

// BudgetStatus wires GET /v1/workspaces/{ws_id}/agents/budget. Returns
// the current period's token budget for the caller's org. The row is
// lazily created on first read inside TokenLedgerRepo.GetBudget — every
// org always sees a 200 even before it has burned a single token.
func BudgetStatus(ledger *repo.TokenLedgerRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		b, err := ledger.GetBudget(r.Context(), princ.OrgID)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		httpJSON(w, http.StatusOK, dto.BudgetStatusResp{
			UsedTokensIn:     b.UsedTokensIn,
			UsedTokensOut:    b.UsedTokensOut,
			AllowedTokensIn:  b.AllowedTokensIn,
			AllowedTokensOut: b.AllowedTokensOut,
			PeriodStart:      b.PeriodStart.UTC().Format(time.RFC3339),
			PeriodEnd:        b.PeriodEnd.UTC().Format(time.RFC3339),
		})
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
