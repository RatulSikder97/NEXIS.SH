package llm

import (
	"fmt"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

func NewFromConfig(cfg config.Config) (domain.LLMProvider, error) {
	switch cfg.LLMProvider {
	case "openai":
		return NewOpenAIProvider(OpenAIConfig{APIKey: cfg.OpenAIAPIKey, Default: cfg.OpenAIModelCheap}), nil
	case "ollama":
		return NewOllamaProvider(OllamaConfig{BaseURL: cfg.OllamaBaseURL, Default: cfg.OllamaModelGen}), nil
	default:
		return nil, fmt.Errorf("unknown LLM_PROVIDER %q (want openai|ollama)", cfg.LLMProvider)
	}
}
