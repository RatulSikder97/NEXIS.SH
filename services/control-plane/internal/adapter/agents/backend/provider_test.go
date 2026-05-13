package backend

import (
	"context"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type fakeLLM struct {
	responses []domain.CompletionResponse
	calls     int
}

func (f *fakeLLM) Name() string                                   { return "openai" }
func (f *fakeLLM) Info(_ context.Context) (domain.LLMInfo, error) { return domain.LLMInfo{Provider: "openai"}, nil }
func (f *fakeLLM) Complete(_ context.Context, req domain.CompletionRequest) (domain.CompletionResponse, error) {
	idx := f.calls
	f.calls++
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	return f.responses[idx], nil
}

func TestBackend_DiffExtracted(t *testing.T) {
	body := "Here's the fix:\n\n```diff\ndiff --git a/src/x.py b/src/x.py\n--- a/src/x.py\n+++ b/src/x.py\n@@ -1,3 +1,4 @@\n existing\n+new\n```\n"
	llm := &fakeLLM{responses: []domain.CompletionResponse{{Content: body, Model: "gpt-4o"}}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 1}
	p := New(client, &agents.RetrievalClient{}, "gpt-4o")

	out, err := p.Run(context.Background(), domain.AgentInput{
		OrgID:    "org",
		Incident: &domain.IncidentPayload{Title: "x"},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Success {
		t.Fatalf("not success: %+v", out)
	}
	diff, ok := out.Structured["patch_diff"].(string)
	if !ok || !strings.Contains(diff, "+++ b/src/x.py") {
		t.Errorf("patch_diff missing or wrong: %v", out.Structured["patch_diff"])
	}
	files, ok := out.Structured["files_changed"].([]string)
	if !ok || len(files) != 1 || files[0] != "src/x.py" {
		t.Errorf("files_changed wrong: %v", out.Structured["files_changed"])
	}
}

func TestBackend_MalformedResponseFailsSchema(t *testing.T) {
	llm := &fakeLLM{responses: []domain.CompletionResponse{{Content: "no diff here", Model: "gpt-4o"}}}
	client := &agents.LLMClient{Provider: llm, SchemaRetryMax: 1}
	p := New(client, &agents.RetrievalClient{}, "gpt-4o")
	_, err := p.Run(context.Background(), domain.AgentInput{OrgID: "org", Incident: &domain.IncidentPayload{Title: "x"}})
	if err == nil {
		t.Fatalf("expected schema mismatch error")
	}
}
