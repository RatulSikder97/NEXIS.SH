variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "region" {
  description = "AWS region (used in dashboard widget properties)."
  type        = string
}

variable "services" {
  description = "Service short names — one tile per service for CPU/Mem."
  type        = list(string)
}

variable "cluster_name" {
  description = "ECS cluster name."
  type        = string
}

variable "rds_instance_id" {
  description = "RDS instance identifier (DBInstanceIdentifier metric dimension)."
  type        = string
}

variable "redis_replication_group_id" {
  description = "ElastiCache replication group ID (null skips the Redis tile)."
  type        = string
  default     = null
}

variable "alb_arn_suffix" {
  description = "ALB ARN suffix — the LoadBalancer dimension value (e.g. app/nexis-prod-alb/abc123)."
  type        = string
}
