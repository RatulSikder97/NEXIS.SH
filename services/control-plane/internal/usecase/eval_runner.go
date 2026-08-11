// Package usecase — eval_runner.go drives the side-by-side OpenAI vs
// Ollama comparison harness. For each provider in EvalRunner.Providers it
// constructs a per-provider agents.Registry, invokes the 5 L1 agents in
// DAG order (architect → backend → qa → devops → data_engineer), and
// persists one eval_transcripts row per (provider × agent) plus the
// rolling eval_runs totals.
package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/architect"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/backend"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/data_engineer"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/devops"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents/qa"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/llm"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/retrieval"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// scenarioFixtureFiles maps a CLI/HTTP scenario label to the JSON fixture
// under services/control-plane/fixtures/scenarios/ (legacy validator
// fixtures/incidents/ are still searched as a fallback). Keep in sync with
// transport/http/handler/pipelines.go:scenarioToFixture. Labels not in the
// map fall back to "<label>.json" so fixtures dropped into the scenarios
// directory are runnable without touching this table.
var scenarioFixtureFiles = map[string]string{
	"schema-drift":      "demo-schema-drift.json",
	"null-deref":        "demo-null-pointer.json",
	"oom":               "demo-oom.json",
	"synthetic":         "demo-null-pointer.json",
	"demo-null-pointer": "demo-null-pointer.json",
	// Fault-injection classes for the evaluation benchmark (fault classes
	// beyond the three demo scenarios; see docs/PROJECT_PLAN.md §7).
	"zero-div":               "demo-zero-div.json",
	"api-contract-violation": "demo-api-contract-violation.json",
	"dependency-breakage":    "demo-dependency-breakage.json",
	"conn-pool-exhaustion":   "demo-conn-pool-exhaustion.json",
	"deadlock":               "demo-deadlock.json",
	"memory-leak":            "demo-memory-leak.json",
	"rate-limit-cascade":     "demo-rate-limit-cascade.json",
	"disk-exhaustion":        "demo-disk-exhaustion.json",
}

// EvalRunner is the per-process harness. Wired in cmd/server/main.go for
// the HTTP entrypoint and constructed inline by cmd/eval/main.go for the
// CLI. The two share this struct so the CLI's `--providers` flag stays
// honoured end-to-end.
type EvalRunner struct {
	Cfg          config.Config
	AppPool      *pgxpool.Pool
	AdminPool    *pgxpool.Pool
	EvalRepo     *repo.EvalRepo
	LedgerRepo   *repo.TokenLedgerRepo
	WorkflowRepo *repo.WorkflowRepo
	AuditWriter  domain.AuditWriter
	Logger       *slog.Logger
	Providers    []string // {"openai","ollama"} by default
}

// RunSync executes the harness inline and returns the eval_run_id once
// every provider leg has terminated. Callers that want to fire-and-forget
// (the HTTP POST handler) wrap this in a goroutine; the CLI calls it
// directly because the user is staring at stdout.
func (e *EvalRunner) RunSync(ctx context.Context, p domain.Principal, scenario string) (string, error) {
	if e.Logger == nil {
		e.Logger = slog.Default()
	}
	if scenario == "" {
		return "", errors.New("eval_runner: scenario required")
	}
	providers := e.Providers
	if len(providers) == 0 {
		providers = []string{"openai", "ollama"}
	}

	// Load fixture incident — same lookup logic as the pipeline demo
	// handler. Missing files are tolerated; the agents fall back to a
	// placeholder incident.
	incident := loadFixtureIncidentForRunner(scenario)

	run := &domain.EvalRun{
		OrgID:         p.OrgID,
		IncidentLabel: scenario,
		Status:        "running",
		Providers:     providers,
	}
	for _, prov := range providers {
		switch prov {
		case "openai":
			run.OpenAIStatus = domain.EvalRunStatusQueued
		case "ollama":
			run.OllamaStatus = domain.EvalRunStatusQueued
		}
	}
	if err := e.EvalRepo.CreateRun(ctx, run); err != nil {
		return "", fmt.Errorf("create eval_run: %w", err)
	}

	// Audit "eval.run_started" so the operator-facing audit log shows the
	// CLI / UI trigger before any LLM tokens are spent.
	if e.AuditWriter != nil {
		_ = e.AuditWriter.Write(ctx, p, "eval.run_started", run.ID, map[string]any{
			"scenario":  scenario,
			"providers": providers,
		})
	}

	// Drive each provider sequentially. Ollama is the slow leg on a cold
	// box, so a sequential schedule keeps log frames in order — but
	// nothing in the data model prevents a future move to errgroup.
	var runErr string
	for _, prov := range providers {
		if err := e.runProvider(ctx, p, run.ID, prov, scenario, incident); err != nil {
			runErr = err.Error()
			e.Logger.Warn("eval: provider leg failed", "provider", prov, "err", err, "run_id", run.ID)
			// Don't break — record the other provider's transcripts so the
			// UI matrix can show one side green / one side red.
		}
	}

	if err := e.EvalRepo.CompleteRun(ctx, run.ID, runErr); err != nil {
		return run.ID, fmt.Errorf("complete eval_run: %w", err)
	}
	if e.AuditWriter != nil {
		_ = e.AuditWriter.Write(ctx, p, "eval.run_completed", run.ID, map[string]any{
			"scenario":  scenario,
			"providers": providers,
			"error":     runErr,
		})
	}
	return run.ID, nil
}

// runProvider executes the 5-agent DAG for one provider. Persists one
// eval_transcripts row per agent and folds totals into the eval_runs row.
func (e *EvalRunner) runProvider(
	ctx context.Context,
	p domain.Principal,
	runID, provider, scenario string,
	incident *domain.IncidentPayload,
) error {
	_ = e.EvalRepo.UpdateProviderStatus(ctx, runID, provider, domain.EvalRunStatusRunning)

	// Per-provider llm.Providers — NewProvidersFor flips cfg.LLMProvider
	// and rebuilds both LLM + Embedding adapters with provider-scoped
	// http.Client instances.
	provs, err := llm.NewProvidersFor(e.Cfg, provider, e.Logger)
	if err != nil {
		_ = e.EvalRepo.UpdateProviderStatus(ctx, runID, provider, domain.EvalRunStatusError)
		return fmt.Errorf("build providers: %w", err)
	}

	llmClient := &agents.LLMClient{
		Provider:       provs.LLM,
		Embedding:      provs.Embedding,
		Ledger:         e.LedgerRepo,
		Audit:          e.AuditWriter,
		Logger:         e.Logger,
		SchemaRetryMax: e.Cfg.AgentSchemaRetryMax,
	}
	var retClient *agents.RetrievalClient
	if e.AdminPool != nil {
		retClient = &agents.RetrievalClient{
			Store:      retrieval.New(e.AdminPool),
			Embed:      provs.Embedding,
			EmbedModel: e.Cfg.OpenAIEmbedModel,
			K:          5,
		}
	} else {
		retClient = &agents.RetrievalClient{} // tolerant — ContextFor returns ""
	}

	models := perProviderModels(e.Cfg, provider)
	registry := agents.NewRegistry(map[domain.AgentName]domain.Agent{
		domain.AgentNameArchitect:    architect.New(llmClient, retClient, models.architect),
		domain.AgentNameBackend:      backend.New(llmClient, retClient, models.backend),
		domain.AgentNameQA:           qa.New(llmClient, retClient, models.qa),
		domain.AgentNameDevOps:       devops.New(llmClient, retClient, models.devops),
		domain.AgentNameDataEngineer: data_engineer.New(llmClient, retClient, models.dataEng),
	})

	prior := map[string]any{}
	// Each eval gets a synthetic workflow_run_id so token_ledger rows
	// can still be joined back to the harness invocation. The id is not
	// FK-bound to workflow_runs because that table is reserved for real
	// Temporal-backed pipelines; we tolerate the FK on token_ledger via
	// the workflow_run_id nullable column.
	syntheticRunID := uuid.NewString()
	anyFailed := false

	for _, name := range domain.AllL1Agents {
		start := time.Now().UTC()
		ag, _ := registry.Get(name)
		in := domain.AgentInput{
			WorkflowRunID: syntheticRunID,
			OrgID:         p.OrgID,
			PriorOutputs:  copyMap(prior),
			PromptContext: "Eval scenario: " + scenario,
			Incident:      incident,
			RepoSHA:       "fixture-" + e.Cfg.FixtureSHA,
		}
		out, runErr := ag.Run(ctx, in)
		finished := time.Now().UTC()

		schemaValid := runErr == nil || !errors.Is(runErr, domain.ErrAgentSchemaMismatch)
		t := &domain.EvalTranscript{
			EvalRunID:    runID,
			OrgID:        p.OrgID,
			Provider:     provider,
			Agent:        name,
			Model:        out.Model,
			InputJSON:    inputSnapshot(in, out.SystemPrompt, out.UserPrompt),
			OutputJSON:   outputSnapshot(out, runErr),
			Success:      out.Success && runErr == nil,
			SchemaValid:  schemaValid && out.Success,
			TokensIn:     out.TokensIn,
			TokensOut:    out.TokensOut,
			CachedTokens: out.CachedTokens,
			CostCents:    out.CostCents,
			DurationMs:   out.DurationMs,
			StartedAt:    start,
			FinishedAt:   finished,
		}
		if err := e.EvalRepo.AppendTranscript(ctx, t); err != nil {
			e.Logger.Warn("eval: append transcript", "err", err, "agent", name)
		}
		_ = e.EvalRepo.AccumulateProviderTotals(
			ctx, runID, provider,
			out.TokensIn, out.TokensOut, out.CostCents, out.DurationMs,
		)

		if runErr != nil {
			anyFailed = true
			// Stop chaining outputs — downstream agents would just produce
			// noise off a missing predecessor structured result.
			break
		}
		if out.Structured != nil {
			prior[string(name)] = out.Structured
		}
	}

	status := domain.EvalRunStatusSucceeded
	if anyFailed {
		status = domain.EvalRunStatusFailed
	}
	_ = e.EvalRepo.UpdateProviderStatus(ctx, runID, provider, status)
	if anyFailed {
		return fmt.Errorf("provider %s: one or more agents failed", provider)
	}
	return nil
}

type perAgentModels struct {
	architect, backend, qa, devops, dataEng string
}

func perProviderModels(cfg config.Config, provider string) perAgentModels {
	if provider == "ollama" {
		return perAgentModels{
			architect: cfg.AgentModelArchitectOllama,
			backend:   cfg.AgentModelBackendOllama,
			qa:        cfg.AgentModelQAOllama,
			devops:    cfg.AgentModelDevOpsOllama,
			dataEng:   cfg.AgentModelDataEngOllama,
		}
	}
	return perAgentModels{
		architect: cfg.AgentModelArchitectOpenAI,
		backend:   cfg.AgentModelBackendOpenAI,
		qa:        cfg.AgentModelQAOpenAI,
		devops:    cfg.AgentModelDevOpsOpenAI,
		dataEng:   cfg.AgentModelDataEngOpenAI,
	}
}

// inputSnapshot captures the resolved AgentInput so eval_transcripts can be
// replayed offline. We trim the prompt strings to a stable shape (system +
// user prompts as separate keys) so the UI doesn't have to know about the
// internal AgentInput layout.
func inputSnapshot(in domain.AgentInput, system, user string) map[string]any {
	out := map[string]any{
		"org_id":          in.OrgID,
		"workflow_run_id": in.WorkflowRunID,
		"prompt_context":  in.PromptContext,
		"repo_sha":        in.RepoSHA,
	}
	if in.Incident != nil {
		out["incident"] = in.Incident
	}
	if len(in.PriorOutputs) > 0 {
		out["prior_outputs"] = in.PriorOutputs
	}
	if system != "" {
		out["system_prompt"] = system
	}
	if user != "" {
		out["user_prompt"] = user
	}
	return out
}

// outputSnapshot mirrors the AgentOutput fields the UI cares about plus the
// error string when the agent failed. Structured is preferred over Content
// — when both are present, Content tends to be a duplicate of Structured
// rendered as JSON.
func outputSnapshot(out domain.AgentOutput, runErr error) map[string]any {
	snap := map[string]any{
		"success":        out.Success,
		"content":        out.Content,
		"model":          out.Model,
		"provider":       out.Provider,
		"tokens_in":      out.TokensIn,
		"tokens_out":     out.TokensOut,
		"cached_tokens":  out.CachedTokens,
		"cost_cents":     out.CostCents,
		"duration_ms":    out.DurationMs,
		"schema_retries": out.SchemaRetries,
	}
	if out.Structured != nil {
		snap["structured"] = out.Structured
	}
	if runErr != nil {
		snap["error"] = runErr.Error()
	}
	return snap
}

// copyMap shallow-clones the prior_outputs map so subsequent agents can't
// mutate a previous agent's structured payload by accident.
func copyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// loadFixtureIncidentForRunner reads the JSON fixture into IncidentPayload.
// Returns nil when the file is missing — agents fall back to an
// "Unspecified incident" placeholder. The fixture base dir is configurable
// via FIXTURE_INCIDENTS_DIR so the CLI / server can both find it whether
// running in compose, locally from web_app/, or from the bin/ directory.
func loadFixtureIncidentForRunner(scenario string) *domain.IncidentPayload {
	file, ok := scenarioFixtureFiles[scenario]
	if !ok {
		// Labels follow the "<basename>.json" convention — try that before
		// giving up so new fixtures don't require a map edit.
		file = scenario + ".json"
	}
	bases := []string{
		os.Getenv("FIXTURE_INCIDENTS_DIR"),
		// Control-plane-owned fault-injection fixtures (preferred).
		"services/control-plane/fixtures/scenarios",
		"fixtures/scenarios",
		"/app/fixtures/scenarios",
		"../../services/control-plane/fixtures/scenarios",
		"../../../services/control-plane/fixtures/scenarios",
		// Legacy validator fixtures.
		"services/validator/fixtures/incidents",
		"/app/fixtures/incidents",
		"../../services/validator/fixtures/incidents",
		"../../../services/validator/fixtures/incidents",
	}
	for _, base := range bases {
		if base == "" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(base, file))
		if err != nil {
			continue
		}
		var payload domain.IncidentPayload
		if err := json.Unmarshal(body, &payload); err == nil && payload.Title != "" {
			return &payload
		}
		// Some fixtures are nested {incident: {...}} or use different keys.
		// Fall back to a generic decode that pulls the common fields.
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err == nil {
			return rawToIncident(raw, scenario)
		}
	}
	return nil
}

func rawToIncident(raw map[string]any, label string) *domain.IncidentPayload {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := raw[k]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
		return ""
	}
	return &domain.IncidentPayload{
		Label:       label,
		Title:       get("title", "summary", "label"),
		Service:     get("service", "service_name"),
		Environment: get("environment", "env"),
		Stacktrace:  get("stacktrace", "stack_trace", "trace"),
		Logs:        get("logs", "log"),
	}
}
