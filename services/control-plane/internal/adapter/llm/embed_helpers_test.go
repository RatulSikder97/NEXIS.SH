package llm

// Coverage for the LLM provider helpers that don't require a live model
// host. Embed / Chat round-trips need an HTTP server fake; those are
// exercised under the integration build tag.

import (
	"testing"
)

// TestOllama_SetAndGetEmbeddingDims — round-trip the dimension override.
// Default falls back to 768 (nomic-embed-text).
func TestOllama_SetAndGetEmbeddingDims(t *testing.T) {
	p := NewOllamaProvider(OllamaConfig{})
	if got := p.EmbeddingDims(); got != 768 {
		t.Fatalf("default dims: got %d want 768", got)
	}
	p.SetEmbeddingDims(1536)
	if got := p.EmbeddingDims(); got != 1536 {
		t.Fatalf("override dims: got %d want 1536", got)
	}
}

// TestOllama_Name returns the canonical "ollama" string.
func TestOllama_Name(t *testing.T) {
	p := NewOllamaProvider(OllamaConfig{})
	if got := p.Name(); got != "ollama" {
		t.Fatalf("name: %q", got)
	}
}

// TestOpenAI_DefaultEmbeddingDims — when no override is set, OpenAI's
// EmbeddingDims falls back to the documented default for
// text-embedding-3-small (1536) or 0 if not configured.
func TestOpenAI_DefaultEmbeddingDims(t *testing.T) {
	p := NewOpenAIProvider(OpenAIConfig{})
	got := p.EmbeddingDims()
	if got <= 0 {
		t.Fatalf("dims must be positive: %d", got)
	}
}

// TestOpenAI_Name returns the canonical "openai" string.
func TestOpenAI_Name(t *testing.T) {
	p := NewOpenAIProvider(OpenAIConfig{})
	if got := p.Name(); got != "openai" {
		t.Fatalf("name: %q", got)
	}
}

