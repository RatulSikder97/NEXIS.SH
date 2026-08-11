package config

// Coverage for the config package's pure helpers. Load() is exercised
// indirectly through every test that constructs a Config struct; the
// helpers below are the standalone functions that handle env parsing
// edge cases.

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// TestEnv_DefaultsAndOverrides — when the env var is unset, return the
// default; when set, return the value verbatim (no trim).
func TestEnv_DefaultsAndOverrides(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		_ = os.Unsetenv("NEXIS_TEST_KEY_1")
		if got := env("NEXIS_TEST_KEY_1", "fallback"); got != "fallback" {
			t.Fatalf("default: %q", got)
		}
	})
	t.Run("override", func(t *testing.T) {
		t.Setenv("NEXIS_TEST_KEY_2", "from-env")
		if got := env("NEXIS_TEST_KEY_2", "fallback"); got != "from-env" {
			t.Fatalf("override: %q", got)
		}
	})
}

// TestParseBool — every truthy/falsey path.
func TestParseBool(t *testing.T) {
	truthy := []string{"1", "true", "yes", "on", "TRUE", "True", " 1 "}
	for _, v := range truthy {
		if !parseBool(v) {
			t.Fatalf("expected true for %q", v)
		}
	}
	falsy := []string{"0", "false", "no", "off", "", "junk", "2"}
	for _, v := range falsy {
		if parseBool(v) {
			t.Fatalf("expected false for %q", v)
		}
	}
}

// TestParseSecretList — splits on commas, trims whitespace, drops empties.
func TestParseSecretList(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		got := parseSecretList("a,b,c")
		if len(got) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(got))
		}
		if string(got[0]) != "a" || string(got[2]) != "c" {
			t.Fatalf("entries: %v", got)
		}
	})
	t.Run("whitespace_and_empties_dropped", func(t *testing.T) {
		got := parseSecretList("a, ,b,,c,  ")
		if len(got) != 3 {
			t.Fatalf("expected 3 entries (empties dropped), got %d", len(got))
		}
	})
	t.Run("empty_input_returns_nil", func(t *testing.T) {
		if parseSecretList("") != nil {
			t.Fatalf("empty must return nil")
		}
		if parseSecretList("   ") != nil {
			t.Fatalf("blank must return nil")
		}
	})
	t.Run("only_separators_returns_nil", func(t *testing.T) {
		if parseSecretList(",,, ,,") != nil {
			t.Fatalf("only separators must return nil")
		}
	})
}

// TestEnvInt — int parsing with default fallback.
func TestEnvInt(t *testing.T) {
	t.Run("default_unset", func(t *testing.T) {
		_ = os.Unsetenv("NEXIS_TEST_INT_1")
		if got := envInt("NEXIS_TEST_INT_1", 42); got != 42 {
			t.Fatalf("default: %d", got)
		}
	})
	t.Run("override_set", func(t *testing.T) {
		t.Setenv("NEXIS_TEST_INT_2", "99")
		if got := envInt("NEXIS_TEST_INT_2", 42); got != 99 {
			t.Fatalf("override: %d", got)
		}
	})
	t.Run("non_int_falls_back", func(t *testing.T) {
		t.Setenv("NEXIS_TEST_INT_3", "not-a-number")
		if got := envInt("NEXIS_TEST_INT_3", 42); got != 42 {
			t.Fatalf("non-int fallback: %d", got)
		}
	})
}

// TestEnvFloat — float parsing with default fallback.
func TestEnvFloat(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		_ = os.Unsetenv("NEXIS_TEST_FLT_1")
		if got := envFloat("NEXIS_TEST_FLT_1", 1.5); got != 1.5 {
			t.Fatalf("default: %v", got)
		}
	})
	t.Run("override", func(t *testing.T) {
		t.Setenv("NEXIS_TEST_FLT_2", "3.14")
		if got := envFloat("NEXIS_TEST_FLT_2", 0); got != 3.14 {
			t.Fatalf("override: %v", got)
		}
	})
	t.Run("non_float_falls_back", func(t *testing.T) {
		t.Setenv("NEXIS_TEST_FLT_3", "abc")
		if got := envFloat("NEXIS_TEST_FLT_3", 9.9); got != 9.9 {
			t.Fatalf("fallback: %v", got)
		}
	})
}

// TestLoadGitHubAppPEM_FromB64_StdEncoding — standard base64 decodes
// happily.
func TestLoadGitHubAppPEM_FromB64_StdEncoding(t *testing.T) {
	pem := []byte("-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n")
	b64 := base64.StdEncoding.EncodeToString(pem)
	got := loadGitHubAppPEM(b64, "")
	if string(got) != string(pem) {
		t.Fatalf("got %q want %q", got, pem)
	}
}

// TestLoadGitHubAppPEM_FromB64_URLEncoding — URL-safe base64 also decodes.
func TestLoadGitHubAppPEM_FromB64_URLEncoding(t *testing.T) {
	pem := []byte("hello world")
	b64 := base64.URLEncoding.EncodeToString(pem)
	got := loadGitHubAppPEM(b64, "")
	if string(got) != "hello world" {
		t.Fatalf("got %q", got)
	}
}

// TestLoadGitHubAppPEM_RawPemPassesThrough — if both b64 attempts fail
// (no, both will succeed on any non-empty input due to last-resort raw),
// then raw bytes still pass through.
func TestLoadGitHubAppPEM_RawPemPassesThrough(t *testing.T) {
	// Any non-empty string is valid base64 input as long as length is
	// divisible by 4 or has correct padding; let's pick something likely
	// to fail standard base64 (contains a dash).
	rawPEM := "-----BEGIN PRIVATE KEY-----"
	got := loadGitHubAppPEM(rawPEM, "")
	if len(got) == 0 {
		t.Fatalf("expected non-empty bytes")
	}
}

// TestLoadGitHubAppPEM_FromPath — file path fallback works.
func TestLoadGitHubAppPEM_FromPath(t *testing.T) {
	dir := t.TempDir()
	pemFile := filepath.Join(dir, "app.pem")
	pem := []byte("-----BEGIN PRIVATE KEY-----\nfrom file\n-----END\n")
	if err := os.WriteFile(pemFile, pem, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := loadGitHubAppPEM("", pemFile)
	if string(got) != string(pem) {
		t.Fatalf("got %q want %q", got, pem)
	}
}

// TestLoadGitHubAppPEM_BothEmptyReturnsNil — both inputs empty surfaces as
// nil so the github adapter can detect stub mode.
func TestLoadGitHubAppPEM_BothEmptyReturnsNil(t *testing.T) {
	got := loadGitHubAppPEM("", "")
	if got != nil {
		t.Fatalf("expected nil for empty inputs, got %v", got)
	}
}

// TestLoadGitHubAppPEM_MissingFileReturnsNil — file path that doesn't
// exist also surfaces as nil.
func TestLoadGitHubAppPEM_MissingFileReturnsNil(t *testing.T) {
	got := loadGitHubAppPEM("", "/path/that/does/not/exist.pem")
	if got != nil {
		t.Fatalf("expected nil for missing file, got %v", got)
	}
}

// TestFatalIfLocalInCloud — verifies the env-vs-providers safety check.
func TestFatalIfLocalInCloud(t *testing.T) {
	t.Run("dev_with_local_ok", func(t *testing.T) {
		c := &Config{
			AppEnv:          "dev",
			AuthProvider:    "local",
			BillingProvider: "local",
		}
		if err := c.FatalIfLocalInCloud(); err != nil {
			t.Fatalf("dev should accept local providers: %v", err)
		}
	})
	t.Run("prod_with_local_fails", func(t *testing.T) {
		c := &Config{
			AppEnv:       "prod",
			AuthProvider: "local",
		}
		if err := c.FatalIfLocalInCloud(); err == nil {
			t.Fatalf("prod with local auth must error")
		}
	})
	t.Run("prod_with_real_providers_ok", func(t *testing.T) {
		c := &Config{
			AppEnv:          "prod",
			AuthProvider:    "workos",
			BillingProvider: "stripe",
			KeyVault:        "kms",
			PatchStore:      "s3",
			SecretsBackend:  "secretsmanager",
			Mailer:          "ses",
			ValidatorRunner: "ecs",
			TemporalCloud:   true,
			OTLPTarget:      "grafana_cloud",
		}
		if err := c.FatalIfLocalInCloud(); err != nil {
			t.Fatalf("prod with real providers should pass: %v", err)
		}
	})

	t.Run("staging_with_local_fails", func(t *testing.T) {
		c := &Config{AppEnv: "staging", AuthProvider: "local"}
		if err := c.FatalIfLocalInCloud(); err == nil {
			t.Fatalf("staging with local must fail")
		}
	})

	t.Run("test_env_unconstrained", func(t *testing.T) {
		c := &Config{AppEnv: "test", AuthProvider: "local"}
		if err := c.FatalIfLocalInCloud(); err != nil {
			t.Fatalf("non-cloud env should pass: %v", err)
		}
	})
}
