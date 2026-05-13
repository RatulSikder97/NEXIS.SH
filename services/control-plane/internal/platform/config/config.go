// Package config loads runtime configuration from environment variables.
// All env-reads happen here; no other package should call os.Getenv directly.
package config

import (
	"os"
	"strconv"
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

	// Phase 3.5
	BillingProvider        string  // "local" | "stripe"
	UsageTickSeconds       int     // default 60
	WorkspaceProvisionFail float64 // default 0.0 — fraction of provisioning runs that should fail synthetically

	// Phase 4 — Temporal + validator + patch storage.
	TemporalHostPort  string // 'temporal:7233' in compose; Cloud HostPort in Phase 7
	TemporalNamespace string // 'default' for dev; 'nexis-prod' in Phase 7
	TemporalTaskQueue string // 'nexis-recovery'
	ValidatorURL      string // 'http://validator:8081'
	ValidatorToken    string // shared bearer between control-plane + validator
	PatchStore        string // 'minio' | 's3'
	MinIOEndpoint     string
	MinIOAccessKey    string
	MinIOSecretKey    string
	MinIOUseSSL       bool
	WorkflowStubDurationMs int // default 0 — when >0 each stub activity sleeps this long

	// Phase 5 — agents L1 + LLM spine.
	TokenBudgetTokensIn       int64
	TokenBudgetTokensOut      int64
	TokenBudgetPeriodDays     int
	OpenAIEmbedModel          string
	OllamaEmbedModel          string
	OpenAIPromptCache         bool
	EvalEnabled               bool
	AgentSchemaRetryMax       int
	AgentModelArchitectOpenAI string
	AgentModelBackendOpenAI   string
	AgentModelQAOpenAI        string
	AgentModelDevOpsOpenAI    string
	AgentModelDataEngOpenAI   string
	AgentModelArchitectOllama string
	AgentModelBackendOllama   string
	AgentModelQAOllama        string
	AgentModelDevOpsOllama    string
	AgentModelDataEngOllama   string
	// FixtureSHA is the resolved repo SHA used as `fixture-<sha>` for retrieval.
	// Filled at server start by the seed CLI / pipeline_demo; empty is OK.
	FixtureSHA string
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

		BillingProvider:        env("BILLING_PROVIDER", "local"),
		UsageTickSeconds:       envInt("USAGE_TICK_SECONDS", 60),
		WorkspaceProvisionFail: envFloat("WORKSPACE_PROVISION_FAIL", 0.0),

		TemporalHostPort:       env("TEMPORAL_HOST_PORT", "temporal:7233"),
		TemporalNamespace:      env("TEMPORAL_NAMESPACE", "default"),
		TemporalTaskQueue:      env("TEMPORAL_TASK_QUEUE", "nexis-recovery"),
		ValidatorURL:           env("VALIDATOR_URL", "http://validator:8081"),
		ValidatorToken:         env("VALIDATOR_TOKEN", "dev-validator-token-32byte"),
		PatchStore:             env("PATCH_STORE", "minio"),
		MinIOEndpoint:          env("MINIO_ENDPOINT", "minio:9000"),
		MinIOAccessKey:         env("MINIO_ACCESS_KEY", "nexis"),
		MinIOSecretKey:         env("MINIO_SECRET_KEY", "nexis_dev_password"),
		MinIOUseSSL:            parseBool(env("MINIO_USE_SSL", "0")),
		WorkflowStubDurationMs: envInt("WORKFLOW_STUB_DURATION_MS", 0),

		// Phase 5
		TokenBudgetTokensIn:   int64(envInt("TOKEN_BUDGET_TOKENS_IN", 1_000_000)),
		TokenBudgetTokensOut:  int64(envInt("TOKEN_BUDGET_TOKENS_OUT", 200_000)),
		TokenBudgetPeriodDays: envInt("TOKEN_BUDGET_PERIOD_DAYS", 30),
		OpenAIEmbedModel:      env("OPENAI_EMBED_MODEL", "text-embedding-3-small"),
		OllamaEmbedModel:      env("OLLAMA_EMBED_MODEL", "nomic-embed-text"),
		OpenAIPromptCache:     parseBool(env("OPENAI_PROMPT_CACHE", "1")),
		EvalEnabled:           parseBool(env("EVAL_ENABLED", "1")),
		AgentSchemaRetryMax:   envInt("AGENT_SCHEMA_RETRY_MAX", 2),

		AgentModelArchitectOpenAI: env("AGENT_MODEL_ARCHITECT_OPENAI", "gpt-4o-mini"),
		AgentModelBackendOpenAI:   env("AGENT_MODEL_BACKEND_OPENAI", "gpt-4o"),
		AgentModelQAOpenAI:        env("AGENT_MODEL_QA_OPENAI", "gpt-4o-mini"),
		AgentModelDevOpsOpenAI:    env("AGENT_MODEL_DEVOPS_OPENAI", "gpt-4o-mini"),
		AgentModelDataEngOpenAI:   env("AGENT_MODEL_DATAENG_OPENAI", "gpt-4o-mini"),

		AgentModelArchitectOllama: env("AGENT_MODEL_ARCHITECT_OLLAMA", "llama3.1:8b"),
		AgentModelBackendOllama:   env("AGENT_MODEL_BACKEND_OLLAMA", "qwen2.5-coder:14b"),
		AgentModelQAOllama:        env("AGENT_MODEL_QA_OLLAMA", "llama3.1:8b"),
		AgentModelDevOpsOllama:    env("AGENT_MODEL_DEVOPS_OLLAMA", "llama3.1:8b"),
		AgentModelDataEngOllama:   env("AGENT_MODEL_DATAENG_OLLAMA", "llama3.1:8b"),
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

// envInt reads an int env var, returning def when the var is unset or unparseable.
func envInt(k string, def int) int {
	v, ok := os.LookupEnv(k)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}

// envFloat reads a float env var, returning def when the var is unset or unparseable.
func envFloat(k string, def float64) float64 {
	v, ok := os.LookupEnv(k)
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return def
	}
	return f
}
