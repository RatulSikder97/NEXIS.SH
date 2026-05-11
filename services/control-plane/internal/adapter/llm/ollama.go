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

type OllamaConfig struct {
	BaseURL string
	Default string
}

type OllamaProvider struct {
	cfg  OllamaConfig
	http *http.Client
}

func NewOllamaProvider(cfg OllamaConfig) *OllamaProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://host.docker.internal:11434"
	}
	return &OllamaProvider{cfg: cfg, http: &http.Client{Timeout: 5 * time.Minute}}
}

func (p *OllamaProvider) Name() string { return "ollama" }

func (p *OllamaProvider) Info(ctx context.Context) (Info, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.BaseURL+"/api/tags", nil)
	if err != nil {
		return Info{}, fmt.Errorf("ollama: build tags request: %w", err)
	}
	resp, err := p.http.Do(httpReq)
	if err != nil {
		return Info{}, fmt.Errorf("%w: ollama tags: %v", domain.ErrLLMRequest, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return Info{}, fmt.Errorf("%w: ollama tags status %d: %s", domain.ErrLLMRequest, resp.StatusCode, string(raw))
	}
	var parsed struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return Info{}, fmt.Errorf("%w: ollama tags decode: %v", domain.ErrLLMRequest, err)
	}
	models := make([]string, 0, len(parsed.Models))
	for _, m := range parsed.Models {
		models = append(models, m.Name)
	}
	return Info{Provider: "ollama", BaseURL: p.cfg.BaseURL, Models: models}, nil
}

func (p *OllamaProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.cfg.Default
	}
	messages := []map[string]string{}
	if req.System != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.System})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.Prompt})

	body := map[string]any{"model": model, "messages": messages, "stream": false}
	if req.JSONResponse {
		body["format"] = "json"
	}
	buf, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: build chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("%w: ollama chat: %v", domain.ErrLLMRequest, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return CompletionResponse{}, fmt.Errorf("%w: ollama chat status %d: %s", domain.ErrLLMRequest, resp.StatusCode, string(raw))
	}
	var parsed struct {
		Model           string `json:"model"`
		Message         struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count"`
		EvalCount       int `json:"eval_count"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return CompletionResponse{}, fmt.Errorf("%w: ollama decode: %v", domain.ErrLLMRequest, err)
	}
	return CompletionResponse{
		Content:      parsed.Message.Content,
		Model:        parsed.Model,
		InputTokens:  parsed.PromptEvalCount,
		OutputTokens: parsed.EvalCount,
	}, nil
}
