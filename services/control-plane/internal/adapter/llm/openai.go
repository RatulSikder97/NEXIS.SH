package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Re-export domain types so callers in this package stay package-local.
type (
	CompletionRequest  = domain.CompletionRequest
	CompletionResponse = domain.CompletionResponse
	Info               = domain.LLMInfo
)

type OpenAIConfig struct {
	BaseURL string
	APIKey  string
	Default string
}

type OpenAIProvider struct {
	cfg  OpenAIConfig
	http *http.Client
}

func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com"
	}
	return &OpenAIProvider{cfg: cfg, http: &http.Client{Timeout: 60 * time.Second}}
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Info(_ context.Context) (Info, error) {
	return Info{Provider: "openai", BaseURL: p.cfg.BaseURL, Models: []string{p.cfg.Default}}, nil
}

func (p *OpenAIProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.cfg.Default
	}
	messages := []map[string]string{}
	if req.System != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.System})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.Prompt})

	body := map[string]any{"model": model, "messages": messages}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.JSONResponse {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	buf, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("%w: openai http: %v", domain.ErrLLMRequest, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return CompletionResponse{}, fmt.Errorf("%w: openai status %d: %s", domain.ErrLLMRequest, resp.StatusCode, string(raw))
	}
	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return CompletionResponse{}, fmt.Errorf("%w: openai decode: %v", domain.ErrLLMRequest, err)
	}
	if len(parsed.Choices) == 0 {
		return CompletionResponse{}, fmt.Errorf("%w: openai returned no choices", domain.ErrLLMRequest)
	}
	return CompletionResponse{
		Content:      parsed.Choices[0].Message.Content,
		Model:        parsed.Model,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
	}, nil
}
