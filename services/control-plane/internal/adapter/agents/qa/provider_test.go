package qa

import (
	"context"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type fakeLLM struct {
	responses []domain.CompletionResponse
	calls     int
}

func (f *fakeLLM) Name() string { return "openai" }
func (f *fakeLLM) Info(_ context.Context) (domain.LLMInfo, error) {
	return domain.LLMInfo{Provider: "openai"}, nil
}
func (f *fakeLLM) Complete(_ context.Context, _ domain.CompletionRequest) (domain.CompletionResponse, error) {
	idx := f.calls
	f.calls++
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	return f.responses[idx], nil
}

func TestQA_HappyPath(t *testing.T) {
	body := `{"tests":{"tests/test_regression.py":"import pytest\nimport mod\ndef test_safe_div_zero():\n    with pytest.raises(ValueError):\n        mod.safe_div(1,0)\n"},"covers_files":["src/mod.py"]}`
	llm := &fakeLLM{responses: []domain.CompletionResponse{{Content: body, Model: "gpt-4o-mini"}}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 1}
	p := New(client, &agents.RetrievalClient{}, "gpt-4o-mini")

	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID:    "org",
		Incident: &domain.IncidentPayload{Title: "x"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	tests, ok := out.Structured["tests"].(map[string]any)
	if !ok || len(tests) == 0 {
		t.Fatalf("tests not populated: %v", out.Structured)
	}
}
