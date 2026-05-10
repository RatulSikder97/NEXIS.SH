// Package config loads runtime config for the api-gateway service.
package config

import (
	"strings"

	"nexis/backend/internal/platform/env"
)

type Config struct {
	Port            string
	Environment     string
	AllowOrigins    []string
	AuthURL         string
	OrchestratorURL string
	IntegrationsURL string
	GitOpsURL       string
	ClerkAudience   string
	ClerkIssuer     string
	ClerkJWKSURL    string
	DevBypassAuth   bool
}

func Load() Config {
	originsCSV := env.Get("ALLOW_ORIGINS", "http://localhost:3000,http://localhost:3001,http://localhost:3002")
	origins := []string{}
	for _, o := range strings.Split(originsCSV, ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}
	return Config{
		Port:            env.Get("PORT", "8080"),
		Environment:     env.Get("APP_ENV", "dev"),
		AllowOrigins:    origins,
		AuthURL:         env.Get("AUTH_URL", "http://auth:8084"),
		OrchestratorURL: env.Get("ORCHESTRATOR_URL", "http://orchestrator:8081"),
		IntegrationsURL: env.Get("INTEGRATIONS_URL", "http://integrations:8088"),
		GitOpsURL:       env.Get("GITOPS_URL", "http://gitops:8087"),
		ClerkAudience:   env.Get("CLERK_AUDIENCE", ""),
		ClerkIssuer:     env.Get("CLERK_ISSUER", ""),
		ClerkJWKSURL:    env.Get("CLERK_JWKS_URL", ""),
		DevBypassAuth:   env.Bool("DEV_BYPASS_AUTH", true),
	}
}
