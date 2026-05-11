// Package config loads runtime configuration from environment variables.
// All env-reads happen here; no other package should call os.Getenv directly.
package config

import "os"

type Config struct {
	Port             string
	AppEnv           string
	LLMProvider      string
	OpenAIAPIKey     string
	OpenAIModelSyn   string
	OpenAIModelCheap string
	OllamaBaseURL    string
	OllamaModelGen   string
	OllamaModelCode  string
	DatabaseURL      string
}

func Load() Config {
	return Config{
		Port:             env("PORT", "8080"),
		AppEnv:           env("APP_ENV", "dev"),
		LLMProvider:      env("LLM_PROVIDER", "openai"),
		OpenAIAPIKey:     env("OPENAI_API_KEY", ""),
		OpenAIModelSyn:   env("OPENAI_MODEL_SYNTHESIS", "gpt-4o"),
		OpenAIModelCheap: env("OPENAI_MODEL_CHEAP", "gpt-4o-mini"),
		OllamaBaseURL:    env("OLLAMA_BASE_URL", "http://host.docker.internal:11434"),
		OllamaModelGen:   env("OLLAMA_MODEL_GENERAL", "llama3.1:8b"),
		OllamaModelCode:  env("OLLAMA_MODEL_CODE", "qwen2.5-coder:14b"),
		DatabaseURL:      env("DATABASE_URL", ""),
	}
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}
