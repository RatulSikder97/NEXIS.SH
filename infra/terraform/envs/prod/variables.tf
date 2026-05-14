variable "env" {
  description = "Environment name."
  type        = string
  default     = "prod"
}

variable "region" {
  description = "AWS region."
  type        = string
  default     = "us-east-1"
}

variable "vpc_cidr" {
  description = "VPC CIDR block."
  type        = string
  default     = "10.30.0.0/16"
}

variable "az_count" {
  description = "Number of AZs."
  type        = number
  default     = 3
}

variable "nat_gateway_count" {
  description = "Number of NAT gateways."
  type        = number
  default     = 3
}

variable "rds_instance_class" {
  description = "RDS instance class."
  type        = string
  default     = "db.m6g.large"
}

variable "rds_storage_gb" {
  description = "RDS allocated storage in GB."
  type        = number
  default     = 500
}

variable "rds_multi_az" {
  description = "RDS Multi-AZ deployment."
  type        = bool
  default     = true
}

variable "redis_node_type" {
  description = "ElastiCache node type."
  type        = string
  default     = "cache.r7g.large"
}

variable "redis_bootstrap_auth_token" {
  description = "Initial Redis AUTH token. Rotate post-bootstrap via the AWS CLI (lifecycle ignores changes)."
  type        = string
  sensitive   = true
}

variable "audit_object_lock_years" {
  description = "Audit export bucket Object Lock retention years."
  type        = number
  default     = 7
}

variable "root_domain" {
  description = "Root domain hosted in Route 53 (e.g. nexis.dev)."
  type        = string
}

variable "github_owner" {
  description = "GitHub org/user owning the repo (for OIDC trust)."
  type        = string
}

variable "github_repo" {
  description = "GitHub repo name (for OIDC trust)."
  type        = string
}

variable "image_tag" {
  description = "Container image tag to deploy (overridden per release; default 'latest' for bootstrap only)."
  type        = string
  default     = "bootstrap"
}

variable "service_cpu" {
  description = "Per-service CPU units. Unspecified services fall back to 512."
  type        = map(number)
  default = {
    "control-plane"    = 1024
    "web"              = 512
    "validator"        = 512
    "gitops"           = 512
    "causal-inference" = 2048
  }
}

variable "service_memory" {
  description = "Per-service memory (MB). Unspecified services fall back to 1024."
  type        = map(number)
  default = {
    "control-plane"    = 2048
    "web"              = 1024
    "validator"        = 1024
    "gitops"           = 1024
    "causal-inference" = 4096
  }
}

variable "service_min_count" {
  description = "Per-service min task count."
  type        = map(number)
  default = {
    "control-plane"    = 3
    "web"              = 2
    "validator"        = 2
    "gitops"           = 1
    "causal-inference" = 1
  }
}

variable "service_max_count" {
  description = "Per-service max task count."
  type        = map(number)
  default = {
    "control-plane"    = 20
    "web"              = 10
    "validator"        = 10
    "gitops"           = 5
    "causal-inference" = 8
  }
}

variable "tags" {
  description = "Tags merged into every resource."
  type        = map(string)
  default = {
    Environment = "prod"
    ManagedBy   = "terraform"
  }
}
