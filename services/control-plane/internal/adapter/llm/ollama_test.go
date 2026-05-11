package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaProvider_Info(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("want /api/tags, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{{"name": "llama3.1:8b", "size": 4_700_000_000}},
		})
	}))
	defer mock.Close()

	p := NewOllamaProvider(OllamaConfig{BaseURL: mock.URL, Default: "llama3.1:8b"})
	info, err := p.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Provider != "ollama" {
		t.Fatalf(`want provider="ollama", got %q`, info.Provider)
	}
	if len(info.Models) != 1 || info.Models[0] != "llama3.1:8b" {
		t.Fatalf("want [llama3.1:8b], got %v", info.Models)
	}
}

func TestOllamaProvider_Complete(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("want /api/chat, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":             "llama3.1:8b",
			"message":           map[string]string{"role": "assistant", "content": "pong"},
			"done":              true,
			"prompt_eval_count": 5,
			"eval_count":        1,
		})
	}))
	defer mock.Close()

	p := NewOllamaProvider(OllamaConfig{BaseURL: mock.URL, Default: "llama3.1:8b"})
	resp, err := p.Complete(context.Background(), CompletionRequest{Prompt: "ping"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "pong" {
		t.Fatalf(`want "pong", got %q`, resp.Content)
	}
	if resp.InputTokens != 5 || resp.OutputTokens != 1 {
		t.Fatalf("want 5/1 tokens, got %d/%d", resp.InputTokens, resp.OutputTokens)
	}
}
