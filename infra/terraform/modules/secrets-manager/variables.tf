variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "secret_names" {
  description = "List of logical secret names (e.g. MASTER_KEY, JWT_SIGNING_KEY)."
  type        = list(string)
}

variable "kms_key_arn" {
  description = "KMS key ARN used to encrypt secrets at rest."
  type        = string
}

variable "service_for_secret" {
  description = "Optional map of secret_name -> service short name (used in the Service tag)."
  type        = map(string)
  default     = {}
}

variable "recovery_window_in_days" {
  description = "Secrets Manager recovery window. 7 for prod safety, 0 for dev for fast iteration."
  type        = number
  default     = 7
}

variable "tags" {
  description = "Tags merged into every secret."
  type        = map(string)
  default     = {}
}
