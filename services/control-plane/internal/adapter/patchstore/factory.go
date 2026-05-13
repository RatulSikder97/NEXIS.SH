// Package patchstore exposes a factory that picks the right PatchStore
// implementation from config. Phase 4 ships the MinIO adapter; Phase 7 adds
// the S3 adapter without changing callers. The factory itself sits at the
// adapter root so callers don't have to know which sub-package implements
// the port.
package patchstore

import (
	"context"
	"fmt"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/patchstore/minio"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/patchstore/s3"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	platformaws "github.com/nexis-eco/nexis/services/control-plane/internal/platform/aws"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// NewFromConfig returns the PatchStore matching cfg.PatchStore. Defaults to
// "minio" when unset so dev compose works out of the box. Phase 7 wires the
// "s3" branch to the real AWS SDK v2 client.
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
		awsCfg, err := platformaws.Load(context.Background(), cfg.AWSRegion)
		if err != nil {
			return nil, fmt.Errorf("patch store aws load: %w", err)
		}
		client := platformaws.NewS3(awsCfg)
		return s3.New(s3.Config{
			Client:    client,
			Presigner: awss3.NewPresignClient(client),
			Region:    cfg.AWSRegion,
			KeyVault:  kv,
		})
	default:
		return nil, fmt.Errorf("unknown PATCH_STORE %q", cfg.PatchStore)
	}
}
