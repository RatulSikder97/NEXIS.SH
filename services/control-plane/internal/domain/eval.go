package domain

import "time"

// EvalRunStatus tags one provider leg of an eval run. The runner records one
// status per provider so the UI matrix can show independent failure modes —
// e.g. openai succeeded while ollama failed schema validation.
type EvalRunStatus string

const (
	EvalRunStatusQueued    EvalRunStatus = "queued"
	EvalRunStatusRunning   EvalRunStatus = "running"
	EvalRunStatusSucceeded EvalRunStatus = "succeeded"
	EvalRunStatusFailed    EvalRunStatus = "failed"
	EvalRunStatusError     EvalRunStatus = "error"
)

// EvalRun is the aggregate row in eval_runs. Per-provider columns are
// optional — they're zero-valued when the run hasn't reached that leg yet.
// CompletedAt is a pointer so an in-flight run can be distinguished from a
// terminal one without sentinel times on the wire.
//
// IncidentLabel and Providers reflect the 0011 schema; the rest is the
// 0014 extension. Status (legacy single column) survives as a string for
// backwards-compatible writes from non-eval callers.
type EvalRun struct {
	ID             string
	OrgID          string
	IncidentLabel  string
	Status         string
	Providers      []string
	StartedAt      time.Time
	CompletedAt    *time.Time
	Error          string

	OpenAIStatus EvalRunStatus
	OllamaStatus EvalRunStatus

	OpenAITokensIn  int
	OpenAITokensOut int
	OllamaTokensIn  int
	OllamaTokensOut int

	OpenAICostCentsExact float64 // exact fractional cents (10000x precision in the DB scale)
	OllamaCostCentsExact float64

	DurationOpenAIMs int64
	DurationOllamaMs int64
}

// EvalTranscript is one (provider × agent) cell. Phase 5 persists both the
// rendered prompt (InputJSON) and the validated assistant output
// (OutputJSON); the legacy columns (SystemPrompt / UserPrompt /
// AssistantOutput) survive in the DB but the runner no longer writes to
// them. SchemaValid is retained because the eval CLI's exit-code
// classifier inspects it (exit 2 on any false).
type EvalTranscript struct {
	ID          string
	EvalRunID   string
	OrgID       string
	Provider    string // "openai" | "ollama"
	Agent       AgentName
	Model       string

	InputJSON  map[string]any
	OutputJSON map[string]any

	Success      bool
	SchemaValid  bool

	TokensIn      int
	TokensOut     int
	CachedTokens  int
	CostCents     float64 // exact fractional cents
	DurationMs    int64

	StartedAt  time.Time
	FinishedAt time.Time
}

// AgentDisplayName maps the snake_case domain.AgentName to the PascalCase
// label the frontend SDK + eval matrix expects on the wire ("Architect",
// "Backend", "QA", "DevOps", "DataEngineer"). Keep in sync with
// apps/web/lib/eval.ts:EvalAgent.
func AgentDisplayName(n AgentName) string {
	switch n {
	case AgentNameArchitect:
		return "Architect"
	case AgentNameBackend:
		return "Backend"
	case AgentNameQA:
		return "QA"
	case AgentNameDevOps:
		return "DevOps"
	case AgentNameDataEngineer:
		return "DataEngineer"
	default:
		return string(n)
	}
}
