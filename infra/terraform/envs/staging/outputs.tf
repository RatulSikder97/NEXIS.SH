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

output "redis_primary_endpoint" {
  description = "Redis primary endpoint."
  value       = module.redis.primary_endpoint
}

output "alb_dns_name" {
  description = "ALB public DNS name."
  value       = module.alb.alb_dns_name
}

output "api_endpoint" {
  description = "Public API endpoint."
  value       = "https://${module.alb.hostnames["api"]}"
}

output "app_endpoint" {
  description = "Public web app endpoint."
  value       = "https://${module.alb.hostnames["app"]}"
}

output "ecr_registry_url" {
  description = "ECR registry URL."
  value       = module.ecr.registry_url
}

output "ecr_repository_urls" {
  description = "Map of service short name -> ECR repository URL."
  value       = module.ecr.repository_urls
}

output "ecs_cluster_arn" {
  description = "ECS cluster ARN."
  value       = module.ecs_cluster.cluster_arn
}

output "ecs_cluster_name" {
  description = "ECS cluster name."
  value       = module.ecs_cluster.cluster_name
}

output "secrets_arn_map" {
  description = "Map of secret_name -> ARN."
  value       = module.secrets.secret_arns
}

output "gha_deploy_role_arn" {
  description = "ARN of the IAM role GitHub Actions assumes for deploys."
  value       = module.gha_oidc.deploy_role_arn
}
