variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "vpc_id" {
  description = "VPC ID to place the security group in."
  type        = string
}

variable "subnet_ids" {
  description = "Private subnet IDs for the RDS subnet group."
  type        = list(string)
}

variable "ecs_security_group_ids" {
  description = "ECS task security group IDs allowed to reach Postgres."
  type        = list(string)
  default     = []
}

variable "kms_key_arn" {
  description = "KMS key ARN for at-rest encryption + master-secret encryption."
  type        = string
}

variable "instance_class" {
  description = "RDS instance class (e.g. db.t4g.medium, db.m6g.large)."
  type        = string
  default     = "db.t4g.medium"
}

variable "engine_version" {
  description = "Postgres engine version."
  type        = string
  default     = "16.4"
}

variable "storage_gb" {
  description = "Allocated storage (GB). max_allocated_storage auto-scales to 4x."
  type        = number
  default     = 50
}

variable "multi_az" {
  description = "Multi-AZ deployment (true for staging+prod)."
  type        = bool
  default     = false
}

variable "backup_retention_days" {
  description = "Backup retention window in days."
  type        = number
  default     = 7
}

variable "tags" {
  description = "Tags merged into all RDS resources."
  type        = map(string)
  default     = {}
}
