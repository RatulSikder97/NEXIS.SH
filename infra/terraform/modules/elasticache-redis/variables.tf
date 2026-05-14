variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "vpc_id" {
  description = "VPC ID."
  type        = string
}

variable "subnet_ids" {
  description = "Private subnet IDs for the cache subnet group."
  type        = list(string)
}

variable "client_security_group_ids" {
  description = "ECS task security group IDs allowed to reach Redis on 6379."
  type        = list(string)
}

variable "kms_key_arn" {
  description = "KMS key ARN for at-rest encryption."
  type        = string
}

variable "auth_token" {
  description = "Redis AUTH token. Inject from Secrets Manager at apply time; rotate out-of-band (lifecycle ignores changes)."
  type        = string
  sensitive   = true
}

variable "node_type" {
  description = "ElastiCache node type (e.g. cache.t4g.small for dev, cache.r7g.large for prod)."
  type        = string
  default     = "cache.t4g.small"
}

variable "engine_version" {
  description = "Redis engine version."
  type        = string
  default     = "7.1"
}

variable "parameter_group_family" {
  description = "Parameter group family matching engine_version (e.g. redis7)."
  type        = string
  default     = "redis7"
}

variable "num_cache_clusters" {
  description = "Number of cache nodes (1 dev, 2+ staging/prod for failover)."
  type        = number
  default     = 1
}

variable "snapshot_retention_limit" {
  description = "Snapshot retention in days."
  type        = number
  default     = 1
}

variable "tags" {
  description = "Tags merged into Redis resources."
  type        = map(string)
  default     = {}
}
