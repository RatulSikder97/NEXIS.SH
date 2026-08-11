package handler

// Coverage for the RLHF JSONL export handler: one JSON object per line in
// the fine-tuning shape, the org scoping (the principal's org drives the
// repo call), modified_diff presence only on modified decisions, and the
// auth guard.

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

type fakeFeedbackExporter struct {
	gotOrg   string
	gotLimit int
	rows     []domain.FeedbackExample
	err      error
}

func (f *fakeFeedbackExporter) Insert(_ context.Context, _ domain.FeedbackExample) (string, error) {
	return "", nil
}

func (f *fakeFeedbackExporter) ExportUnexported(_ context.Context, orgID string, limit int) ([]domain.FeedbackExample, error) {
	f.gotOrg = orgID
	f.gotLimit = limit
	return f.rows, f.err
}

func rlhfRequest(orgID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/admin/rlhf-export", nil)
	if orgID != "" {
		r = r.WithContext(appmw.WithPrincipal(r.Context(), domain.Principal{
			OrgID: orgID, UserID: "user-1", Role: domain.RoleOwner, SessionID: "sess-1",
		}))
	}
	return r
}

func TestRLHFExport_StreamsJSONLAndScopesToOrg(t *testing.T) {
	decidedAt := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	fake := &fakeFeedbackExporter{rows: []domain.FeedbackExample{
		{
			Scenario: "oom", PatchDiff: "+orig", Decision: "modified",
			ModifiedDiff: "+edited", WorkflowRunID: "run-1",
			IncidentID: "inc-1", DecidedBy: "user-2", DecidedAt: decidedAt,
		},
		{
			Scenario: "null_deref", PatchDiff: "+fix", Decision: "approved",
			WorkflowRunID: "run-2", DecidedAt: decidedAt,
		},
	}}
	rec := httptest.NewRecorder()
	RLHFExport(fake)(rec, rlhfRequest("org-9"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.gotOrg != "org-9" {
		t.Fatalf("export must be scoped to the principal's org, got %q", fake.gotOrg)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "ndjson") {
		t.Fatalf("content type must be ndjson, got %q", ct)
	}

	// Exactly one JSON object per line, decodable independently.
	scanner := bufio.NewScanner(rec.Body)
	var lines []map[string]any
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("line is not standalone JSON: %q (%v)", line, err)
		}
		lines = append(lines, obj)
	}
	if len(lines) != 2 {
		t.Fatalf("want 2 JSONL lines, got %d", len(lines))
	}

	first := lines[0]
	if first["scenario"] != "oom" || first["decision"] != "modified" || first["modified_diff"] != "+edited" {
		t.Fatalf("modified row wrong: %v", first)
	}
	if first["decided_at"] != "2026-08-10T12:00:00Z" {
		t.Fatalf("decided_at must be RFC3339, got %v", first["decided_at"])
	}

	second := lines[1]
	if second["decision"] != "approved" {
		t.Fatalf("approved row wrong: %v", second)
	}
	if _, present := second["modified_diff"]; present {
		t.Fatalf("modified_diff must be omitted on non-modified decisions: %v", second)
	}
}

func TestRLHFExport_UnauthorizedWithoutPrincipal(t *testing.T) {
	rec := httptest.NewRecorder()
	RLHFExport(&fakeFeedbackExporter{})(rec, rlhfRequest(""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing principal must 401, got %d", rec.Code)
	}
}

func TestRLHFExport_RepoErrorIs500(t *testing.T) {
	rec := httptest.NewRecorder()
	RLHFExport(&fakeFeedbackExporter{err: context.DeadlineExceeded})(rec, rlhfRequest("org-1"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("repo failure must 500, got %d", rec.Code)
	}
}
