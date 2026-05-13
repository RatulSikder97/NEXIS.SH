package aws

import (
	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// NewSecretsManager returns an SDK v2 Secrets Manager client bound to cfg.
func NewSecretsManager(cfg awsv2.Config) *secretsmanager.Client {
	return secretsmanager.NewFromConfig(cfg)
}
