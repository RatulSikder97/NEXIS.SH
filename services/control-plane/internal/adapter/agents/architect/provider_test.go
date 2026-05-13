package architect

import (
	"context"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeLLM is a tiny stub provider used by the agent tests. Returns
// sequential canned responses; tracks call count for retry assertions.
type fakeLLM struct {
	responses []domain.CompletionResponse
	errs      []error
	calls     int
	lastReq   domain.CompletionRequest
}

func (f *fakeLLM) Name() string                           { return "openai" }
func (f *fakeLLM) Info(_ context.Context) (domain.LLMInfo, error) { return domain.LLMInfo{Provider: "openai"}, nil }
func (f *fakeLLM) Complete(_ context.Context, req domain.CompletionRequest) (domain.CompletionResponse, error) {
	idx := f.calls
	f.lastReq = req
	f.calls++
	if idx < len(f.errs) && f.errs[idx] != nil {
		return domain.CompletionResponse{}, f.errs[idx]
	}
	if idx < len(f.responses) {
		return f.responses[idx], nil
	}
	return f.responses[len(f.responses)-1], nil
}

func TestArchitect_HappyPath(t *testing.T) {
	good := domain.CompletionResponse{
		Content: `{"plan_steps":[{"id":"p1","description":"guard div by zero","files":["src/x.py"],"rationale":"matches trace"}],"affected_files":["src/x.py"],"risk_level":"low"}`,
		Model:   "gpt-4o-mini", InputTokens: 1200, OutputTokens: 300, CachedTokens: 800, CostCents: 0.42, DurationMs: 1200,
	}
	llm := &fakeLLM{responses: []domain.CompletionResponse{good}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 2}
	p := New(client, &agents.RetrievalClient{}, "gpt-4o-mini")

	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID: "org-1",
		Incident: &domain.IncidentPayload{
			Title: "TypeError", Service: "fixture", Environment: "prod",
			Stacktrace: "ZeroDivisionError",
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success, got %+v", out)
	}
	if out.TokensIn != 1200 || out.TokensOut != 300 {
		t.Errorf("tokens not forwarded: %+v", out)
	}
	steps, ok := out.Structured["plan_steps"].([]any)
	if !ok || len(steps) == 0 {
		t.Errorf("plan_steps not populated: %+v", out.Structured)
	}
	if !strings.Contains(llm.lastReq.System, "Architect") {
		t.Errorf("system prompt missing architect: %q", llm.lastReq.System)
	}
	if !llm.lastReq.JSONResponse {
		t.Errorf("expected JSONResponse=true")
	}
	if !llm.lastReq.CacheSystem {
		t.Errorf("expected CacheSystem=true")
	}
}

func TestArchitect_SchemaRetryThenSuccess(t *testing.T) {
	bad := domain.CompletionResponse{Content: `not json`, Model: "gpt-4o-mini"}
	good := domain.CompletionResponse{
		Content: `{"plan_steps":[{"id":"p1","description":"d","files":["f"],"rationale":"r"}],"affected_files":["f"],"risk_level":"low"}`,
		Model:   "gpt-4o-mini",
	}
	llm := &fakeLLM{responses: []domain.CompletionResponse{bad, good}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 2}
	p := New(client, &agents.RetrievalClient{}, "gpt-4o-mini")

	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID:    "org",
		Incident: &domain.IncidentPayload{Title: "x"},
	})
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if !out.Success {
		t.Fatalf("not success: %+v", out)
	}
	if out.SchemaRetries != 1 {
		t.Errorf("expected SchemaRetries=1, got %d", out.SchemaRetries)
	}
	if llm.calls != 2 {
		t.Errorf("expected 2 calls, got %d", llm.calls)
	}
}

func TestArchitect_SchemaRetryExhausted(t *testing.T) {
	bad := domain.CompletionResponse{Content: `not json`, Model: "gpt-4o-mini"}
	llm := &fakeLLM{responses: []domain.CompletionResponse{bad}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 2}
	p := New(client, &agents.RetrievalClient{}, "gpt-4o-mini")

	_, err := p.Run(context.Background(), domain.AgentInput{
		OrgID:    "org",
		Incident: &domain.IncidentPayload{Title: "x"},
	})
	if err == nil {
		t.Fatalf("expected error after retry exhaustion")
	}
	if llm.calls != 3 { // 1 initial + 2 retries
		t.Errorf("expected 3 calls, got %d", llm.calls)
	}
}
