package aws

import (
	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
)

// NewKMS returns an SDK v2 KMS client bound to cfg.
func NewKMS(cfg awsv2.Config) *kms.Client {
	return kms.NewFromConfig(cfg)
}
