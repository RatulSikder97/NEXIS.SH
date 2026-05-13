package dto

// EvalRunSummaryResp is the list-view wire shape for an eval_runs row.
// Field names mirror the Go runner struct verbatim so the snake_case JSON
// hits apps/web/lib/eval.ts:EvalRunSummary without translation.
//
// Costs are emitted as *_cents_exact: fractional cents with 4-decimal
// precision (cents × 10000). The frontend's formatCentsExact() divides by
// 10000 / 100 to render USD so sub-cent agent calls (~$0.002 per
// Architect invocation) don't read as "$0.00".
type EvalRunSummaryResp struct {
	ID                   string  `json:"id"`
	Scenario             string  `json:"scenario"`
	OpenAIStatus         string  `json:"openai_status"`
	OllamaStatus         string  `json:"ollama_status"`
	OpenAICostCentsExact float64 `json:"openai_cost_cents_exact"`
	OllamaCostCentsExact float64 `json:"ollama_cost_cents_exact"`
	OpenAITokensIn       int     `json:"openai_tokens_in"`
	OpenAITokensOut      int     `json:"openai_tokens_out"`
	OllamaTokensIn       int     `json:"ollama_tokens_in"`
	OllamaTokensOut      int     `json:"ollama_tokens_out"`
	DurationOpenAIMs     int64   `json:"duration_openai_ms"`
	DurationOllamaMs     int64   `json:"duration_ollama_ms"`
	StartedAt            string  `json:"started_at"`
	FinishedAt           string  `json:"finished_at,omitempty"`
	Error                string  `json:"error,omitempty"`
}

// EvalTranscriptResp is one (provider × agent) cell. Agent is the
// PascalCase display name ("Architect", "Backend", "QA", "DevOps",
// "DataEngineer") matching the frontend's EvalAgent union — the
// domain.AgentName -> display string conversion lives in
// domain.AgentDisplayName.
type EvalTranscriptResp struct {
	ID             string         `json:"id"`
	EvalRunID      string         `json:"eval_run_id"`
	Provider       string         `json:"provider"`
	Agent          string         `json:"agent"`
	Model          string         `json:"model,omitempty"`
	InputJSON      map[string]any `json:"input_json"`
	OutputJSON     map[string]any `json:"output_json"`
	Success        bool           `json:"success"`
	TokensIn       int            `json:"tokens_in"`
	TokensOut      int            `json:"tokens_out"`
	CachedTokens   int            `json:"cached_tokens"`
	CostCentsExact float64        `json:"cost_cents_exact"`
	DurationMs     int64          `json:"duration_ms"`
	StartedAt      string         `json:"started_at"`
	FinishedAt     string         `json:"finished_at"`
}

// EvalDetailResp is the GET /v1/workspaces/{ws}/eval/{run_id} body.
type EvalDetailResp struct {
	Run         EvalRunSummaryResp   `json:"run"`
	Transcripts []EvalTranscriptResp `json:"transcripts"`
}

// CreateEvalReq is the POST /v1/workspaces/{ws}/eval body. The handler
// whitelists scenario against {"schema-drift", "null-deref", "oom"} so
// arbitrary strings never reach the runner.
type CreateEvalReq struct {
	Scenario string `json:"scenario"`
}

// CreateEvalResp is the immediate 202 response. The runner starts in a
// goroutine; the client polls /v1/workspaces/{ws}/eval to see progress.
type CreateEvalResp struct {
	RunID string `json:"run_id"`
}

// BudgetStatusResp drives the token-budget topbar pill. Used/allowed
// values are integer token counts (no _exact suffix because tokens are
// already whole units).
type BudgetStatusResp struct {
	UsedTokensIn     int64  `json:"used_tokens_in"`
	UsedTokensOut    int64  `json:"used_tokens_out"`
	AllowedTokensIn  int64  `json:"allowed_tokens_in"`
	AllowedTokensOut int64  `json:"allowed_tokens_out"`
	PeriodStart      string `json:"period_start"`
	PeriodEnd        string `json:"period_end"`
}
