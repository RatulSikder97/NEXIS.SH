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

variable "audit_object_lock_years" {
  description = "Audit export bucket Object Lock retention years."
  type        = number
  default     = 7
}

variable "tags" {
  description = "Tags merged into every resource."
  type        = map(string)
  default = {
    Environment = "prod"
    ManagedBy   = "terraform"
  }
}
