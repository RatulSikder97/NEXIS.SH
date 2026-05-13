package aws

import (
	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
)

// NewSESv2 returns an SDK v2 SES v2 client bound to cfg.
func NewSESv2(cfg awsv2.Config) *sesv2.Client {
	return sesv2.NewFromConfig(cfg)
}
