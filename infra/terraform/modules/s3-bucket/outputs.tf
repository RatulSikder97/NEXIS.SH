output "bucket_name" {
  description = "Bucket name."
  value       = aws_s3_bucket.this.id
}

output "bucket_arn" {
  description = "Bucket ARN."
  value       = aws_s3_bucket.this.arn
}

output "bucket_regional_domain_name" {
  description = "Regional bucket domain (used by ALB + CloudFront)."
  value       = aws_s3_bucket.this.bucket_regional_domain_name
}
