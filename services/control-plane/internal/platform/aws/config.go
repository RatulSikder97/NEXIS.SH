// Package aws wraps the AWS SDK v2 config loader and per-service client
// constructors used by Phase 7 adapters. Keeping the wrappers here means the
// adapter packages don't pull in `config.LoadDefaultConfig` themselves and
// the SDK options surface (region, retry, credentials chain) lives in one
// place.
package aws

import (
	"context"
	"fmt"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

// Load returns an SDK v2 config for the requested region using the default
// credentials chain (env, shared, IRSA, ECS task role). Callers pass the
// returned config to each per-service constructor below.
func Load(ctx context.Context, region string) (awsv2.Config, error) {
	if region == "" {
		region = "us-east-1"
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return awsv2.Config{}, fmt.Errorf("aws: load default config: %w", err)
	}
	return cfg, nil
}
