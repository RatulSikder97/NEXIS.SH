package secrets

import (
	"context"
	"fmt"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	platformaws "github.com/nexis-eco/nexis/services/control-plane/internal/platform/aws"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// NewFromConfig returns the SecretsStore matching cfg.SecretsBackend.
//
//	"env" | "" -> EnvStore (dev / compose)
//	"secretsmanager" -> SecretsManagerStore (staging / prod)
//
// FatalIfLocalInCloud refuses to boot if SecretsBackend stays "env" in
// staging/prod, so the env branch will never run in cloud envs.
func NewFromConfig(ctx context.Context, cfg config.Config) (domain.SecretsStore, error) {
	switch cfg.SecretsBackend {
	case "env", "":
		return NewEnv(), nil
	case "secretsmanager":
		awsCfg, err := platformaws.Load(ctx, cfg.AWSRegion)
		if err != nil {
			return nil, fmt.Errorf("secrets aws load: %w", err)
		}
		return NewSecretsManager(platformaws.NewSecretsManager(awsCfg), cfg.SecretsPrefix), nil
	default:
		return nil, fmt.Errorf("unknown SECRETS %q (want env|secretsmanager)", cfg.SecretsBackend)
	}
}
