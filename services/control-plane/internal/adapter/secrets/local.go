// Package secrets implements domain.SecretsStore. Two backends today:
//
//   - env             (dev / compose) — reads os.Getenv. Names are
//     uppercased + dotted-to-underscored (stripe.api_key -> STRIPE_API_KEY).
//
//   - secretsmanager  (staging / prod) — reads AWS Secrets Manager via the
//     SDK v2 client. Names are joined to cfg.SecretsPrefix.
//
// The factory picks based on cfg.SecretsBackend.
package secrets

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// EnvStore reads secrets from process env vars. Name "stripe.api_key" is
// looked up as "STRIPE_API_KEY". Empty values surface as "secret not set".
//
// Writes (Put) are accepted in-process for the lifetime of the run but
// never reach disk — they're intended for test fixtures only.
type EnvStore struct {
	mem map[string][]byte
}

// NewEnv constructs an EnvStore. The mem map is empty until Put is called.
func NewEnv() *EnvStore {
	return &EnvStore{mem: map[string][]byte{}}
}

// compile-time conformance check
var _ domain.SecretsStore = (*EnvStore)(nil)

// Get returns the env var matching name. Lookup order:
//
//  1. in-memory map (last Put wins),
//  2. os.LookupEnv with name normalised to SCREAMING_SNAKE.
//
// Returns an error when neither yields a value.
func (s *EnvStore) Get(_ context.Context, name string) ([]byte, error) {
	if v, ok := s.mem[name]; ok {
		return v, nil
	}
	envName := normalize(name)
	if v, ok := os.LookupEnv(envName); ok && v != "" {
		return []byte(v), nil
	}
	return nil, fmt.Errorf("secrets/env: %q not set (looked up %q)", name, envName)
}

// Put stores value under name in the in-memory map. Survives only for the
// process lifetime; intentionally not persisted to disk.
func (s *EnvStore) Put(_ context.Context, name string, value []byte) error {
	s.mem[name] = value
	return nil
}

// normalize converts dotted/lowercase secret names into SCREAMING_SNAKE for
// env var lookup. "stripe.api_key" -> "STRIPE_API_KEY".
func normalize(name string) string {
	out := strings.NewReplacer(".", "_", "-", "_", "/", "_").Replace(name)
	return strings.ToUpper(out)
}
