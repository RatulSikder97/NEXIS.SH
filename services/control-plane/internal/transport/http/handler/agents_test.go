package handler

// Unit coverage for the agents handler's helper functions. The DB-bound
// list/events endpoints live under tests/integration; here we cover the
// pure helpers plus the status-derivation predicate that drove the Wave 1
// "succeeded filter" regression.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestParseLimitOffset walks every branch of the URL-query parser. Defaults
// apply when the query is empty; clamps engage on out-of-bounds values;
// non-integers are silently ignored (defaults take over).
func TestParseLimitOffset(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		defLimit   int
		maxLimit   int
		wantLimit  int
		wantOffset int
	}{
		{
			name:       "defaults_when_empty",
			query:      "",
			defLimit:   50,
			maxLimit:   200,
			wantLimit:  50,
			wantOffset: 0,
		},
		{
			name:       "respects_values",
			query:      "limit=25&offset=10",
			defLimit:   50,
			maxLimit:   200,
			wantLimit:  25,
			wantOffset: 10,
		},
		{
			name:       "limit_clamped_at_max",
			query:      "limit=500&offset=0",
			defLimit:   50,
			maxLimit:   200,
			wantLimit:  200,
			wantOffset: 0,
		},
		{
			name:       "non_int_limit_falls_back",
			query:      "limit=junk&offset=5",
			defLimit:   50,
			maxLimit:   200,
			wantLimit:  50,
			wantOffset: 5,
		},
		{
			name:       "negative_limit_falls_back_to_default",
			query:      "limit=-3&offset=0",
			defLimit:   50,
			maxLimit:   200,
			wantLimit:  50,
			wantOffset: 0,
		},
		{
			name:       "negative_offset_falls_back_to_zero",
			query:      "limit=10&offset=-5",
			defLimit:   50,
			maxLimit:   200,
			wantLimit:  10,
			wantOffset: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/runs?"+tc.query, nil)
			limit, offset := parseLimitOffset(req, tc.defLimit, tc.maxLimit)
			if limit != tc.wantLimit {
				t.Fatalf("limit: got %d want %d", limit, tc.wantLimit)
			}
			if offset != tc.wantOffset {
				t.Fatalf("offset: got %d want %d", offset, tc.wantOffset)
			}
		})
	}
}

// TestDeriveAgentRunStatus encodes the documented precedence:
// failed > running > degraded > succeeded.
//
// This is also the regression fixture for the Wave 1 bug where the
// AgentsList filter on status="succeeded" was inadvertently broken — the
// derive order had to be locked in so a degraded finish doesn't masquerade
// as succeeded.
func TestDeriveAgentRunStatus(t *testing.T) {
	cases := []struct {
		name      string
		failed    bool
		started   bool
		succeeded bool
		degraded  bool
		want      string
	}{
		{"anyFailed_beats_others", true, true, true, true, "failed"},
		{"started_no_succeeded_running", false, true, false, false, "running"},
		{"started_and_succeeded_not_running", false, true, true, false, "succeeded"},
		{"degraded_only_when_no_failed_no_running", false, true, true, true, "degraded"},
		{"all_false_default_succeeded", false, false, false, false, "succeeded"},
		{"started_no_succeeded_yes_degraded_still_running", false, true, false, true, "running"},
		{"failed_overrides_degraded", true, false, false, true, "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveAgentRunStatus(tc.failed, tc.started, tc.succeeded, tc.degraded)
			if got != tc.want {
				t.Fatalf("derive: got %q want %q", got, tc.want)
			}
		})
	}
}

// TestAgentsListEndpoint_StatusSucceededFilter is the regression test for the
// bug Wave 1 fixed: the agent status set must include "succeeded" as the
// neutral fallback when no failed/running/degraded signal is present. Here
// we drive deriveAgentRunStatus with the canonical succeeded shape and
// confirm the wire status comes back as "succeeded".
func TestAgentsListEndpoint_StatusSucceededFilter(t *testing.T) {
	// A run with at least one started + one succeeded frame and no failed /
	// degraded markers must surface as succeeded — NOT as running.
	got := deriveAgentRunStatus(false, true, true, false)
	if got != "succeeded" {
		t.Fatalf("regression: succeeded path returned %q want succeeded", got)
	}
}

// TestTruncateLargeOutput shrinks oversize string fields. Each rawKey is
// truncated independently and stamped with _truncated + _original_bytes
// siblings so the UI can show the truncation banner.
func TestTruncateLargeOutput(t *testing.T) {
	big := strings.Repeat("A", 9000)
	payload := map[string]any{
		"content":      big,
		"model_output": big,
		"summary":      "untouched",
		"tool_calls":   []any{"keep me intact"},
	}
	truncateLargeOutput(payload, 8*1024)

	contentStr, ok := payload["content"].(string)
	if !ok {
		t.Fatalf("content type changed: %T", payload["content"])
	}
	if !strings.HasSuffix(contentStr, "... [truncated]") {
		t.Fatalf("content not truncated: %q", contentStr[len(contentStr)-32:])
	}
	if payload["content_truncated"] != true {
		t.Fatalf("content_truncated flag missing")
	}
	if payload["content_original_bytes"] != 9000 {
		t.Fatalf("content_original_bytes: got %v want 9000", payload["content_original_bytes"])
	}
	// model_output should also be truncated.
	if _, ok := payload["model_output_truncated"]; !ok {
		t.Fatalf("model_output_truncated flag missing")
	}
	// summary is not a rawKey and must be untouched.
	if payload["summary"] != "untouched" {
		t.Fatalf("summary mutated: %v", payload["summary"])
	}
	// tool_calls is not a string; the helper must skip it without panicking.
	if _, ok := payload["tool_calls"].([]any); !ok {
		t.Fatalf("tool_calls mutated: %T", payload["tool_calls"])
	}
}

// TestTruncateLargeOutput_NoOpUnderThreshold — strings under the threshold
// pass through unchanged with no companion flags.
func TestTruncateLargeOutput_NoOpUnderThreshold(t *testing.T) {
	short := "small content"
	payload := map[string]any{"content": short}
	truncateLargeOutput(payload, 1024)
	if payload["content"] != short {
		t.Fatalf("short content mutated: %v", payload["content"])
	}
	if _, ok := payload["content_truncated"]; ok {
		t.Fatalf("under-threshold should NOT set _truncated flag")
	}
}

// TestLoadAgentRuns_NilPoolReturnsEmpty exercises the nil-pool guard. Used
// for the dev/no-DB boot path so tests that don't wire a pool still get a
// sane (empty) response instead of a panic.
func TestLoadAgentRuns_NilPoolReturnsEmpty(t *testing.T) {
	runs, total, err := loadAgentRuns(httptest.NewRequest(http.MethodGet, "/", nil).Context(), nil, "org", "ws", "backend", 50, 0)
	if err != nil {
		t.Fatalf("nil pool err: %v", err)
	}
	if total != 0 {
		t.Fatalf("total: got %d want 0", total)
	}
	if runs == nil {
		t.Fatalf("runs must be non-nil empty slice for JSON marshalling")
	}
	if len(runs) != 0 {
		t.Fatalf("runs len: %d", len(runs))
	}
}

// TestLoadAgentRunEvents_NilPoolReturnsEmpty — same nil-pool guarantee for
// the events endpoint.
func TestLoadAgentRunEvents_NilPoolReturnsEmpty(t *testing.T) {
	events, err := loadAgentRunEvents(httptest.NewRequest(http.MethodGet, "/", nil).Context(), nil, "org", "ws", "run-1", "backend")
	if err != nil {
		t.Fatalf("nil pool err: %v", err)
	}
	if events == nil {
		t.Fatalf("events must be non-nil empty slice")
	}
}

// TestLoadAgentStats_NilPoolReturnsEmpty — likewise for the stats query.
func TestLoadAgentStats_NilPoolReturnsEmpty(t *testing.T) {
	since := time.Now().Add(-7 * 24 * time.Hour)
	out := loadAgentStats(httptest.NewRequest(http.MethodGet, "/", nil).Context(), nil, "org-1", since)
	if out == nil {
		t.Fatalf("stats map must be non-nil empty")
	}
	if len(out) != 0 {
		t.Fatalf("stats len: %d", len(out))
	}
}
