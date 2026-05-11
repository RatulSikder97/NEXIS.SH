package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIProvider_Complete(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing/invalid auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "chatcmpl-x",
			"object": "chat.completion",
			"model":  "gpt-4o-mini",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": "pong"},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 1, "total_tokens": 6},
		})
	}))
	defer mock.Close()

	p := NewOpenAIProvider(OpenAIConfig{BaseURL: mock.URL, APIKey: "test-key", Default: "gpt-4o-mini"})
	resp, err := p.Complete(context.Background(), CompletionRequest{Prompt: "ping"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "pong" {
		t.Fatalf(`want "pong", got %q`, resp.Content)
	}
	if resp.OutputTokens != 1 {
		t.Fatalf("want 1 output token, got %d", resp.OutputTokens)
	}
}

func TestOpenAIProvider_Info(t *testing.T) {
	p := NewOpenAIProvider(OpenAIConfig{BaseURL: "https://api.openai.com", Default: "gpt-4o"})
	info, err := p.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Provider != "openai" {
		t.Fatalf(`want provider="openai", got %q`, info.Provider)
	}
	if len(info.Models) == 0 {
		t.Fatal("want at least one model in Info")
	}
}
