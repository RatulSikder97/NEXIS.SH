package domain

import "context"

// AgentName names the 5 L1 agents (Phase 5) and the 4 L2 agents (Phase 6).
// Phase 5 ships only the L1 set; Phase 6 fills in the rest. The wire value is
// snake_case to match AgentRole — the two enums share string values so
// activity_events.agent_role + token_ledger.agent join naturally on text.
type AgentName string

const (
	AgentNameArchitect    AgentName = "architect"
	AgentNameBackend      AgentName = "backend"
	AgentNameQA           AgentName = "qa"
	AgentNameDevOps       AgentName = "devops"
	AgentNameDataEngineer AgentName = "data_engineer"

	// Phase 6 — L2 agents. Sentinel detection lives outside the agent fleet
	// (it's a goroutine in internal/sentinel/), but the workflow still needs
	// the AgentName constants for Registry dispatch on the synthesiser /
	// pathfinder / validator_l2 sides.
	AgentNamePathfinder  AgentName = "pathfinder"
	AgentNameSynthesiser AgentName = "synthesiser"
	AgentNameValidatorL2 AgentName = "validator_l2"
)

// AllL1Agents lets the eval harness + factory iterate without hard-coding.
// Order is the canonical DAG order: architect → backend → qa → devops →
// data_engineer.
var AllL1Agents = []AgentName{
	AgentNameArchitect, AgentNameBackend, AgentNameQA,
	AgentNameDevOps, AgentNameDataEngineer,
}

// AllL2Agents lists the Phase 6 L2 agents that go through the Registry.
// Note: AgentNameSentinel is NOT here because Sentinel.Detect is just an
// ack inside the workflow — the real detection happens in the goroutine.
var AllL2Agents = []AgentName{
	AgentNamePathfinder, AgentNameSynthesiser, AgentNameValidatorL2,
}

// IncidentPayload is the minimal shape consumed by L1 agents in Phase 5. The
// demo usecase fills this from services/validator/fixtures/incidents/<label>.json.
type IncidentPayload struct {
	Label       string `json:"label,omitempty"`
	Title       string `json:"title"`
	Service     string `json:"service"`
	Environment string `json:"environment"`
	Stacktrace  string `json:"stacktrace,omitempty"`
	Logs        string `json:"logs,omitempty"`
}

// AgentInput is what every agent reads. PriorOutputs is the {agent_name → Structured}
// map from earlier steps in the DAG (Architect.Structured keyed at "architect", etc.).
// PromptContext is the workflow-run-scoped narrative the L2 Synthesiser produced;
// Phase 5's demo path seeds it with a fixture incident description.
//
// Context (Phase 7 — projects/self-healing) is the optional per-agent
// scratch map. Today it carries the project mappings (github_repo,
// github_default_branch, github_installation_id) so the L1 prompts can tell
// the LLM which repo to target. Nil-safe — agents must fall back to defaults
// when a key is missing.
type AgentInput struct {
	WorkflowRunID string           `json:"workflow_run_id"`
	OrgID         string           `json:"org_id"`
	WorkspaceID   string           `json:"workspace_id"`
	PriorOutputs  map[string]any   `json:"prior_outputs,omitempty"`
	PromptContext string           `json:"prompt_context,omitempty"`
	Incident      *IncidentPayload `json:"incident,omitempty"`
	RepoSHA       string           `json:"repo_sha,omitempty"` // retrieval scope
	Context       map[string]any   `json:"context,omitempty"`
}

// AgentOutput is what every agent returns. Structured is the schema-validated
// JSON payload Phase 6 agents consume; Content is the raw model text for
// transcripts + diffing across providers. SystemPrompt and UserPrompt are
// included so eval_transcripts can persist the exact strings used.
type AgentOutput struct {
	Success       bool           `json:"success"`
	Content       string         `json:"content"`
	Structured    map[string]any `json:"structured"`
	TokensIn      int            `json:"tokens_in"`
	TokensOut     int            `json:"tokens_out"`
	CachedTokens  int            `json:"cached_tokens,omitempty"`
	CostCents     float64        `json:"cost_cents"`
	DurationMs    int64          `json:"duration_ms"`
	Model         string         `json:"model"`
	Provider      string         `json:"provider"`
	SchemaRetries int            `json:"schema_retries,omitempty"`
	SystemPrompt  string         `json:"system_prompt,omitempty"`
	UserPrompt    string         `json:"user_prompt,omitempty"`
}

// Agent is the single port every L1/L2 agent implementation satisfies. The
// Registry in internal/adapter/agents/registry.go dispatches by Name().
type Agent interface {
	Name() AgentName
	Run(ctx context.Context, in AgentInput) (AgentOutput, error)
}
