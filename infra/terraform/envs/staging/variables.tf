variable "env" {
  description = "Environment name."
  type        = string
  default     = "staging"
}

variable "region" {
  description = "AWS region."
  type        = string
  default     = "us-east-1"
}

variable "vpc_cidr" {
  description = "VPC CIDR block."
  type        = string
  default     = "10.20.0.0/16"
}

variable "az_count" {
  description = "Number of AZs."
  type        = number
  default     = 2
}

variable "nat_gateway_count" {
  description = "Number of NAT gateways."
  type        = number
  default     = 1
}

variable "rds_instance_class" {
  description = "RDS instance class."
  type        = string
  default     = "db.t4g.medium"
}

variable "rds_storage_gb" {
  description = "RDS allocated storage in GB."
  type        = number
  default     = 100
}

variable "rds_multi_az" {
  description = "RDS Multi-AZ deployment."
  type        = bool
  default     = false
}

variable "redis_node_type" {
  description = "ElastiCache node type (single node for staging)."
  type        = string
  default     = "cache.t4g.small"
}

variable "redis_bootstrap_auth_token" {
  description = "Initial Redis AUTH token. Rotate post-bootstrap."
  type        = string
  sensitive   = true
}

variable "audit_object_lock_years" {
  description = "Audit export bucket Object Lock retention years."
  type        = number
  default     = 7
}

variable "root_domain" {
  description = "Root domain for the staging hosted zone (e.g. staging.nexis.dev)."
  type        = string
}

variable "github_owner" {
  description = "GitHub org/user owning the repo."
  type        = string
}

variable "github_repo" {
  description = "GitHub repo name."
  type        = string
}

variable "image_tag" {
  description = "Container image tag to deploy."
  type        = string
  default     = "bootstrap"
}

variable "tags" {
  description = "Tags merged into every resource."
  type        = map(string)
  default = {
    Environment = "staging"
    ManagedBy   = "terraform"
  }
}
