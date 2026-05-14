package handler

// Coverage for a grab-bag of pure helpers across packages: json.go,
// pipelines.go, ops_validator.go, ops_activity.go, projects.go's safe-error
// variant.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/usecase"
)

// TestNullableTimeStr — nil → empty; non-nil → RFC3339 in UTC.
func TestNullableTimeStr(t *testing.T) {
	if got := nullableTimeStr(nil); got != "" {
		t.Fatalf("nil case: %q", got)
	}
	tm := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	if got := nullableTimeStr(&tm); got != "2026-05-13T12:00:00Z" {
		t.Fatalf("non-nil case: %q", got)
	}
}

// TestToHex — round-trip hex encoding.
func TestToHex(t *testing.T) {
	if got := toHex([]byte{0xDE, 0xAD, 0xBE, 0xEF}); got != "deadbeef" {
		t.Fatalf("toHex: %q", got)
	}
	if got := toHex(nil); got != "" {
		t.Fatalf("toHex nil: %q", got)
	}
}

// TestHttpJSON — sets content-type + status + encodes body. Nil body skips
// the encoder so callers can use it for empty 204 / 200 OK responses.
func TestHttpJSON(t *testing.T) {
	t.Run("with_body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		httpJSON(rec, http.StatusOK, map[string]string{"ok": "yes"})
		if rec.Code != http.StatusOK {
			t.Fatalf("status: %d", rec.Code)
		}
		if rec.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("content-type: %q", rec.Header().Get("Content-Type"))
		}
		var got map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got["ok"] != "yes" {
			t.Fatalf("body: %+v", got)
		}
	})
	t.Run("nil_body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		httpJSON(rec, http.StatusNoContent, nil)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status: %d", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("body must be empty for nil: %q", rec.Body.String())
		}
	})
}

// TestSafeErrorMessage — dev surfaces the underlying error; prod returns
// the canonical generic string. Nil err returns the generic string in both.
func TestSafeErrorMessage(t *testing.T) {
	t.Run("dev_surfaces", func(t *testing.T) {
		got := safeErrorMessage(errors.New("postgres timeout"), config.Config{AppEnv: "dev"}, "test.op")
		if got != "postgres timeout" {
			t.Fatalf("dev: %q", got)
		}
	})
	t.Run("prod_generic", func(t *testing.T) {
		got := safeErrorMessage(errors.New("postgres timeout"), config.Config{AppEnv: "prod"}, "test.op")
		if got != "internal server error" {
			t.Fatalf("prod: %q", got)
		}
	})
	t.Run("nil_err_generic", func(t *testing.T) {
		got := safeErrorMessage(nil, config.Config{AppEnv: "dev"}, "test.op")
		if got != "internal server error" {
			t.Fatalf("nil err: %q", got)
		}
	})
}

// TestDeriveValidatorRunStatus walks the failed > running > succeeded
// reduction.
func TestDeriveValidatorRunStatus(t *testing.T) {
	cases := []struct {
		name    string
		failed, started, succeeded bool
		want    string
	}{
		{"failed_wins", true, true, true, "failed"},
		{"running_when_started_no_succeeded", false, true, false, "running"},
		{"succeeded", false, true, true, "succeeded"},
		{"unknown_when_nothing", false, false, false, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveValidatorRunStatus(tc.failed, tc.started, tc.succeeded)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestToWorkflowRunResp — every field of the wire shape is populated, and
// "pending" placeholders are filtered out.
func TestToWorkflowRunResp(t *testing.T) {
	started := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	finished := started.Add(30 * time.Second)
	duration := int64(30000)

	t.Run("in_flight_no_completed", func(t *testing.T) {
		got := toWorkflowRunResp(domain.WorkflowRun{
			ID: "wf-1", OrgID: "org-1", WorkspaceID: "ws-1",
			WorkflowType: "RecoveryPipeline", Status: domain.WRQueued,
			StartedAt: started,
		})
		if got.ID != "wf-1" || got.Status != "queued" {
			t.Fatalf("got %+v", got)
		}
		if got.CompletedAt != "" {
			t.Fatalf("CompletedAt must be empty: %q", got.CompletedAt)
		}
	})
	t.Run("completed_with_temporal_ids", func(t *testing.T) {
		got := toWorkflowRunResp(domain.WorkflowRun{
			ID: "wf-2", Status: domain.WRSucceeded,
			StartedAt: started, CompletedAt: &finished,
			DurationMs:    &duration,
			TemporalWfID:  "twf-1",
			TemporalRunID: "trun-1",
		})
		if got.CompletedAt != "2026-05-01T12:00:30Z" {
			t.Fatalf("CompletedAt: %q", got.CompletedAt)
		}
		if got.DurationMs != duration {
			t.Fatalf("duration: %d", got.DurationMs)
		}
		if got.TemporalWorkflowID != "twf-1" {
			t.Fatalf("TemporalWorkflowID: %q", got.TemporalWorkflowID)
		}
	})
	t.Run("filters_pending_placeholders", func(t *testing.T) {
		got := toWorkflowRunResp(domain.WorkflowRun{
			TemporalWfID:  "pending",
			TemporalRunID: "pending",
		})
		if got.TemporalWorkflowID != "" {
			t.Fatalf("pending TemporalWorkflowID must be empty, got %q", got.TemporalWorkflowID)
		}
		if got.TemporalRunID != "" {
			t.Fatalf("pending TemporalRunID must be empty, got %q", got.TemporalRunID)
		}
	})
}

// TestToActivityEventResp — every field passes through; nil/empty Payload
// becomes nil map.
func TestToActivityEventResp(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	t.Run("with_payload", func(t *testing.T) {
		got := toActivityEventResp(domain.ActivityEvent{
			WorkflowRunID: "wf-1", Seq: 1, AgentRole: "backend",
			ActivityName: "synthesise", Status: "succeeded", Attempt: 1,
			Message: "ok", TS: now,
			Payload: []byte(`{"k":"v"}`),
		})
		if got.Seq != 1 || got.AgentRole != "backend" {
			t.Fatalf("got: %+v", got)
		}
		if got.Payload["k"] != "v" {
			t.Fatalf("payload: %+v", got.Payload)
		}
	})
	t.Run("empty_payload", func(t *testing.T) {
		got := toActivityEventResp(domain.ActivityEvent{TS: now})
		if got.Payload != nil {
			t.Fatalf("empty payload must stay nil, got %+v", got.Payload)
		}
	})
}

// TestMapProjectErrorSafe — env-aware error mapping for project errors.
// Sentinels map to their canonical status codes; unknown errors split on
// AppEnv.
func TestMapProjectErrorSafe(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		env        string
		wantStatus int
	}{
		{"not_found", domain.ErrNotFound, "prod", http.StatusNotFound},
		{"cap_exceeded", usecase.ErrCapExceeded, "prod", http.StatusForbidden},
		{"missing_integration", usecase.ErrIntegrationRequired, "prod", http.StatusBadRequest},
		{"invalid_env", usecase.ErrInvalidEnvironment, "prod", http.StatusBadRequest},
		{"invalid_selectors", usecase.ErrInvalidSelectors, "prod", http.StatusBadRequest},
		{"slug_alloc", usecase.ErrSlugAllocationFailed, "prod", http.StatusConflict},
		{"unknown_in_prod", errors.New("boom"), "prod", http.StatusInternalServerError},
		{"unknown_in_dev", errors.New("boom"), "dev", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mapProjectErrorSafe(rec, tc.err, config.Config{AppEnv: tc.env}, "test.op")
			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

// TestParseLimitOffsetFromStrings — pulls limit/offset from query strings
// and applies defaults / clamps. Mirrors parseLimitOffset but takes
// string params (used by the ops_activity SSE path that wraps the query
// before calling the helper).
func TestParseLimitOffsetFromStrings(t *testing.T) {
	cases := []struct {
		name       string
		limitStr   string
		offsetStr  string
		defLimit   int
		maxLimit   int
		wantLimit  int
		wantOffset int
	}{
		{"defaults", "", "", 50, 200, 50, 0},
		{"applies", "25", "10", 50, 200, 25, 10},
		{"clamps_at_max", "5000", "0", 50, 200, 200, 0},
		{"non_int_falls_back", "junk", "5", 50, 200, 50, 5},
		{"negative_offset_zero", "10", "-5", 50, 200, 10, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limit, offset := parseLimitOffsetFromStrings(tc.limitStr, tc.offsetStr, tc.defLimit, tc.maxLimit)
			if limit != tc.wantLimit {
				t.Fatalf("limit: %d want %d", limit, tc.wantLimit)
			}
			if offset != tc.wantOffset {
				t.Fatalf("offset: %d want %d", offset, tc.wantOffset)
			}
		})
	}
}
