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
type CompletionRequest struct {
	Model        string  `json:"model"`
	System       string  `json:"system,omitempty"`
	Prompt       string  `json:"prompt"`
	MaxTokens    int     `json:"max_tokens,omitempty"`
	Temperature  float32 `json:"temperature,omitempty"`
	JSONResponse bool    `json:"json_response,omitempty"`
}

// CompletionResponse is the provider-agnostic shape for a completion result.
type CompletionResponse struct {
	Content      string `json:"content"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Model        string `json:"model"`
}
