package aws

import (
	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// NewS3 returns an SDK v2 S3 client bound to cfg.
func NewS3(cfg awsv2.Config) *s3.Client {
	return s3.NewFromConfig(cfg)
}
