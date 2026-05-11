package llm

import (
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

func TestNewFromConfig(t *testing.T) {
	cases := []struct {
		name     string
		cfg      config.Config
		wantName string
		wantErr  bool
	}{
		{"openai", config.Config{LLMProvider: "openai", OpenAIAPIKey: "k", OpenAIModelCheap: "gpt-4o-mini"}, "openai", false},
		{"ollama", config.Config{LLMProvider: "ollama", OllamaBaseURL: "http://x", OllamaModelGen: "llama3.1:8b"}, "ollama", false},
		{"unknown", config.Config{LLMProvider: "wat"}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := NewFromConfig(c.cfg)
			if c.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if p.Name() != c.wantName {
				t.Fatalf("want %q, got %q", c.wantName, p.Name())
			}
		})
	}
}
