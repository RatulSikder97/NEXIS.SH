variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "name" {
  description = "Bucket purpose (e.g. patches, audit-export). Goes into 'nexis-<env>-<name>'."
  type        = string
}

variable "kms_key_arn" {
  description = "KMS key ARN for SSE. Null -> AES256."
  type        = string
  default     = null
}

variable "object_lock" {
  description = "Enable Object Lock for compliance buckets."
  type        = bool
  default     = false
}

variable "object_lock_years" {
  description = "Retention years when object_lock=true."
  type        = number
  default     = 7
}

variable "tags" {
  description = "Tags merged into the bucket."
  type        = map(string)
  default     = {}
}
