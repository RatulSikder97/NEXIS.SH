// Package config loads runtime config for the auth service.
package config

import (
	"fmt"

	"nexis/backend/internal/platform/env"
)

type Config struct {
	Port                string
	DatabaseURL         string
	ClerkWebhookSecret  string
	ClerkAudience       string
	ClerkIssuer         string
	APIKeyDefaultExpiry int // days
	AllowOrigins        []string
	Environment         string
}

func Load() (Config, error) {
	c := Config{
		Port:                env.Get("PORT", "8084"),
		DatabaseURL:         env.Get("DATABASE_URL", ""),
		ClerkWebhookSecret:  env.Get("CLERK_WEBHOOK_SECRET", ""),
		ClerkAudience:       env.Get("CLERK_AUDIENCE", ""),
		ClerkIssuer:         env.Get("CLERK_ISSUER", ""),
		APIKeyDefaultExpiry: env.Int("API_KEY_DEFAULT_EXPIRY_DAYS", 365),
		AllowOrigins:        []string{env.Get("CONSOLE_ORIGIN", "http://localhost:3001")},
		Environment:         env.Get("APP_ENV", "dev"),
	}
	if c.Environment == "prod" && c.ClerkWebhookSecret == "" {
		return c, fmt.Errorf("config: CLERK_WEBHOOK_SECRET required in prod")
	}
	return c, nil
}
