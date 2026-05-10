// Package env provides typed loading of environment variables.
// All env reads in the codebase MUST go through this package.
package env

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Get returns the env var or fallback if unset.
func Get(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// Required returns the env var or returns an error.
func Required(key string) (string, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", fmt.Errorf("env: required variable %q is not set", key)
	}
	return v, nil
}

// Int returns the env var parsed as int or fallback.
func Int(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// Bool returns the env var parsed as bool or fallback.
func Bool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		switch v {
		case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON":
			return true
		case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
			return false
		}
	}
	return fallback
}

// Duration returns the env var parsed as time.Duration or fallback.
func Duration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
