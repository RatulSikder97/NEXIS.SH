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
	cfg       OllamaConfig
	http      *http.Client
	embedDims int
}

func NewOllamaProvider(cfg OllamaConfig) *OllamaProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://host.docker.internal:11434"
	}
	return &OllamaProvider{cfg: cfg, http: &http.Client{Timeout: 5 * time.Minute}}
}

func (p *OllamaProvider) Name() string { return "ollama" }

// SetEmbeddingDims is called by the factory after probing /api/show on the
// configured embed model. Defaults to nomic-embed-text's 768 when unset.
func (p *OllamaProvider) SetEmbeddingDims(d int) { p.embedDims = d }

func (p *OllamaProvider) EmbeddingDims() int {
	if p.embedDims > 0 {
		return p.embedDims
	}
	return 768 // nomic-embed-text default
}

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
	start := time.Now()
	model := req.Model
	if model == "" {
		model = p.cfg.Default
	}
	// CacheSystem is intentionally ignored — Ollama has no equivalent.
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
		Model   string `json:"model"`
		Message struct {
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
		DurationMs:   time.Since(start).Milliseconds(),
		CostCents:    0,
	}, nil
}

type ollamaEmbedReq struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type ollamaEmbedResp struct {
	Embedding  []float32   `json:"embedding"`
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed batches one-at-a-time through /api/embeddings; Ollama's endpoint
// accepts only a single input per call. Returns one vector per input text.
func (p *OllamaProvider) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	if model == "" {
		model = "nomic-embed-text"
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		body, _ := json.Marshal(ollamaEmbedReq{Model: model, Input: t})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/api/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("ollama embed build: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := p.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%w: ollama embed: %v", domain.ErrEmbeddingFailed, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("%w: ollama embed status %d: %s", domain.ErrEmbeddingFailed, resp.StatusCode, string(raw))
		}
		var parsed ollamaEmbedResp
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("%w: ollama embed decode: %v", domain.ErrEmbeddingFailed, err)
		}
		vec := parsed.Embedding
		if len(vec) == 0 && len(parsed.Embeddings) > 0 {
			vec = parsed.Embeddings[0]
		}
		out[i] = vec
	}
	return out, nil
}

// ProbeModel hits POST /api/show to confirm the model is pulled locally.
// Returns true on 200, false on 404 / connection errors. The factory uses
// this to decide whether to fall back to OpenAI embeddings.
func (p *OllamaProvider) ProbeModel(ctx context.Context, model string) bool {
	body, _ := json.Marshal(map[string]string{"name": model})
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, p.cfg.BaseURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}
