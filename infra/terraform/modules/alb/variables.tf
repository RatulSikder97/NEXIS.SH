variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "vpc_id" {
  description = "VPC ID."
  type        = string
}

variable "public_subnet_ids" {
  description = "Public subnet IDs the ALB is attached to (>= 2 AZs)."
  type        = list(string)
}

variable "root_domain" {
  description = "Root domain hosted in Route 53 (e.g. nexis.dev). app/api/status subdomains are derived."
  type        = string
}

variable "access_logs_bucket" {
  description = "S3 bucket name for ALB access logs. Null disables access logging."
  type        = string
  default     = null
}

variable "tags" {
  description = "Tags merged into ALB-related resources."
  type        = map(string)
  default     = {}
}
