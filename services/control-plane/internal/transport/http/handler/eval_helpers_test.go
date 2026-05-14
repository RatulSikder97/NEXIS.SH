package handler

// Coverage for the eval handler's pure helpers: costToWire, toEvalRunSummaryResp,
// normaliseStatus, toEvalTranscriptResp, orEmpty, errString.

import (
	"errors"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// TestCostToWire — the frontend expects cents × 10000. Verify the scaling
// is correct for a few representative values.
func TestCostToWire(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{0, 0},
		{0.42, 4200},
		{1.0, 10000},
		{0.0042, 42},
	}
	for _, tc := range cases {
		got := costToWire(tc.in)
		if got != tc.want {
			t.Fatalf("costToWire(%v): got %v want %v", tc.in, got, tc.want)
		}
	}
}

// TestNormaliseStatus — empty maps to queued, error maps to failed, every
// other recognised value passes through verbatim.
func TestNormaliseStatus(t *testing.T) {
	cases := []struct {
		in   domain.EvalRunStatus
		want string
	}{
		{domain.EvalRunStatusQueued, "queued"},
		{domain.EvalRunStatusRunning, "running"},
		{domain.EvalRunStatusSucceeded, "succeeded"},
		{domain.EvalRunStatusFailed, "failed"},
		{domain.EvalRunStatusError, "failed"}, // error → failed for the UI
		{"", "queued"},                         // empty → queued
		{"weird-future-state", "weird-future-state"},
	}
	for _, tc := range cases {
		got := normaliseStatus(tc.in)
		if got != tc.want {
			t.Fatalf("normaliseStatus(%q): got %q want %q", tc.in, got, tc.want)
		}
	}
}

// TestToEvalRunSummaryResp — every relevant field copies through. The
// CompletedAt branch (running run with nil completed) collapses to empty,
// while a terminal run shows RFC3339.
func TestToEvalRunSummaryResp(t *testing.T) {
	started := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	finished := started.Add(30 * time.Second)

	t.Run("in_flight_no_finished_at", func(t *testing.T) {
		r := domain.EvalRun{
			ID: "r-1", IncidentLabel: "demo",
			OpenAIStatus: domain.EvalRunStatusRunning,
			OllamaStatus: domain.EvalRunStatusRunning,
			StartedAt:    started,
		}
		got := toEvalRunSummaryResp(r)
		if got.ID != "r-1" || got.Scenario != "demo" {
			t.Fatalf("identity: %+v", got)
		}
		if got.FinishedAt != "" {
			t.Fatalf("finished must be empty for in-flight, got %q", got.FinishedAt)
		}
		if got.OpenAIStatus != "running" {
			t.Fatalf("openai status: %q", got.OpenAIStatus)
		}
	})
	t.Run("terminal_finished_at_set", func(t *testing.T) {
		r := domain.EvalRun{
			ID: "r-2", IncidentLabel: "demo",
			OpenAIStatus:         domain.EvalRunStatusSucceeded,
			OllamaStatus:         domain.EvalRunStatusFailed,
			OpenAICostCentsExact: 0.42,
			OpenAITokensIn:       100, OpenAITokensOut: 50,
			DurationOpenAIMs: 5000,
			StartedAt:        started,
			CompletedAt:      &finished,
		}
		got := toEvalRunSummaryResp(r)
		if got.FinishedAt != "2026-05-01T12:00:30Z" {
			t.Fatalf("finished_at: %q", got.FinishedAt)
		}
		// cost is scaled by 10000.
		if got.OpenAICostCentsExact != 4200 {
			t.Fatalf("openai cost: %v", got.OpenAICostCentsExact)
		}
		if got.OllamaStatus != "failed" {
			t.Fatalf("ollama status: %q", got.OllamaStatus)
		}
	})
}

// TestToEvalTranscriptResp — the per-cell wire shape preserves the
// transcript's contents and surfaces an empty map for nil InputJSON /
// OutputJSON so the frontend's type guard works.
func TestToEvalTranscriptResp(t *testing.T) {
	tr := domain.EvalTranscript{
		ID: "t-1", EvalRunID: "r-1", Provider: "openai",
		Agent: domain.AgentNameArchitect, Model: "gpt-4",
		InputJSON:  map[string]any{"prompt": "x"},
		OutputJSON: nil, // expect empty map via orEmpty
		Success:    true,
		TokensIn:   200,
		TokensOut:  100,
		CostCents:  0.01,
		DurationMs: 1234,
		StartedAt:  time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 5, 1, 12, 0, 1, 0, time.UTC),
	}
	got := toEvalTranscriptResp(tr)
	if got.ID != "t-1" || got.EvalRunID != "r-1" {
		t.Fatalf("identity: %+v", got)
	}
	if got.Provider != "openai" {
		t.Fatalf("provider: %q", got.Provider)
	}
	if got.InputJSON == nil || got.InputJSON["prompt"] != "x" {
		t.Fatalf("input_json: %+v", got.InputJSON)
	}
	if got.OutputJSON == nil {
		t.Fatalf("output_json must be empty map, not nil")
	}
	if len(got.OutputJSON) != 0 {
		t.Fatalf("output_json: %+v", got.OutputJSON)
	}
	// Cost is scaled by 10000.
	if got.CostCentsExact != 100 {
		t.Fatalf("cost: %v", got.CostCentsExact)
	}
}

// TestOrEmpty — nil returns empty map; non-nil passes through.
func TestOrEmpty(t *testing.T) {
	if got := orEmpty(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil case: %+v", got)
	}
	in := map[string]any{"k": "v"}
	if got := orEmpty(in); got["k"] != "v" {
		t.Fatalf("passthrough: %+v", got)
	}
}

// TestErrString — nil → empty; error → .Error() body.
func TestErrString(t *testing.T) {
	if got := errString(nil); got != "" {
		t.Fatalf("nil err: %q", got)
	}
	if got := errString(errors.New("boom")); got != "boom" {
		t.Fatalf("err: %q", got)
	}
}
