// Phase 2 SQA §9 — exercise the AUTH_PROVIDER decision tree:
//
//   - local (or "") → Provider, no error.
//   - workos + ALLOW_STUB_WORKOS=1 → local provider with a warning log.
//   - workos + ALLOW_STUB_WORKOS=0 → error (fail-fast at boot).
//   - anything else → unknown-provider error.
//
// The fail-fast case is the load-bearing one: silently substituting the
// local provider for "workos" would let a misconfigured prod deploy boot
// with auth=local, which is the original SQA gap this guard exists for.
package auth

import (
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

func TestNewFromConfig_Local(t *testing.T) {
	for _, name := range []string{"local", ""} {
		t.Run("provider="+name, func(t *testing.T) {
			p, err := NewFromConfig(config.Config{AuthProvider: name, SessionSecret: "x"}, nil)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if p == nil {
				t.Fatal("nil provider")
			}
			if p.Name() != "local" {
				t.Fatalf("want local, got %q", p.Name())
			}
		})
	}
}

func TestNewFromConfig_WorkOSFailFast(t *testing.T) {
	// AllowStubWorkOS=false must produce an error rather than silently
	// substitute the local provider.
	cfg := config.Config{AuthProvider: "workos", AllowStubWorkOS: false}
	p, err := NewFromConfig(cfg, nil)
	if err == nil {
		t.Fatalf("want error, got provider=%v", p)
	}
	// The error message must be operator-readable and mention the env var
	// they need to set; we assert on a substring so the exact wording can
	// evolve without breaking the test.
	if !strings.Contains(err.Error(), "ALLOW_STUB_WORKOS") {
		t.Fatalf("want error mentioning ALLOW_STUB_WORKOS, got: %v", err)
	}
}

func TestNewFromConfig_WorkOSStubAllowed(t *testing.T) {
	cfg := config.Config{AuthProvider: "workos", AllowStubWorkOS: true, SessionSecret: "x"}
	p, err := NewFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// Stub path falls through to local — provider name should reflect
	// what's actually serving requests, not the configured value.
	if p.Name() != "local" {
		t.Fatalf("want local (stub fallback), got %q", p.Name())
	}
}

func TestNewFromConfig_Unknown(t *testing.T) {
	_, err := NewFromConfig(config.Config{AuthProvider: "wat"}, nil)
	if err == nil {
		t.Fatal("want error for unknown provider")
	}
}
