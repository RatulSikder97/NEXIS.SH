variable "services" {
  description = "List of service short names to create ECR repos for (one repo per service, prefixed nexis/<service>)."
  type        = list(string)
}

variable "kms_key_arn" {
  description = "KMS key ARN for SSE-KMS. Null -> AES256."
  type        = string
  default     = null
}

variable "force_delete" {
  description = "Allow deleting non-empty repos on destroy (true for dev/staging only)."
  type        = bool
  default     = false
}

variable "tags" {
  description = "Tags merged into all ECR resources."
  type        = map(string)
  default     = {}
}
