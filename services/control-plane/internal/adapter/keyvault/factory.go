// Package keyvault is the host package for the KeyVault factory + the
// LocalKeyVault and KMSVault adapters. The factory routes between them based
// on cfg.KeyVault — "local" (default) or "kms".
package keyvault

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	platformaws "github.com/nexis-eco/nexis/services/control-plane/internal/platform/aws"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// NewFromConfig selects the KeyVault implementation based on cfg.KeyVault.
//
//	"local" | ""  -> LocalKeyVault sourced from cfg.MasterKey (base64, 32B).
//	"kms"         -> KMSVault sourced from cfg.KMSKeyARN over the AWS SDK v2
//	                 default credentials chain (env, shared, IRSA, ECS task).
//
// The dev fallback in main.go (using a zero master key when MasterKey is
// empty) lives there; this factory enforces the production contract: empty
// inputs are a config error.
func NewFromConfig(ctx context.Context, cfg config.Config) (domain.KeyVault, error) {
	switch cfg.KeyVault {
	case "local", "":
		if cfg.MasterKey == "" {
			return nil, fmt.Errorf("KEYVAULT=local requires MASTER_KEY (base64, 32 bytes)")
		}
		key, err := base64.StdEncoding.DecodeString(cfg.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("MASTER_KEY base64 decode: %w", err)
		}
		return NewLocal(key)
	case "kms":
		if cfg.KMSKeyARN == "" {
			return nil, fmt.Errorf("KEYVAULT=kms requires KMS_KEY_ARN")
		}
		awsCfg, err := platformaws.Load(ctx, cfg.AWSRegion)
		if err != nil {
			return nil, fmt.Errorf("kms vault aws load: %w", err)
		}
		return NewKMSVault(platformaws.NewKMS(awsCfg), cfg.KMSKeyARN), nil
	default:
		return nil, fmt.Errorf("unknown KEYVAULT %q (want local|kms)", cfg.KeyVault)
	}
}
