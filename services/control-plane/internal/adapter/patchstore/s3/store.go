// Package s3 is the Phase 7 PatchStore target. Phase 4 ships only this stub
// so the factory can switch on PATCH_STORE without compile-time changes when
// the AWS adapter lands. Every method returns ErrNotImplemented.
package s3

import (
	"context"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Config holds the deps the future S3 adapter will need. KeyVault is shared
// with the MinIO adapter so envelope wire-format stays identical across the
// migration.
type Config struct {
	KeyVault domain.KeyVault
	Region   string
	Endpoint string // optional (LocalStack)
}

// Store is the Phase 7 stub. Construct via New; every call returns ErrNotImplemented.
type Store struct{}

// New returns a Store. cfg is accepted (and ignored) so the factory's switch
// statement compiles without a per-case shim.
func New(_ Config) (*Store, error) { return &Store{}, nil }

// compile-time conformance check
var _ domain.PatchStore = (*Store)(nil)

func (s *Store) EnsureBucket(_ context.Context, _ string) error {
	return domain.ErrNotImplemented
}
func (s *Store) Put(_ context.Context, _ domain.PutOptions) error {
	return domain.ErrNotImplemented
}
func (s *Store) Get(_ context.Context, _, _ string) ([]byte, error) {
	return nil, domain.ErrNotImplemented
}
func (s *Store) SignURL(_ context.Context, _, _ string, _ time.Duration) (string, error) {
	return "", domain.ErrNotImplemented
}
