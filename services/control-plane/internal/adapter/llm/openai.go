package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// NewOpenAIProvider builds the client. BaseURL must include the API version
// segment (".../v1", ".../v3/openai", …) because OpenAI-compatible gateways
// disagree on where the version sits: OpenAI serves /v1/chat/completions
// while Novita serves /v3/openai/chat/completions. Keeping the version in
// the configured base — rather than hardcoding "/v1" into every request
// path — is what lets one client talk to both.
func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	return &OpenAIProvider{cfg: cfg, http: &http.Client{Timeout: 180 * time.Second}}
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Info(_ context.Context) (Info, error) {
	return Info{Provider: "openai", BaseURL: p.cfg.BaseURL, Models: []string{p.cfg.Default}}, nil
}

// openaiCacheControl is the JSON tag-friendly cache hint we attach to the
// system block when req.CacheSystem == true.
type openaiCacheControl struct {
	Type string `json:"type"`
}

// openaiContentBlock is the array-of-blocks shape OpenAI uses when a message
// carries cache_control. Plain string content is still supported when no
// caching is requested.
type openaiContentBlock struct {
	Type         string              `json:"type"`
	Text         string              `json:"text"`
	CacheControl *openaiCacheControl `json:"cache_control,omitempty"`
}

func (p *OpenAIProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	start := time.Now()
	model := req.Model
	if model == "" {
		model = p.cfg.Default
	}

	// Build messages — when CacheSystem is requested, the system message uses
	// the array-of-blocks shape with cache_control: ephemeral. Otherwise we
	// keep the simple string content for backward compatibility with the
	// Phase 1 callers + the openai_test.go fakes.
	messages := []map[string]any{}
	if req.System != "" {
		if req.CacheSystem {
			block := openaiContentBlock{
				Type:         "text",
				Text:         req.System,
				CacheControl: &openaiCacheControl{Type: "ephemeral"},
			}
			messages = append(messages, map[string]any{
				"role":    "system",
				"content": []any{block},
			})
		} else {
			messages = append(messages, map[string]any{
				"role":    "system",
				"content": req.System,
			})
		}
	}
	messages = append(messages, map[string]any{
		"role":    "user",
		"content": req.Prompt,
	})

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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/chat/completions", bytes.NewReader(buf))
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
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return CompletionResponse{}, fmt.Errorf("%w: openai decode: %v", domain.ErrLLMRequest, err)
	}
	if len(parsed.Choices) == 0 {
		return CompletionResponse{}, fmt.Errorf("%w: openai returned no choices", domain.ErrLLMRequest)
	}

	cached := parsed.Usage.PromptTokensDetails.CachedTokens
	nonCachedIn := parsed.Usage.PromptTokens - cached
	if nonCachedIn < 0 {
		nonCachedIn = parsed.Usage.PromptTokens
		cached = 0
	}
	cost := costCentsForOpenAI(parsed.Model, nonCachedIn, parsed.Usage.CompletionTokens, cached)
	return CompletionResponse{
		Content:      parsed.Choices[0].Message.Content,
		Model:        parsed.Model,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
		CachedTokens: cached,
		CostCents:    cost,
		DurationMs:   time.Since(start).Milliseconds(),
	}, nil
}

// embeddingDims is the canonical dimension count used for code_embeddings.
// We always use 1536 (text-embedding-3-small); if the configured embed model
// returns a different dim, the factory pads or falls back.
const embeddingDims = 1536

// EmbeddingDims returns the dim count emitted by Embed.
func (p *OpenAIProvider) EmbeddingDims() int { return embeddingDims }

type openaiEmbedReq struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openaiEmbedResp struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
	} `json:"usage"`
}

// Embed calls POST /v1/embeddings and returns one float32 vector per input.
// Empty texts produce an error from the API — caller filters them out.
func (p *OpenAIProvider) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	if model == "" {
		model = "text-embedding-3-small"
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	body, _ := json.Marshal(openaiEmbedReq{Model: model, Input: texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai embed build: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: openai embed http: %v", domain.ErrEmbeddingFailed, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: openai embed status %d: %s", domain.ErrEmbeddingFailed, resp.StatusCode, string(raw))
	}
	var out openaiEmbedResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: openai embed decode: %v", domain.ErrEmbeddingFailed, err)
	}
	vecs := make([][]float32, len(out.Data))
	for i := range out.Data {
		vecs[i] = out.Data[i].Embedding
	}
	return vecs, nil
}

// USD-cents-per-1M-tokens rate table for the OpenAI models we use. Phase 5
// only wires four models — adding more is one line. Source: openai.com/api/
// pricing as of 2026-05.
var openaiPricing = map[string]struct {
	InputCentsPer1M       float64
	OutputCentsPer1M      float64
	CachedInputCentsPer1M float64
}{
	"gpt-4o":      {250.0, 1000.0, 125.0},
	"gpt-4o-mini": {15.0, 60.0, 7.5},

	"text-embedding-3-small": {2.0, 0.0, 2.0},
}

func costCentsForOpenAI(model string, in, out, cached int) float64 {
	p, ok := openaiPricing[model]
	if !ok {
		return 0
	}
	return float64(in)*p.InputCentsPer1M/1e6 +
		float64(out)*p.OutputCentsPer1M/1e6 +
		float64(cached)*p.CachedInputCentsPer1M/1e6
}
