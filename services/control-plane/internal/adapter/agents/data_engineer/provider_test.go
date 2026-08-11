package data_engineer

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

func TestDataEng_EmptyMigrations(t *testing.T) {
	body := `{"migrations":[],"data_backfill":null}`
	llm := &fakeLLM{responses: []domain.CompletionResponse{{Content: body, Model: "gpt-4o-mini"}}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 1}
	p := New(client, &agents.RetrievalClient{}, "gpt-4o-mini")
	out, err := p.Run(context.Background(), domain.AgentInput{OrgID: "org", Incident: &domain.IncidentPayload{Title: "x"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	migs, ok := out.Structured["migrations"].([]any)
	if !ok {
		t.Fatalf("migrations missing")
	}
	if len(migs) != 0 {
		t.Errorf("expected zero migrations, got %d", len(migs))
	}
}

func TestDataEng_WithMigration(t *testing.T) {
	body := `{"migrations":[{"version":"20260513120000","name":"add_audit","up_sql":"ALTER TABLE x ADD COLUMN y INT;","down_sql":"ALTER TABLE x DROP COLUMN y;"}],"data_backfill":null}`
	llm := &fakeLLM{responses: []domain.CompletionResponse{{Content: body, Model: "gpt-4o-mini"}}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 1}
	p := New(client, &agents.RetrievalClient{}, "gpt-4o-mini")
	out, err := p.Run(context.Background(), domain.AgentInput{OrgID: "org", Incident: &domain.IncidentPayload{Title: "x"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	migs := out.Structured["migrations"].([]any)
	if len(migs) != 1 {
		t.Errorf("expected 1 migration, got %d", len(migs))
	}
}
