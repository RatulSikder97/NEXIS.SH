package domain

import "context"

// LLMProvider is the only contract every agent module talks to.
// Switching providers (openai → ollama → anthropic) is one wire-up swap.
type LLMProvider interface {
	Name() string
	Info(ctx context.Context) (LLMInfo, error)
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

// LLMInfo describes a provider's identity + the models it currently has available.
type LLMInfo struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
	BaseURL  string   `json:"base_url,omitempty"`
}

// CompletionRequest is the provider-agnostic shape for a chat completion call.
// Phase 5 adds CacheSystem (opt into OpenAI prompt caching on the system
// block) and Modality (reserved; defaults to 'chat'). Existing callers are
// unaffected — defaults preserve prior behavior.
type CompletionRequest struct {
	Model        string  `json:"model"`
	System       string  `json:"system,omitempty"`
	Prompt       string  `json:"prompt"`
	MaxTokens    int     `json:"max_tokens,omitempty"`
	Temperature  float32 `json:"temperature,omitempty"`
	JSONResponse bool    `json:"json_response,omitempty"`
	CacheSystem  bool    `json:"cache_system,omitempty"` // NEW — prompt caching (OpenAI only; Ollama ignores)
	Modality     string  `json:"modality,omitempty"`     // NEW — 'chat' (default) | 'embed'
}

// CompletionResponse is the provider-agnostic shape for a completion result.
// Phase 5 adds CachedTokens (count from OpenAI usage.prompt_tokens_details),
// DurationMs (wall-clock time inside the adapter) and CostCents (computed by
// the adapter using its per-model price table).
type CompletionResponse struct {
	Content      string  `json:"content"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CachedTokens int     `json:"cached_tokens,omitempty"` // NEW
	Model        string  `json:"model"`
	DurationMs   int64   `json:"duration_ms,omitempty"` // NEW
	CostCents    float64 `json:"cost_cents,omitempty"`  // NEW — populated by the adapter
}
