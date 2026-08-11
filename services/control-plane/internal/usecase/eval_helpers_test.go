package usecase

// Coverage for the EvalRunner's pure helpers: inputSnapshot, outputSnapshot,
// copyMap, rawToIncident. The RunSync orchestration path needs an LLM
// adapter — that lives under tests/integration with mocked transports.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// TestInputSnapshot_HappyPath — every relevant field copies through, and
// optional fields are omitted when empty.
func TestInputSnapshot_HappyPath(t *testing.T) {
	in := domain.AgentInput{
		WorkflowRunID: "wf-1",
		OrgID:         "org-1",
		PromptContext: "context here",
		RepoSHA:       "abcdef",
		Incident: &domain.IncidentPayload{
			Title: "boom",
		},
		PriorOutputs: map[string]any{
			"architect": map[string]any{"plan": "..."},
		},
	}
	got := inputSnapshot(in, "you are X", "fix Y")
	if got["org_id"] != "org-1" {
		t.Fatalf("org_id: %v", got["org_id"])
	}
	if got["workflow_run_id"] != "wf-1" {
		t.Fatalf("workflow_run_id: %v", got["workflow_run_id"])
	}
	if got["prompt_context"] != "context here" {
		t.Fatalf("prompt_context: %v", got["prompt_context"])
	}
	if got["repo_sha"] != "abcdef" {
		t.Fatalf("repo_sha: %v", got["repo_sha"])
	}
	if got["system_prompt"] != "you are X" {
		t.Fatalf("system_prompt: %v", got["system_prompt"])
	}
	if got["user_prompt"] != "fix Y" {
		t.Fatalf("user_prompt: %v", got["user_prompt"])
	}
	if _, ok := got["incident"]; !ok {
		t.Fatalf("incident must be set when not nil")
	}
	if _, ok := got["prior_outputs"]; !ok {
		t.Fatalf("prior_outputs must be set when non-empty")
	}
}

// TestInputSnapshot_OmitsEmptyOptionals — nil Incident, empty PriorOutputs,
// empty prompts result in absent keys (not zero-valued keys) so the
// admin/eval UI doesn't have to render them.
func TestInputSnapshot_OmitsEmptyOptionals(t *testing.T) {
	in := domain.AgentInput{OrgID: "o", WorkflowRunID: "wf"}
	got := inputSnapshot(in, "", "")
	if _, ok := got["incident"]; ok {
		t.Fatalf("incident must be omitted when nil")
	}
	if _, ok := got["prior_outputs"]; ok {
		t.Fatalf("prior_outputs must be omitted when empty")
	}
	if _, ok := got["system_prompt"]; ok {
		t.Fatalf("system_prompt must be omitted when empty")
	}
	if _, ok := got["user_prompt"]; ok {
		t.Fatalf("user_prompt must be omitted when empty")
	}
}

// TestOutputSnapshot_CapturesEveryFieldAndErrorString — non-nil error
// string is included; structured map is preserved.
func TestOutputSnapshot_CapturesEveryFieldAndErrorString(t *testing.T) {
	o := domain.AgentOutput{
		Success: true, Content: "raw", Model: "gpt-4",
		Provider: "openai", TokensIn: 100, TokensOut: 50,
		CachedTokens: 10, CostCents: 0.42, DurationMs: 1234,
		SchemaRetries: 1,
		Structured:    map[string]any{"k": "v"},
	}
	snap := outputSnapshot(o, errors.New("boom"))
	if snap["success"] != true {
		t.Fatalf("success: %v", snap["success"])
	}
	if snap["model"] != "gpt-4" {
		t.Fatalf("model: %v", snap["model"])
	}
	if snap["provider"] != "openai" {
		t.Fatalf("provider: %v", snap["provider"])
	}
	if snap["cost_cents"] != 0.42 {
		t.Fatalf("cost: %v", snap["cost_cents"])
	}
	if snap["schema_retries"] != 1 {
		t.Fatalf("retries: %v", snap["schema_retries"])
	}
	if structured, ok := snap["structured"].(map[string]any); !ok || structured["k"] != "v" {
		t.Fatalf("structured: %+v", snap["structured"])
	}
	if snap["error"] != "boom" {
		t.Fatalf("error str: %v", snap["error"])
	}
}

// TestOutputSnapshot_NoErrorNoErrorKey — a nil error omits the error key.
func TestOutputSnapshot_NoErrorNoErrorKey(t *testing.T) {
	snap := outputSnapshot(domain.AgentOutput{Success: true}, nil)
	if _, ok := snap["error"]; ok {
		t.Fatalf("error key must be omitted on success")
	}
}

// TestOutputSnapshot_NilStructuredOmitsKey — when Structured is nil the
// snapshot leaves the key out.
func TestOutputSnapshot_NilStructuredOmitsKey(t *testing.T) {
	snap := outputSnapshot(domain.AgentOutput{Success: true, Structured: nil}, nil)
	if _, ok := snap["structured"]; ok {
		t.Fatalf("nil structured must be omitted")
	}
}

// TestCopyMap_ReturnsCleanCopy — mutating the input after the copy must
// NOT affect the output.
func TestCopyMap_ReturnsCleanCopy(t *testing.T) {
	in := map[string]any{"a": 1, "b": "two"}
	out := copyMap(in)
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("copy mismatch: in=%+v out=%+v", in, out)
	}
	in["new"] = "key"
	if _, ok := out["new"]; ok {
		t.Fatalf("copy must not share backing map")
	}
}

// TestCopyMap_NilReturnsEmpty — nil in → empty map out (so the caller
// can range over it without a nil check).
func TestCopyMap_NilReturnsEmpty(t *testing.T) {
	out := copyMap(nil)
	if out == nil || len(out) != 0 {
		t.Fatalf("expected empty map, got %+v", out)
	}
}

// TestCopyMap_EmptyReturnsEmpty — empty in → empty out.
func TestCopyMap_EmptyReturnsEmpty(t *testing.T) {
	out := copyMap(map[string]any{})
	if out == nil || len(out) != 0 {
		t.Fatalf("expected empty map, got %+v", out)
	}
}

// TestRawToIncident_NormalKeys — common field names pluck through.
func TestRawToIncident_NormalKeys(t *testing.T) {
	raw := map[string]any{
		"title":       "Null pointer",
		"service":     "orders-api",
		"environment": "prod",
		"stacktrace":  "Stack frame...",
	}
	got := rawToIncident(raw, "null-deref")
	if got == nil {
		t.Fatalf("expected non-nil incident")
	}
	if got.Title != "Null pointer" {
		t.Fatalf("title: %q", got.Title)
	}
	if got.Service != "orders-api" {
		t.Fatalf("service: %q", got.Service)
	}
	if got.Stacktrace != "Stack frame..." {
		t.Fatalf("stacktrace: %q", got.Stacktrace)
	}
	if got.Label != "null-deref" {
		t.Fatalf("label: %q", got.Label)
	}
}

// TestRawToIncident_AltKeys — when the fixture uses alternate keys
// (summary instead of title, env instead of environment), the helper
// pulls from the alternates.
func TestRawToIncident_AltKeys(t *testing.T) {
	raw := map[string]any{
		"summary":      "Boom",
		"env":          "staging",
		"service_name": "users-svc",
		"stack_trace":  "frame...",
	}
	got := rawToIncident(raw, "drift")
	if got == nil {
		t.Fatalf("expected non-nil")
	}
	if got.Title != "Boom" {
		t.Fatalf("title from summary: %q", got.Title)
	}
	if got.Environment != "staging" {
		t.Fatalf("environment from env: %q", got.Environment)
	}
	if got.Service != "users-svc" {
		t.Fatalf("service from service_name: %q", got.Service)
	}
	if got.Stacktrace != "frame..." {
		t.Fatalf("stack from stack_trace: %q", got.Stacktrace)
	}
}

// TestLoadFixtureIncidentForRunner_UnknownScenario — returns nil for an
// unmapped scenario.
func TestLoadFixtureIncidentForRunner_UnknownScenario(t *testing.T) {
	got := loadFixtureIncidentForRunner("this-scenario-does-not-exist")
	if got != nil {
		t.Fatalf("unknown scenario must return nil, got %+v", got)
	}
}

// TestPerProviderModels_OpenAIPath — OpenAI provider returns the OpenAI
// model names.
func TestPerProviderModels_OpenAIPath(t *testing.T) {
	cfg := config.Config{
		AgentModelArchitectOpenAI: "gpt-4-architect",
		AgentModelBackendOpenAI:   "gpt-4-backend",
		AgentModelQAOpenAI:        "gpt-4-qa",
		AgentModelDevOpsOpenAI:    "gpt-4-devops",
		AgentModelDataEngOpenAI:   "gpt-4-dataeng",
	}
	got := perProviderModels(cfg, "openai")
	if got.architect != "gpt-4-architect" {
		t.Fatalf("architect: %q", got.architect)
	}
	if got.backend != "gpt-4-backend" {
		t.Fatalf("backend: %q", got.backend)
	}
	if got.qa != "gpt-4-qa" {
		t.Fatalf("qa: %q", got.qa)
	}
	if got.devops != "gpt-4-devops" {
		t.Fatalf("devops: %q", got.devops)
	}
	if got.dataEng != "gpt-4-dataeng" {
		t.Fatalf("dataEng: %q", got.dataEng)
	}
}

// TestPerProviderModels_OllamaPath — Ollama provider returns the Ollama
// model names.
func TestPerProviderModels_OllamaPath(t *testing.T) {
	cfg := config.Config{
		AgentModelArchitectOllama: "ollama-architect",
		AgentModelBackendOllama:   "ollama-backend",
	}
	got := perProviderModels(cfg, "ollama")
	if got.architect != "ollama-architect" {
		t.Fatalf("ollama architect: %q", got.architect)
	}
	if got.backend != "ollama-backend" {
		t.Fatalf("ollama backend: %q", got.backend)
	}
}

// TestPerProviderModels_UnknownProviderFallsBackToOpenAI — anything other
// than "ollama" routes through the OpenAI path.
func TestPerProviderModels_UnknownProviderFallsBackToOpenAI(t *testing.T) {
	cfg := config.Config{
		AgentModelArchitectOpenAI: "default-architect",
	}
	got := perProviderModels(cfg, "anthropic") // unknown
	if got.architect != "default-architect" {
		t.Fatalf("unknown provider must fall back to OpenAI: %q", got.architect)
	}
}
