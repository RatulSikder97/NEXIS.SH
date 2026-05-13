output "vpc_id" {
  description = "VPC ID."
  value       = module.vpc.vpc_id
}

output "private_subnet_ids" {
  description = "Private subnet IDs."
  value       = module.vpc.private_subnet_ids
}

output "kms_data_key_arn" {
  description = "Data-plane KMS key ARN."
  value       = module.kms_data.key_arn
}

output "kms_audit_key_arn" {
  description = "Audit-plane KMS key ARN."
  value       = module.kms_audit.key_arn
}

output "s3_patches_bucket" {
  description = "Patches bucket name."
  value       = module.s3_patches.bucket_name
}

output "s3_audit_export_bucket" {
  description = "Audit export bucket name."
  value       = module.s3_audit_export.bucket_name
}

output "rds_endpoint" {
  description = "RDS endpoint."
  value       = module.rds.endpoint
}
