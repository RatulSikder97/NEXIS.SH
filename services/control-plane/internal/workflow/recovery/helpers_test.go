package recovery

// Coverage for the workflow/recovery package's pure helpers:
// isBudgetExceeded, buildAgentContext, summariseAgentOutput. The temporal-
// bound activities + workflow body need a Temporal test environment; those
// run under workflow_test.go already.

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// TestIsBudgetExceeded — recognises ErrBudgetExceeded directly + when
// wrapped in a chain.
func TestIsBudgetExceeded(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		if !isBudgetExceeded(domain.ErrBudgetExceeded) {
			t.Fatalf("direct must be recognised")
		}
	})
	t.Run("wrapped", func(t *testing.T) {
		err := fmt.Errorf("agent run: %w", domain.ErrBudgetExceeded)
		if !isBudgetExceeded(err) {
			t.Fatalf("wrapped must be recognised")
		}
	})
	t.Run("unrelated", func(t *testing.T) {
		if isBudgetExceeded(errors.New("postgres timeout")) {
			t.Fatalf("unrelated must NOT match")
		}
	})
	t.Run("nil", func(t *testing.T) {
		if isBudgetExceeded(nil) {
			t.Fatalf("nil err must NOT match")
		}
	})
}

// TestBuildAgentContext_FromProject — when a project is bound, the
// selectors map populates the context with every project field.
func TestBuildAgentContext_FromProject(t *testing.T) {
	in := PipelineInput{
		Project: &domain.Project{
			ID: "proj-1", Slug: "orders", Environment: domain.EnvironmentProd,
			Selectors: domain.ProjectSelectors{
				GitHubRepo:            "acme/orders-api",
				GitHubDefaultBranch:   "main",
				GitHubInstallationID:  12345,
				ArgoCDAppName:         "orders-prod",
				ArgoCDProject:         "default",
			},
		},
	}
	got := buildAgentContext(in)
	if got["project_id"] != "proj-1" {
		t.Fatalf("project_id: %v", got["project_id"])
	}
	if got["github_repo"] != "acme/orders-api" {
		t.Fatalf("github_repo: %v", got["github_repo"])
	}
	if got["github_default_branch"] != "main" {
		t.Fatalf("default_branch: %v", got["github_default_branch"])
	}
	if got["github_installation_id"] != int64(12345) {
		t.Fatalf("installation_id: %v", got["github_installation_id"])
	}
	if got["argocd_app_name"] != "orders-prod" {
		t.Fatalf("argocd_app: %v", got["argocd_app_name"])
	}
	if got["environment"] != string(domain.EnvironmentProd) {
		t.Fatalf("env: %v", got["environment"])
	}
}

// TestSummariseAgentOutput_BackendDiffShape — backend agent produces a
// "Unified diff: N lines (T tokens, model)" summary.
func TestSummariseAgentOutput_BackendDiffShape(t *testing.T) {
	out := domain.AgentOutput{
		Structured: map[string]any{
			"patch_diff": "diff --git a/x b/x\nindex 1..2\n+new line",
		},
		TokensOut: 100,
		Model:     "gpt-4",
	}
	got := summariseAgentOutput(domain.AgentBackend, out)
	if got != "Unified diff: 3 lines (100 tokens, gpt-4)" {
		t.Fatalf("backend summary: %q", got)
	}
}

// TestSummariseAgentOutput_QATestCount — QA agent produces a "Generated N
// test cases" summary.
func TestSummariseAgentOutput_QATestCount(t *testing.T) {
	out := domain.AgentOutput{
		Structured: map[string]any{
			"test_cases": []any{"t1", "t2", "t3"},
		},
		TokensOut: 50,
		Model:     "ollama",
	}
	got := summariseAgentOutput(domain.AgentQA, out)
	if got != "Generated 3 test cases (50 tokens, ollama)" {
		t.Fatalf("qa summary: %q", got)
	}
}

// TestSummariseAgentOutput_ArchitectPlan — architect summary mentions
// solution plan char count.
func TestSummariseAgentOutput_ArchitectPlan(t *testing.T) {
	out := domain.AgentOutput{
		Structured: map[string]any{
			"solution_plan": "step1\nstep2",
		},
		TokensOut: 25,
		Model:     "gpt-4",
	}
	got := summariseAgentOutput(domain.AgentArchitect, out)
	if got != "Solution plan: 11 chars (25 tokens, gpt-4)" {
		t.Fatalf("architect summary: %q", got)
	}
}

// TestSummariseAgentOutput_DevOpsPipeline — devops summary mentions YAML
// chars.
func TestSummariseAgentOutput_DevOpsPipeline(t *testing.T) {
	out := domain.AgentOutput{
		Structured: map[string]any{
			"pipeline_yaml": "name: build\non: push",
		},
		TokensOut: 30,
		Model:     "gpt-4",
	}
	got := summariseAgentOutput(domain.AgentDevOps, out)
	if got != "Pipeline YAML: 20 chars (30 tokens, gpt-4)" {
		t.Fatalf("devops summary: %q", got)
	}
}

// TestSummariseAgentOutput_DataEngineerMigration — data engineer summary.
func TestSummariseAgentOutput_DataEngineerMigration(t *testing.T) {
	out := domain.AgentOutput{
		Structured: map[string]any{
			"migration_sql": "CREATE TABLE x (id int)",
		},
		TokensOut: 40,
		Model:     "gpt-4",
	}
	got := summariseAgentOutput(domain.AgentDataEngineer, out)
	if got != "Migration SQL: 23 chars (40 tokens, gpt-4)" {
		t.Fatalf("data engineer summary: %q", got)
	}
}

// TestSummariseAgentOutput_FallbackOnEmptyStructured — when no recognised
// structured key fires, fall back to the generic token summary.
func TestSummariseAgentOutput_FallbackOnEmptyStructured(t *testing.T) {
	out := domain.AgentOutput{
		Structured: map[string]any{}, // no recognised keys
		TokensOut:  75,
		Model:      "gpt-4",
	}
	got := summariseAgentOutput(domain.AgentBackend, out)
	if got != "Completed: 75 tokens out (gpt-4)" {
		t.Fatalf("fallback: %q", got)
	}
}

// TestSummariseAgentOutput_NilStructured — same fallback when Structured
// is nil.
func TestSummariseAgentOutput_NilStructured(t *testing.T) {
	out := domain.AgentOutput{TokensOut: 10, Model: "x"}
	got := summariseAgentOutput(domain.AgentBackend, out)
	if got != "Completed: 10 tokens out (x)" {
		t.Fatalf("nil structured fallback: %q", got)
	}
}
