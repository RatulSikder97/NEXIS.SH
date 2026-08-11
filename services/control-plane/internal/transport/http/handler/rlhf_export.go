// Package handler — RLHF feedback-export HTTP surface.
//
// One endpoint: GET /v1/admin/rlhf-export. Streams the org's unexported
// feedback_examples rows as JSONL — one JSON object per line, the standard
// wire shape for LLM fine-tuning datasets:
//
//	{"scenario":"oom","patch_diff":"...","decision":"modified","modified_diff":"..."}
//
// Owner-only (same gate as admin_eval_export.go — the rows carry raw patch
// diffs we don't expose to members). The read IS the cursor advance: every
// served row is stamped exported_at=now() in the same statement, so a second
// call returns only feedback that accumulated since. Feeding the exported
// JSONL into an actual fine-tuning job is a deliberate manual step outside
// this service — the export is the pipeline's terminal artefact.
//
// Capped at 5000 rows per call defensively; re-call to drain a larger
// backlog (the cursor makes that safe).
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// rlhfExportLine is the JSONL wire shape. scenario/patch/decision are the
// training triple; modified_diff carries the human correction when the
// decision was "modified". run/decided_by/decided_at are provenance so a
// dataset can be audited back to the console.
type rlhfExportLine struct {
	Scenario      string `json:"scenario"`
	PatchDiff     string `json:"patch_diff"`
	Decision      string `json:"decision"`
	ModifiedDiff  string `json:"modified_diff,omitempty"`
	WorkflowRunID string `json:"workflow_run_id"`
	IncidentID    string `json:"incident_id,omitempty"`
	DecidedBy     string `json:"decided_by,omitempty"`
	DecidedAt     string `json:"decided_at"`
}

// RLHFExport wires GET /v1/admin/rlhf-export. Owner-only.
func RLHFExport(feedback domain.FeedbackRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		rows, err := feedback.ExportUnexported(r.Context(), princ.OrgID, 5000)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "export failed")
			return
		}

		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="rlhf-export.jsonl"`)
		enc := json.NewEncoder(w) // Encode appends the newline — exactly one object per line
		for _, fe := range rows {
			_ = enc.Encode(rlhfExportLine{
				Scenario:      fe.Scenario,
				PatchDiff:     fe.PatchDiff,
				Decision:      fe.Decision,
				ModifiedDiff:  fe.ModifiedDiff,
				WorkflowRunID: fe.WorkflowRunID,
				IncidentID:    fe.IncidentID,
				DecidedBy:     fe.DecidedBy,
				DecidedAt:     fe.DecidedAt.UTC().Format(time.RFC3339),
			})
		}
	}
}
