package synthesiser

import (
	"context"
	"errors"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeLLM is a sequential-canned-response stub, shared with the L1 agent
// test pattern.
type fakeLLM struct {
	responses []domain.CompletionResponse
	errs      []error
	calls     int
	last      domain.CompletionRequest
}

func (f *fakeLLM) Name() string { return "openai" }
func (f *fakeLLM) Info(_ context.Context) (domain.LLMInfo, error) {
	return domain.LLMInfo{Provider: "openai"}, nil
}
func (f *fakeLLM) Complete(_ context.Context, req domain.CompletionRequest) (domain.CompletionResponse, error) {
	f.last = req
	idx := f.calls
	f.calls++
	if idx < len(f.errs) && f.errs[idx] != nil {
		return domain.CompletionResponse{}, f.errs[idx]
	}
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	return f.responses[idx], nil
}

// pathfinderPrior builds a PriorOutputs map that mirrors what the
// workflow constructs after the Pathfinder activity completes. The
// Structured payload is wrapped under the "pathfinder" key.
func pathfinderPrior(structured map[string]any) map[string]any {
	return map[string]any{
		"pathfinder": structured,
	}
}

// TestSynthesiser_FastPath_SchemaDrift — Pathfinder evidence contains a
// psycopg2 column error. Synthesiser must pick schema_drift and route
// to {architect, data_engineer, backend, qa} without invoking the LLM.
func TestSynthesiser_FastPath_SchemaDrift(t *testing.T) {
	llm := &fakeLLM{}
	client := &agents.LLMClient{Provider: llm}
	p := New(client, "gpt-4o-mini")

	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID: "org",
		PriorOutputs: pathfinderPrior(map[string]any{
			"hypothesis":     "users.email_verified_at missing",
			"confidence":     0.91,
			"evidence_chain": []any{"UndefinedColumn", "psycopg2.errors"},
		}),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if scen := out.Structured["scenario"].(string); scen != "schema_drift" {
		t.Errorf("expected schema_drift, got %q", scen)
	}
	if src := out.Structured["source"].(string); src != "fast_path" {
		t.Errorf("expected source=fast_path, got %q", src)
	}
	sel := out.Structured["selected_agents"].([]string)
	want := []string{"architect", "data_engineer", "backend", "qa"}
	if len(sel) != len(want) {
		t.Fatalf("expected %d selected agents, got %d (%v)", len(want), len(sel), sel)
	}
	for i, w := range want {
		if sel[i] != w {
			t.Errorf("position %d: expected %q got %q (full=%v)", i, w, sel[i], sel)
		}
	}
	skipped := out.Structured["skipped_agents"].([]string)
	for _, s := range skipped {
		if s == "data_engineer" || s == "architect" || s == "backend" || s == "qa" {
			t.Errorf("agent %q should not be in skipped: %v", s, skipped)
		}
	}
	if llm.calls != 0 {
		t.Errorf("fast path should not call LLM, got %d calls", llm.calls)
	}
}

// TestSynthesiser_FastPath_NullDeref — NoneType attribute trace. Must
// route to null_deref → {architect, backend, qa}.
func TestSynthesiser_FastPath_NullDeref(t *testing.T) {
	p := New(nil, "")
	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID: "org",
		PriorOutputs: pathfinderPrior(map[string]any{
			"hypothesis":     "render attribute on None",
			"confidence":     0.78,
			"evidence_chain": []any{"AttributeError", "NoneType has no attribute profile"},
		}),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if scen := out.Structured["scenario"].(string); scen != "null_deref" {
		t.Errorf("expected null_deref, got %q (rationale=%q)", scen, out.Structured["rationale"])
	}
	sel := out.Structured["selected_agents"].([]string)
	want := []string{"architect", "backend", "qa"}
	for i, w := range want {
		if sel[i] != w {
			t.Errorf("position %d: expected %q got %q (full=%v)", i, w, sel[i], sel)
		}
	}
	skipped := out.Structured["skipped_agents"].([]string)
	hasData, hasDevOps := false, false
	for _, s := range skipped {
		if s == "data_engineer" {
			hasData = true
		}
		if s == "devops" {
			hasDevOps = true
		}
	}
	if !hasData || !hasDevOps {
		t.Errorf("expected data_engineer + devops in skipped, got %v", skipped)
	}
}

// TestSynthesiser_FastPath_OOM — MemoryError + OOMKilled. Routes to
// {architect, devops, backend}.
func TestSynthesiser_FastPath_OOM(t *testing.T) {
	p := New(nil, "")
	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID: "org",
		PriorOutputs: pathfinderPrior(map[string]any{
			"hypothesis":     "container OOMKilled under load",
			"confidence":     0.84,
			"evidence_chain": []any{"MemoryError", "OOMKilled"},
		}),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if scen := out.Structured["scenario"].(string); scen != "oom" {
		t.Errorf("expected oom, got %q", scen)
	}
	sel := out.Structured["selected_agents"].([]string)
	want := []string{"architect", "devops", "backend"}
	for i, w := range want {
		if sel[i] != w {
			t.Errorf("position %d: expected %q got %q (full=%v)", i, w, sel[i], sel)
		}
	}
}

// TestSynthesiser_LLMFallback_UnknownToScenario — Pathfinder evidence has
// nothing the fast path recognises, so we call the LLM. LLM returns
// schema_drift and Synthesiser routes accordingly.
func TestSynthesiser_LLMFallback_UnknownToScenario(t *testing.T) {
	llm := &fakeLLM{responses: []domain.CompletionResponse{
		{
			Content:      `{"scenario":"schema_drift","confidence":0.6,"rationale":"model believes column missing"}`,
			Model:        "gpt-4o-mini",
			InputTokens:  80,
			OutputTokens: 30,
			CostCents:    0.05,
		},
	}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 1}
	p := New(client, "gpt-4o-mini")

	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID: "org",
		PriorOutputs: pathfinderPrior(map[string]any{
			"hypothesis":     "some weird intermittent latency spike",
			"confidence":     0.4,
			"evidence_chain": []any{"latency_p99=2400ms", "no_obvious_signal"},
		}),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if scen := out.Structured["scenario"].(string); scen != "schema_drift" {
		t.Errorf("expected schema_drift from LLM, got %q", scen)
	}
	if src := out.Structured["source"].(string); src != "llm" {
		t.Errorf("expected source=llm, got %q", src)
	}
	if llm.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", llm.calls)
	}
	if !llm.last.JSONResponse {
		t.Errorf("expected JSONResponse=true on classifier call")
	}
	if !llm.last.CacheSystem {
		t.Errorf("expected CacheSystem=true (prompt caching)")
	}
	conf, _ := out.Structured["confidence"].(float64)
	if conf < 0.59 || conf > 0.61 {
		t.Errorf("expected confidence ~0.6 from LLM, got %v", conf)
	}
}

// TestSynthesiser_LLMFallback_ProviderError — LLM stub errors, agent must
// fall back to scenario:"unknown" with source:"fallback" and a non-empty
// selected_agents list (architect, backend, qa).
func TestSynthesiser_LLMFallback_ProviderError(t *testing.T) {
	llm := &fakeLLM{
		responses: []domain.CompletionResponse{{}},
		errs:      []error{errors.New("provider 503")},
	}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 0}
	p := New(client, "gpt-4o-mini")

	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID: "org",
		PriorOutputs: pathfinderPrior(map[string]any{
			"hypothesis":     "unclear",
			"evidence_chain": []any{"???"},
		}),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if scen := out.Structured["scenario"].(string); scen != "unknown" {
		t.Errorf("expected unknown on LLM error, got %q", scen)
	}
	if src := out.Structured["source"].(string); src != "fallback" {
		t.Errorf("expected source=fallback, got %q", src)
	}
	sel := out.Structured["selected_agents"].([]string)
	if len(sel) == 0 {
		t.Errorf("selected_agents must remain non-empty on fallback")
	}
}

// TestSynthesiser_NoLLM_FastPathMiss — no LLM configured + no fast-path
// match. Must still return scenario:"unknown" with source:"fallback".
func TestSynthesiser_NoLLM_FastPathMiss(t *testing.T) {
	p := New(nil, "")
	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID: "org",
		PriorOutputs: pathfinderPrior(map[string]any{
			"hypothesis":     "??",
			"evidence_chain": []any{"weird_signal"},
		}),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if scen := out.Structured["scenario"].(string); scen != "unknown" {
		t.Errorf("expected unknown, got %q", scen)
	}
	if src := out.Structured["source"].(string); src != "fallback" {
		t.Errorf("expected source=fallback, got %q", src)
	}
}

// TestSynthesiser_NoPathfinderInput — workflow somehow omits the
// Pathfinder output. Synthesiser must not panic; routes to unknown.
func TestSynthesiser_NoPathfinderInput(t *testing.T) {
	p := New(nil, "")
	out, err := p.Run(context.Background(), domain.AgentInput{OrgID: "org"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Structured["scenario"].(string) != "unknown" {
		t.Errorf("expected unknown when pathfinder absent")
	}
}

// TestSynthesiser_FastPathPriorityOverLLM — even with an LLM available,
// fast-path matches must short-circuit. Cost telemetry should be zero.
func TestSynthesiser_FastPathPriorityOverLLM(t *testing.T) {
	llm := &fakeLLM{responses: []domain.CompletionResponse{
		{Content: `{"scenario":"oom","confidence":0.99,"rationale":"x"}`, Model: "gpt-4o-mini"},
	}}
	client := &agents.LLMClient{Provider: llm}
	p := New(client, "gpt-4o-mini")

	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID: "org",
		PriorOutputs: pathfinderPrior(map[string]any{
			"evidence_chain": []any{"NoneType has no attribute"},
		}),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if scen := out.Structured["scenario"].(string); scen != "null_deref" {
		t.Errorf("fast path must beat LLM; got %q", scen)
	}
	if llm.calls != 0 {
		t.Errorf("LLM should not be invoked when fast path matches; calls=%d", llm.calls)
	}
	if out.CostCents != 0 {
		t.Errorf("fast-path cost should be zero, got %v", out.CostCents)
	}
}

// TestSynthesiser_LLM_BadSchemaFallsBack — model returns junk → schema
// retries exhausted → agent routes to unknown. We deliberately set
// SchemaRetryMax=0 so the helper makes exactly one attempt before giving up.
func TestSynthesiser_LLM_BadSchemaFallsBack(t *testing.T) {
	llm := &fakeLLM{responses: []domain.CompletionResponse{
		{Content: `not json`, Model: "gpt-4o-mini"},
	}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 0}
	p := New(client, "gpt-4o-mini")

	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID: "org",
		PriorOutputs: pathfinderPrior(map[string]any{
			"hypothesis":     "?",
			"evidence_chain": []any{"opaque"},
		}),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if scen := out.Structured["scenario"].(string); scen != "unknown" {
		t.Errorf("expected unknown after schema mismatch, got %q", scen)
	}
	if src := out.Structured["source"].(string); src != "fallback" {
		t.Errorf("expected source=fallback, got %q", src)
	}
}
