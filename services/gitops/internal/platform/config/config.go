// Package config loads the gitops service's environment configuration. All
// values are sourced from environment variables — there is no config file
// (the service is a single binary with a small surface).
package config

import (
	"os"
	"strconv"
)

// Config is the resolved environment-driven settings the gitops service uses.
// Fields map 1:1 to the env vars listed in their tags.
type Config struct {
	Port                    string // PORT (default 8082)
	DatabaseURL             string // DATABASE_URL (required for InstallationID lookup)
	GitOpsToken             string // GITOPS_AUTH_TOKEN (required) — bearer secret control-plane sends
	GitHubAppID             int64  // GITHUB_APP_ID (required for go-github transport)
	GitHubAppPrivateKeyPath string // GITHUB_APP_PRIVATE_KEY_PATH (required — PEM file)
	LogLevel                string // LOG_LEVEL (default info)
}

// Load reads the env vars and returns a Config. Missing required fields are
// returned as empty values — main.go validates + logs which keys are blank
// before crashing.
func Load() Config {
	return Config{
		Port:                    env("PORT", "8082"),
		DatabaseURL:             env("DATABASE_URL", ""),
		GitOpsToken:             env("GITOPS_AUTH_TOKEN", ""),
		GitHubAppID:             envInt64("GITHUB_APP_ID", 0),
		GitHubAppPrivateKeyPath: env("GITHUB_APP_PRIVATE_KEY_PATH", ""),
		LogLevel:                env("LOG_LEVEL", "info"),
	}
}

// env returns the value of key or d when unset/empty.
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// envInt64 parses key as int64, returning d on missing or parse error.
func envInt64(k string, d int64) int64 {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return d
	}
	return parsed
}
