package secrets

// Coverage for the env-backed secrets store. The AWS Secrets Manager path
// requires the AWS SDK; that lives behind an integration build tag if at
// all. Here we cover EnvStore + the normalize helper.

import (
	"context"
	"testing"
)

// TestEnvStore_GetFromMemory — Put then Get returns the stored bytes.
func TestEnvStore_GetFromMemory(t *testing.T) {
	s := NewEnv()
	if err := s.Put(context.Background(), "stripe.api_key", []byte("sk_test_abc")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(context.Background(), "stripe.api_key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "sk_test_abc" {
		t.Fatalf("got %q want %q", got, "sk_test_abc")
	}
}

// TestEnvStore_GetFromEnv — when no Put has run, fall back to os.Getenv.
// Use t.Setenv so the test cleans up after itself.
func TestEnvStore_GetFromEnv(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "from-env")
	s := NewEnv()
	got, err := s.Get(context.Background(), "stripe.api_key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "from-env" {
		t.Fatalf("got %q want from-env", got)
	}
}

// TestEnvStore_GetMissing — neither memory nor env yields a value → error.
func TestEnvStore_GetMissing(t *testing.T) {
	s := NewEnv()
	_, err := s.Get(context.Background(), "totally.missing.key")
	if err == nil {
		t.Fatalf("expected error for missing key")
	}
}

// TestEnvStore_MemoryShadowsEnv — Put overrides the env var lookup.
func TestEnvStore_MemoryShadowsEnv(t *testing.T) {
	t.Setenv("FOO_BAR", "from-env")
	s := NewEnv()
	if err := s.Put(context.Background(), "foo.bar", []byte("from-mem")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, _ := s.Get(context.Background(), "foo.bar")
	if string(got) != "from-mem" {
		t.Fatalf("memory must shadow env: got %q", got)
	}
}

// TestNormalize — dotted, dashed, and slashed names all canonicalise to
// SCREAMING_SNAKE.
func TestNormalize(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"stripe.api_key", "STRIPE_API_KEY"},
		{"foo-bar", "FOO_BAR"},
		{"path/to/secret", "PATH_TO_SECRET"},
		{"already_upper", "ALREADY_UPPER"},
		{"mixed.Case-name", "MIXED_CASE_NAME"},
	}
	for _, tc := range cases {
		got := normalize(tc.in)
		if got != tc.want {
			t.Fatalf("normalize(%q): got %q want %q", tc.in, got, tc.want)
		}
	}
}

// TestEnvStore_EmptyEnvCountsAsMissing — env var set to "" returns the
// not-found error (the empty string is treated as un-set).
func TestEnvStore_EmptyEnvCountsAsMissing(t *testing.T) {
	t.Setenv("EMPTY_SECRET", "")
	s := NewEnv()
	_, err := s.Get(context.Background(), "empty.secret")
	if err == nil {
		t.Fatalf("empty env value must surface as missing")
	}
}
