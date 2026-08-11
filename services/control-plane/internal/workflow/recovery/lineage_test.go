package recovery

import (
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/data_engineer"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

func migrationStructured() map[string]any {
	return map[string]any{
		"migrations": []any{
			map[string]any{
				"version":  "20260810120000",
				"name":     "widen_orders_total",
				"up_sql":   "ALTER TABLE orders ALTER COLUMN total TYPE numeric;",
				"down_sql": "ALTER TABLE orders ALTER COLUMN total TYPE text;",
			},
		},
		"data_backfill": nil,
	}
}

// TestMigrationLineageEvent_Builds — a structured output with migrations
// yields a fully-populated repo.LineageEvent carrying the OpenLineage JSON.
func TestMigrationLineageEvent_Builds(t *testing.T) {
	in := PipelineInput{OrgID: "org-1", RunID: "run-1"}
	at := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	ev, ok := migrationLineageEvent(in, migrationStructured(), data_engineer.LineageJobPropose, at)
	if !ok {
		t.Fatal("expected an event")
	}
	if ev.OrgID != "org-1" || ev.WorkflowRunID != "run-1" {
		t.Errorf("org/run = %q/%q", ev.OrgID, ev.WorkflowRunID)
	}
	if ev.EventType != "COMPLETE" || !ev.EventTime.Equal(at) {
		t.Errorf("eventType/time = %q/%v", ev.EventType, ev.EventTime)
	}
	if ev.JobNamespace != data_engineer.LineageJobNamespace || ev.JobName != data_engineer.LineageJobPropose {
		t.Errorf("job = %q/%q", ev.JobNamespace, ev.JobName)
	}
	if ev.RunID == "" {
		t.Error("run id empty")
	}
	if ev.Event["eventType"] != "COMPLETE" {
		t.Errorf("event JSON missing eventType: %+v", ev.Event)
	}
	outputs, _ := ev.Event["outputs"].([]any)
	if len(outputs) != 1 {
		t.Errorf("event outputs = %+v, want the orders dataset", ev.Event["outputs"])
	}
}

// TestMigrationLineageEvent_NoMigrations — nil / empty structured output
// means nothing to emit.
func TestMigrationLineageEvent_NoMigrations(t *testing.T) {
	cases := []struct {
		name       string
		structured map[string]any
	}{
		{"nil structured", nil},
		{"empty migrations", map[string]any{"migrations": []any{}, "data_backfill": nil}},
		{"stub payload without structured", map[string]any{"output_summary": "stubbed"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := migrationLineageEvent(PipelineInput{RunID: "r"}, tc.structured, data_engineer.LineageJobPropose, time.Now())
			if ok {
				t.Fatal("expected no event")
			}
		})
	}
}

// TestStructuredFromResult — pulls the structured submap out of a runAgent
// payload; the stub-fallback payload (no "structured" key) yields nil.
func TestStructuredFromResult(t *testing.T) {
	res := domain.ActivityResult{Payload: map[string]any{"structured": migrationStructured()}}
	if got := structuredFromResult(res); got == nil {
		t.Fatal("expected structured map")
	}
	if got := structuredFromResult(domain.ActivityResult{Payload: map[string]any{"degraded": true}}); got != nil {
		t.Fatalf("stub payload should yield nil, got %+v", got)
	}
	if got := structuredFromResult(domain.ActivityResult{}); got != nil {
		t.Fatalf("nil payload should yield nil, got %+v", got)
	}
}

// TestDataEngineerStructuredFromPrior — handles both the folded runAgent
// payload shape and the pre-flattened structured shape.
func TestDataEngineerStructuredFromPrior(t *testing.T) {
	// Folded payload shape (foldPrior stores the whole runAgent payload).
	prior := map[string]any{"data_engineer": map[string]any{"structured": migrationStructured()}}
	got := dataEngineerStructuredFromPrior(prior)
	if got == nil || got["migrations"] == nil {
		t.Fatalf("folded shape not unwrapped: %+v", got)
	}
	// Already-flattened shape.
	prior = map[string]any{"data_engineer": migrationStructured()}
	got = dataEngineerStructuredFromPrior(prior)
	if got == nil || got["migrations"] == nil {
		t.Fatalf("flattened shape not passed through: %+v", got)
	}
	// Missing key.
	if got := dataEngineerStructuredFromPrior(map[string]any{}); got != nil {
		t.Fatalf("missing prior should yield nil, got %+v", got)
	}
}
