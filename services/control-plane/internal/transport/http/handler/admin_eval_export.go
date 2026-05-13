// Package handler — admin eval-export HTTP surface (Phase 8 — public beta).
//
// One endpoint: GET /v1/admin/eval-export?format=csv. Returns the eval matrix
// for the current org as CSV. Owner-only — the matrix carries token + cost
// data that we don't expose to members.
//
// The export does NOT paginate; the eval_runs table is small (one row per
// scenario × time) so a single CSV streaming response is acceptable. We cap
// at 1000 rows defensively.
package handler

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// AdminEvalExport wires GET /v1/admin/eval-export?format=csv. Owner-only.
func AdminEvalExport(evalRepo *repo.EvalRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		format := r.URL.Query().Get("format")
		if format == "" {
			format = "csv"
		}
		if format != "csv" {
			writeError(w, http.StatusBadRequest, "format must be csv")
			return
		}
		// API-key principals may have empty SessionID; mark non-empty so
		// EvalRepo.List uses the per-request RLS tx via db.FromCtx.
		if princ.SessionID == "" {
			princ.SessionID = "http"
		}
		rows, err := evalRepo.List(r.Context(), princ, 1000, 0)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list failed")
			return
		}

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="eval-export.csv"`)
		cw := csv.NewWriter(w)
		// Header row — keep order stable so dashboards can pin column indices.
		_ = cw.Write([]string{
			"id", "scenario", "status",
			"openai_status", "ollama_status",
			"openai_tokens_in", "openai_tokens_out",
			"ollama_tokens_in", "ollama_tokens_out",
			"openai_cost_cents", "ollama_cost_cents",
			"duration_openai_ms", "duration_ollama_ms",
			"started_at", "completed_at",
			"error",
		})
		for _, run := range rows {
			completedAt := ""
			if run.CompletedAt != nil {
				completedAt = run.CompletedAt.UTC().Format(time.RFC3339)
			}
			_ = cw.Write([]string{
				run.ID,
				run.IncidentLabel,
				run.Status,
				string(run.OpenAIStatus),
				string(run.OllamaStatus),
				strconv.Itoa(run.OpenAITokensIn),
				strconv.Itoa(run.OpenAITokensOut),
				strconv.Itoa(run.OllamaTokensIn),
				strconv.Itoa(run.OllamaTokensOut),
				strconv.FormatFloat(run.OpenAICostCentsExact, 'f', 6, 64),
				strconv.FormatFloat(run.OllamaCostCentsExact, 'f', 6, 64),
				strconv.FormatInt(run.DurationOpenAIMs, 10),
				strconv.FormatInt(run.DurationOllamaMs, 10),
				run.StartedAt.UTC().Format(time.RFC3339),
				completedAt,
				run.Error,
			})
		}
		cw.Flush()
	}
}
