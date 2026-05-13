// Package patchstore exposes a factory that picks the right PatchStore
// implementation from config. Phase 4 ships the MinIO adapter; Phase 7 adds
// the S3 adapter without changing callers. The factory itself sits at the
// adapter root so callers don't have to know which sub-package implements
// the port.
package patchstore

import (
	"fmt"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/patchstore/minio"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/patchstore/s3"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// NewFromConfig returns the PatchStore matching cfg.PatchStore. Defaults to
// "minio" when unset so dev compose works out of the box.
func NewFromConfig(cfg config.Config, kv domain.KeyVault) (domain.PatchStore, error) {
	switch cfg.PatchStore {
	case "minio", "":
		return minio.New(minio.Config{
			Endpoint:  cfg.MinIOEndpoint,
			AccessKey: cfg.MinIOAccessKey,
			SecretKey: cfg.MinIOSecretKey,
			UseSSL:    cfg.MinIOUseSSL,
			KeyVault:  kv,
		})
	case "s3":
		return s3.New(s3.Config{KeyVault: kv})
	default:
		return nil, fmt.Errorf("unknown PATCH_STORE %q", cfg.PatchStore)
	}
}
