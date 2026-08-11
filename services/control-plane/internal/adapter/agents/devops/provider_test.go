package devops

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

func TestDevOps_HappyPath(t *testing.T) {
	body := `{"argocd_app_yaml":"apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: foo","gh_actions_yaml":"name: ci\non: [pull_request]\njobs: { build: { runs-on: ubuntu-latest, steps: [{ uses: actions/checkout@v4 }] } }","rollout_strategy":"canary-10-50-100"}`
	llm := &fakeLLM{responses: []domain.CompletionResponse{{Content: body, Model: "gpt-4o-mini"}}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 1}
	p := New(client, &agents.RetrievalClient{}, "gpt-4o-mini")

	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID:    "org",
		Incident: &domain.IncidentPayload{Title: "x", Service: "fixture", Environment: "prod"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := out.Structured["argocd_app_yaml"].(string); !ok {
		t.Errorf("argocd_app_yaml missing")
	}
}
