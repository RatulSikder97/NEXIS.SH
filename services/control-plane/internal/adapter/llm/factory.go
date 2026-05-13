package llm

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// Providers bundles the LLM + Embedding port so callers (agents, seed CLI,
// eval CLI) get both from one factory call.
type Providers struct {
	LLM       domain.LLMProvider
	Embedding domain.EmbeddingProvider
}

// NewFromConfig preserves the Phase 1 single-provider contract by returning
// only the LLMProvider. Phase 5 callers should prefer NewProviders.
func NewFromConfig(cfg config.Config) (domain.LLMProvider, error) {
	p, err := NewProviders(cfg, slog.Default())
	if err != nil {
		return nil, err
	}
	return p.LLM, nil
}

// NewProviders is the Phase 5 entrypoint. When LLM_PROVIDER=ollama and the
// configured ollama embed model is missing, falls back to OpenAI for
// embeddings only — the LLM path stays on ollama. Logs the fallback.
func NewProviders(cfg config.Config, logger *slog.Logger) (Providers, error) {
	switch cfg.LLMProvider {
	case "openai":
		op := NewOpenAIProvider(OpenAIConfig{
			APIKey:  cfg.OpenAIAPIKey,
			Default: cfg.OpenAIModelCheap,
		})
		return Providers{LLM: op, Embedding: op}, nil
	case "ollama":
		ol := NewOllamaProvider(OllamaConfig{
			BaseURL: cfg.OllamaBaseURL,
			Default: cfg.OllamaModelGen,
		})
		// Probe ollama for the embed model. If absent → fall back to OpenAI
		// embeddings (the agents layer doesn't care which provider owns
		// embeddings, only that EmbeddingDims() is consistent).
		probeCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if ol.ProbeModel(probeCtx, cfg.OllamaEmbedModel) {
			return Providers{LLM: ol, Embedding: ol}, nil
		}
		if logger != nil {
			logger.Warn("ollama embed model missing — falling back to OpenAI for embeddings",
				"missing_model", cfg.OllamaEmbedModel,
				"fallback_model", cfg.OpenAIEmbedModel)
		}
		op := NewOpenAIProvider(OpenAIConfig{APIKey: cfg.OpenAIAPIKey})
		return Providers{LLM: ol, Embedding: op}, nil
	default:
		return Providers{}, fmt.Errorf("unknown LLM_PROVIDER %q (want openai|ollama)", cfg.LLMProvider)
	}
}

// NewProvidersFor is the canonical eval-override entry. p must be one of
// "openai" or "ollama". The returned Providers carries an independent HTTP
// client so concurrent evals don't share connection pools across providers.
func NewProvidersFor(cfg config.Config, p string, logger *slog.Logger) (Providers, error) {
	cfg.LLMProvider = p
	return NewProviders(cfg, logger)
}
