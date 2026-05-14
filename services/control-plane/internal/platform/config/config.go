// Package config loads runtime configuration from environment variables.
// All env-reads happen here; no other package should call os.Getenv directly.
package config

import (
	"encoding/base64"
	"fmt"
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

	// Sentry integration — REST base URL. Defaults to https://sentry.io; set
	// to a self-hosted Sentry's base when applicable. Consumed by
	// internal/adapter/integration/sentry to build the typed REST client.
	SentryBaseURL string

	// DatadogWebhookSigningSecret is the optional process-wide HMAC-SHA256
	// secret used by the Datadog adapter to verify the X-Datadog-Signature
	// header on inbound webhooks. When empty the adapter falls back to a
	// per-tenant secret stored alongside the API + APP keys (and accepts
	// unsigned payloads when no per-tenant secret is configured either).
	// Sourced from DATADOG_WEBHOOK_SIGNING_SECRET.
	DatadogWebhookSigningSecret []byte

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

	// Phase 6 — Agents L2 + Approval Gate + GitOps + Slack + Neo4j + Causal.
	SentinelEnabled              bool
	SentinelPollIntervalMs       int
	Neo4jURI                     string
	Neo4jUser                    string
	Neo4jPass                    string
	CausalGRPCEndpoint           string
	CausalEnabled                bool
	PathfinderLLMRefine          bool
	GitOpsURL                    string
	GitOpsToken                  string
	GitHubAppID                  int64
	GitHubAppPrivateKeyPath      string
	// GitHubAppPrivateKeyPEM is the raw PEM bytes of the GitHub App private
	// key. Sourced from GITHUB_APP_PRIVATE_KEY_PEM (base64-encoded so the env
	// var stays on one line) at boot, falling back to reading the file at
	// GitHubAppPrivateKeyPath when the env var is empty. Empty when neither
	// is configured — the github adapter operates in stub mode in that case.
	GitHubAppPrivateKeyPEM       []byte
	// GitHubAppSlug is the slug from the GitHub App settings page (e.g.
	// "nexis-recovery"). Used to build the install-redirect URL surfaced via
	// Status.Metadata.app_slug so the UI can deep-link to the App settings.
	GitHubAppSlug                string
	// GitHubWebhookSecret is the per-App webhook secret configured in the
	// GitHub App UI. Distinct from GitHubDefaultWebhookSecret, which is the
	// dev-only fallback used before any tenant has connected — once the App
	// is wired this becomes the canonical secret for HMAC verification.
	GitHubWebhookSecret          []byte
	FixtureRepoOwner             string
	FixtureRepoName              string
	FixtureRepoDefaultBranch     string
	FixtureRepoInstallationID    int64
	SlackEnabled                 bool
	ApprovalMediumTimeoutSeconds int

	// Slack OAuth v2 install + interactivity (Task 5 of real-integrations).
	//
	//   SlackClientID / SlackClientSecret — workspace-app credentials from
	//     the Slack app console. Used by oauth.v2.access to exchange the
	//     install code for a bot token.
	//
	//   SlackSigningSecret — workspace-app Signing Secret (distinct from the
	//     OAuth client secret). Used to verify the X-Slack-Signature header
	//     on POST /v1/integrations/slack/interactivity. Kept as []byte so the
	//     hmac.New call doesn't have to convert on every request.
	//
	//   SlackAppRedirectURI — the OAuth callback URL registered with Slack.
	//     Defaults to {APP_BASE_URL}/v1/integrations/slack/callback at Load
	//     time when the env var is unset, so dev wiring "just works".
	SlackClientID       string
	SlackClientSecret   []byte
	SlackSigningSecret  []byte
	SlackAppRedirectURI string

	// SlackDefaultChannel is the workspace-wide default channel id used by
	// the Approval Gate when a recovery isn't bound to a project (or the
	// bound project has no SlackChannelID set). Empty string disables the
	// default fallback — the gate stays silent on Slack in that case.
	// Sourced from SLACK_DEFAULT_CHANNEL.
	SlackDefaultChannel string

	// Phase 7 — cloud cutover selectors. Defaults are dev-safe; the
	// FatalIfLocalInCloud assertion fires at boot when AppEnv in
	// {staging, prod} and any selector is still local/dev-shaped.
	AWSRegion       string // us-east-1 default
	KeyVault        string // "local" | "kms"
	SecretsBackend  string // "env"   | "secretsmanager"
	Mailer          string // "smtp"  | "ses"
	ValidatorRunner string // "docker"| "modal"
	TemporalCloud   bool   // when true, dial Temporal Cloud over mTLS
	OTLPTarget      string // "local" | "grafana_cloud"

	// AWS adapter config. Resolved at runtime from Secrets Manager in
	// staging/prod; populated from env in dev.
	KMSKeyARN           string
	S3PatchBucket       string
	S3AuditExportBucket string
	SecretsPrefix       string // "nexis-<env>/control-plane/"

	// WorkOS (Phase 7 real impl).
	WorkOSAPIKey        string
	WorkOSClientID      string
	WorkOSRedirectURI   string
	WorkOSWebhookSecret string

	// Stripe (Phase 7 real impl).
	StripeSecretKey      string
	StripePublishableKey string
	StripeWebhookSecret  string
	StripePriceRuntime   string
	StripePriceEvents    string
	StripePriceTokens    string

	// Mailer (SES).
	SESFromAddress string

	// Temporal Cloud — mTLS dial.
	TemporalCloudNamespace string
	TemporalCloudTLSCertPath string
	TemporalCloudTLSKeyPath  string

	// Grafana Cloud OTLP.
	GrafanaCloudOTLPEndpoint string
	GrafanaCloudOTLPToken    string

	// Modal validator runner.
	ModalAppURL string
	ModalToken  string

	// Audit anchor cron.
	AuditAnchorBucket string
	AuditAnchorCron   string

	// Phase 8 — public-beta surface.
	//
	//   SignupRequiresInvite — when true, POST /v1/auth/signup requires
	//     ?invite=<code> and rejects with 403 if missing or invalid. Defaults
	//     to false so the existing dev path keeps working.
	//
	//   SentryProbeSecret — HMAC secret for the BetterStack probe at
	//     POST /v1/integrations/sentry/probe. Falls back to
	//     GitHubDefaultWebhookSecret in dev so the probe works without
	//     dedicated env wiring.
	//
	//   TemporalHealthzWindowSeconds — staleness budget for the temporal
	//     heartbeat. Defaults to 60.
	//
	//   TemporalHeartbeatIntervalMs — how often the heartbeat goroutine
	//     pings Temporal. Defaults to 15000ms so a ~1/4 of the staleness
	//     window is covered by every ping.
	SignupRequiresInvite         bool
	SentryProbeSecret            string
	TemporalHealthzWindowSeconds int
	TemporalHeartbeatIntervalMs  int

	// PagerDuty integration. PagerDutyFromEmail is addressed in the
	// `From:` header on every POST /incidents (PagerDuty rejects the call
	// without it — the header names a real user creating the incident on
	// behalf of the bot). PagerDutyWebhookSecrets is the rotation set used
	// to verify X-PagerDuty-Signature on every inbound webhook —
	// comma-separated in env so operators can roll the secret without
	// dropping in-flight deliveries (we accept ANY match).
	PagerDutyFromEmail      string
	PagerDutyWebhookSecrets [][]byte

	// Per-org rate limiting (SQA F-7). The middleware uses a
	// token-bucket per principal.OrgID. Defaults are tuned for the
	// public-beta surface: ~100 req/min steady state with a burst of
	// 200 to absorb a UI page-load that fires many parallel reads.
	//
	//   RateLimitPerOrgRPS    — steady-state requests/sec (default 1.67 → 100/min)
	//   RateLimitBurst        — token-bucket burst capacity (default 200)
	//   RateLimitEnabled      — operator escape hatch; defaults to true.
	//                           Set RATE_LIMIT_ENABLED=0 to disable the
	//                           middleware entirely.
	//
	// On exceed the middleware emits 429 + Retry-After (seconds until
	// the next token replenishes, rounded up to 1).
	RateLimitPerOrgRPS float64
	RateLimitBurst     int
	RateLimitEnabled   bool

	// DefaultBudgetTokensPerDay — operator-tunable token cap per org
	// per day. Mirrors TokenBudgetTokensIn for surfaces that want a
	// "per-day" framing; the actual enforcement still runs through
	// TokenLedgerRepo's period rows so the existing budget pill and
	// per-period rotation logic keep working.
	//
	// Default: 1_000_000 tokens/day. Sourced from
	// DEFAULT_BUDGET_TOKENS_PER_DAY.
	DefaultBudgetTokensPerDay int64
}

func Load() Config {
	c := Config{
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

		SentryBaseURL: env("SENTRY_BASE_URL", "https://sentry.io"),

		DatadogWebhookSigningSecret: []byte(env("DATADOG_WEBHOOK_SIGNING_SECRET", "")),

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

		// Phase 6 — Agents L2 + Approval Gate + GitOps + Slack + Neo4j + Causal.
		SentinelEnabled:              parseBool(env("SENTINEL_ENABLED", "1")),
		SentinelPollIntervalMs:       envInt("SENTINEL_POLL_INTERVAL_MS", 10000),
		Neo4jURI:                     env("NEO4J_URI", "bolt://neo4j:7687"),
		Neo4jUser:                    env("NEO4J_USER", "neo4j"),
		Neo4jPass:                    env("NEO4J_PASS", "nexis_dev_password"),
		CausalGRPCEndpoint:           env("CAUSAL_GRPC_ENDPOINT", "causal-inference:8090"),
		CausalEnabled:                parseBool(env("CAUSAL_ENABLED", "1")),
		PathfinderLLMRefine:          parseBool(env("PATHFINDER_LLM_REFINE", "0")),
		GitOpsURL:                    env("GITOPS_URL", "http://gitops:8082"),
		GitOpsToken:                  env("GITOPS_TOKEN", "dev-gitops-token-32byte"),
		GitHubAppID:                  int64(envInt("GITHUB_APP_ID", 12345)),
		GitHubAppPrivateKeyPath:      env("GITHUB_APP_PRIVATE_KEY_PATH", "/run/secrets/github-app.pem"),
		GitHubAppPrivateKeyPEM:       loadGitHubAppPEM(env("GITHUB_APP_PRIVATE_KEY_PEM", ""), env("GITHUB_APP_PRIVATE_KEY_PATH", "")),
		GitHubAppSlug:                env("GITHUB_APP_SLUG", ""),
		GitHubWebhookSecret:          []byte(env("GITHUB_WEBHOOK_SECRET", "")),
		FixtureRepoOwner:             env("FIXTURE_REPO_OWNER", "nexis-eco"),
		FixtureRepoName:              env("FIXTURE_REPO_NAME", "fixture-recovery-demo"),
		FixtureRepoDefaultBranch:     env("FIXTURE_REPO_DEFAULT_BRANCH", "main"),
		FixtureRepoInstallationID:    int64(envInt("FIXTURE_REPO_INSTALLATION_ID", 98765)),
		SlackEnabled:                 parseBool(env("SLACK_ENABLED", "1")),
		ApprovalMediumTimeoutSeconds: envInt("APPROVAL_MEDIUM_TIMEOUT_SECONDS", 120),

		// Phase 7 — cloud cutover selectors.
		AWSRegion:       env("AWS_REGION", "us-east-1"),
		KeyVault:        env("KEYVAULT", "local"),
		SecretsBackend:  env("SECRETS", "env"),
		Mailer:          env("MAILER", "smtp"),
		ValidatorRunner: env("VALIDATOR_RUNNER", "docker"),
		TemporalCloud:   parseBool(env("TEMPORAL_CLOUD", "0")),
		OTLPTarget:      env("OTLP_TARGET", "local"),

		KMSKeyARN:           env("KMS_KEY_ARN", ""),
		S3PatchBucket:       env("S3_PATCH_BUCKET", ""),
		S3AuditExportBucket: env("S3_AUDIT_EXPORT_BUCKET", ""),
		SecretsPrefix:       env("SECRETS_PREFIX", ""),

		WorkOSAPIKey:        env("WORKOS_API_KEY", ""),
		WorkOSClientID:      env("WORKOS_CLIENT_ID", ""),
		WorkOSRedirectURI:   env("WORKOS_REDIRECT_URI", ""),
		WorkOSWebhookSecret: env("WORKOS_WEBHOOK_SECRET", ""),

		StripeSecretKey:      env("STRIPE_SECRET_KEY", ""),
		StripePublishableKey: env("STRIPE_PUBLISHABLE_KEY", ""),
		StripeWebhookSecret:  env("STRIPE_WEBHOOK_SECRET", ""),
		StripePriceRuntime:   env("STRIPE_PRICE_RUNTIME", ""),
		StripePriceEvents:    env("STRIPE_PRICE_EVENTS", ""),
		StripePriceTokens:    env("STRIPE_PRICE_TOKENS", ""),

		SESFromAddress: env("SES_FROM_ADDRESS", "noreply@nexis.dev"),

		TemporalCloudNamespace:   env("TEMPORAL_CLOUD_NAMESPACE", ""),
		TemporalCloudTLSCertPath: env("TEMPORAL_CLOUD_TLS_CERT_PATH", ""),
		TemporalCloudTLSKeyPath:  env("TEMPORAL_CLOUD_TLS_KEY_PATH", ""),

		GrafanaCloudOTLPEndpoint: env("GRAFANA_CLOUD_OTLP_ENDPOINT", ""),
		GrafanaCloudOTLPToken:    env("GRAFANA_CLOUD_OTLP_TOKEN", ""),

		ModalAppURL: env("MODAL_APP_URL", ""),
		ModalToken:  env("MODAL_TOKEN", ""),

		AuditAnchorBucket: env("AUDIT_ANCHOR_BUCKET", ""),
		AuditAnchorCron:   env("AUDIT_ANCHOR_CRON", "0 2 * * *"),

		// Phase 8 — public-beta surface.
		SignupRequiresInvite:         parseBool(env("SIGNUP_REQUIRES_INVITE", "0")),
		SentryProbeSecret:            env("SENTRY_PROBE_SECRET", ""),
		TemporalHealthzWindowSeconds: envInt("TEMPORAL_HEALTHZ_WINDOW_SECONDS", 60),
		TemporalHeartbeatIntervalMs:  envInt("TEMPORAL_HEARTBEAT_INTERVAL_MS", 15000),

		// PagerDuty.
		PagerDutyFromEmail:      env("PAGERDUTY_FROM_EMAIL", "nexis-bot@nexis.dev"),
		PagerDutyWebhookSecrets: parseSecretList(env("PAGERDUTY_WEBHOOK_SECRETS", "")),

		// Slack OAuth v2 + interactivity (Task 5 of real-integrations).
		// SlackAppRedirectURI defaults to {AppBaseURL}/v1/integrations/slack/callback
		// after the struct is built; see the post-process below so the default
		// can reference AppBaseURL.
		SlackClientID:       env("SLACK_CLIENT_ID", ""),
		SlackClientSecret:   []byte(env("SLACK_CLIENT_SECRET", "")),
		SlackSigningSecret:  []byte(env("SLACK_SIGNING_SECRET", "")),
		SlackAppRedirectURI: env("SLACK_APP_REDIRECT_URI", ""),
		SlackDefaultChannel: env("SLACK_DEFAULT_CHANNEL", ""),

		// Per-org rate limit (SQA F-7). Defaults yield 100 req/min steady
		// state with a 200 burst — a comfortable headroom for a page-load
		// that fires several parallel reads.
		RateLimitPerOrgRPS: envFloat("RATE_LIMIT_PER_ORG_RPS", 100.0/60.0),
		RateLimitBurst:     envInt("RATE_LIMIT_BURST", 200),
		RateLimitEnabled:   parseBool(env("RATE_LIMIT_ENABLED", "1")),

		// Default per-org per-day token budget. 1M tokens covers ~20
		// full recovery runs at current per-run cost. Operators raise
		// this for paying customers via the per-org override row in
		// token_budgets.
		DefaultBudgetTokensPerDay: int64(envInt("DEFAULT_BUDGET_TOKENS_PER_DAY", 1_000_000)),
	}
	// SlackAppRedirectURI default depends on AppBaseURL, so fill it in after
	// the struct literal has captured both. Keeping the default behaviour
	// explicit avoids surprising operators who set APP_BASE_URL alone and
	// expect the Slack callback to track it.
	if c.SlackAppRedirectURI == "" && c.AppBaseURL != "" {
		c.SlackAppRedirectURI = c.AppBaseURL + "/v1/integrations/slack/callback"
	}
	return c
}

// FatalIfLocalInCloud returns an error when AppEnv is staging or prod but
// any provider selector is still pointing at a local/dev value. This is the
// Phase 7 cutover contract's first line of defence (Risk 16.14).
//
// Callers should treat the returned error as fatal (os.Exit(2)).
func (c *Config) FatalIfLocalInCloud() error {
	if c.AppEnv != "staging" && c.AppEnv != "prod" {
		return nil
	}
	var bad []string
	check := func(name, value, devValue string) {
		if value == devValue || value == "" {
			bad = append(bad, name+"="+value)
		}
	}
	check("AUTH_PROVIDER", c.AuthProvider, "local")
	check("BILLING_PROVIDER", c.BillingProvider, "local")
	check("PATCH_STORE", c.PatchStore, "minio")
	check("KEYVAULT", c.KeyVault, "local")
	check("SECRETS", c.SecretsBackend, "env")
	check("MAILER", c.Mailer, "smtp")
	check("VALIDATOR_RUNNER", c.ValidatorRunner, "docker")
	if !c.TemporalCloud {
		bad = append(bad, "TEMPORAL_CLOUD=0")
	}
	if c.OTLPTarget != "grafana_cloud" {
		bad = append(bad, "OTLP_TARGET="+c.OTLPTarget)
	}
	if len(bad) > 0 {
		return fmt.Errorf("cloud env %q has local providers: %v", c.AppEnv, bad)
	}
	return nil
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

// parseSecretList splits a comma-separated env value into a rotation set of
// secret byte slices. Empty entries (a trailing comma, double comma,
// whitespace-only) are dropped so operators can format the env value
// however they like. Returns nil for an empty/blank input so callers can
// distinguish "unset" from "set to []".
func parseSecretList(raw string) [][]byte {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([][]byte, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, []byte(p))
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

// loadGitHubAppPEM resolves the GitHub App private key bytes from one of two
// sources, with env-var-first precedence:
//
//  1. GITHUB_APP_PRIVATE_KEY_PEM — base64-encoded PEM. Convenient for
//     12-factor deployments where the secret comes from a vault and lands in
//     env. We base64-decode so newlines do not have to be escaped.
//  2. GITHUB_APP_PRIVATE_KEY_PATH — file path. Convenient for Docker secrets
//     and the dev compose file (/run/secrets/github-app.pem).
//
// Both unset → returns nil; the github adapter operates in stub mode then.
// Decode failures fall through to the file path; file errors return nil so a
// missing key does not crash boot — the adapter logs at init when it sees
// nil PEM bytes.
func loadGitHubAppPEM(b64, path string) []byte {
	if b64 != "" {
		// Try standard base64 first, then URL-encoded — operators copy-paste
		// from various sources and we want both shapes to "just work".
		if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64)); err == nil && len(decoded) > 0 {
			return decoded
		}
		if decoded, err := base64.URLEncoding.DecodeString(strings.TrimSpace(b64)); err == nil && len(decoded) > 0 {
			return decoded
		}
		// Last resort: treat the env value as raw PEM (some operators paste
		// the PEM with \n already in it — this preserves that path).
		return []byte(b64)
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return data
		}
	}
	return nil
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
