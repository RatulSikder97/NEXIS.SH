// Package config loads runtime configuration from environment variables.
// All env-reads happen here; no other package should call os.Getenv directly.
package config

import (
	"os"
	"strings"
)

type Config struct {
	// HTTP + runtime
	Port   string
	AppEnv string

	// LLM (Phase 1)
	LLMProvider      string
	OpenAIAPIKey     string
	OpenAIModelSyn   string
	OpenAIModelCheap string
	OllamaBaseURL    string
	OllamaModelGen   string
	OllamaModelCode  string

	// Database — Phase 1 used DATABASE_URL (superuser/owner) for migrations
	// and dev-time admin queries. Phase 2 introduces DATABASE_URL_APP, used
	// at runtime by the nexis_app non-superuser role.
	DatabaseURL    string
	DatabaseURLApp string

	// Auth (Phase 2)
	AuthProvider    string // "local" | "workos"
	AllowStubWorkOS bool
	SessionSecret   string // 32-byte base64
	AuditSecret     string // 32-byte base64, independent from SessionSecret
	SMTPHost        string
	SMTPPort        string
	SMTPFrom        string
	AppBaseURL      string // used to build magic-link URLs

	// Observability (Phase 2)
	OTelServiceName string
	OTelEndpoint    string

	// Phase 3
	MasterKey                  string // 32-byte base64 for LocalKeyVault
	GitHubDefaultWebhookSecret string
}

func Load() Config {
	return Config{
		Port:   env("PORT", "8080"),
		AppEnv: env("APP_ENV", "dev"),

		LLMProvider:      env("LLM_PROVIDER", "openai"),
		OpenAIAPIKey:     env("OPENAI_API_KEY", ""),
		OpenAIModelSyn:   env("OPENAI_MODEL_SYNTHESIS", "gpt-4o"),
		OpenAIModelCheap: env("OPENAI_MODEL_CHEAP", "gpt-4o-mini"),
		OllamaBaseURL:    env("OLLAMA_BASE_URL", "http://host.docker.internal:11434"),
		OllamaModelGen:   env("OLLAMA_MODEL_GENERAL", "llama3.1:8b"),
		OllamaModelCode:  env("OLLAMA_MODEL_CODE", "qwen2.5-coder:14b"),

		DatabaseURL:    env("DATABASE_URL", ""),
		DatabaseURLApp: env("DATABASE_URL_APP", ""),

		AuthProvider:    env("AUTH_PROVIDER", "local"),
		AllowStubWorkOS: parseBool(env("ALLOW_STUB_WORKOS", "0")),
		SessionSecret:   env("SESSION_SECRET", ""),
		AuditSecret:     env("AUDIT_SECRET", ""),
		SMTPHost:        env("SMTP_HOST", "mailhog"),
		SMTPPort:        env("SMTP_PORT", "1025"),
		SMTPFrom:        env("SMTP_FROM", "no-reply@nexis.local"),
		AppBaseURL:      env("APP_BASE_URL", "http://localhost:3000"),

		OTelServiceName: env("OTEL_SERVICE_NAME", "nexis-control-plane"),
		OTelEndpoint:    env("OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4318"),

		MasterKey:                  env("MASTER_KEY", ""),
		GitHubDefaultWebhookSecret: env("GITHUB_WEBHOOK_SECRET", "dev-github-webhook-secret-32-byte"),
	}
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}

// parseBool accepts "1" or "true" (case-insensitive) as true. Anything else is false.
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
