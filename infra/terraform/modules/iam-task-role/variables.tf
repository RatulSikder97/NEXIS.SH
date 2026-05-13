variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "service" {
  description = "Service short name (control-plane, validator, gitops, web)."
  type        = string
}

variable "policies" {
  description = "Inline policies attached to the task role. Each entry: {name, document(JSON string)}."
  type = list(object({
    name     = string
    document = string
  }))
  default = []
}

variable "secrets_arns" {
  description = "Secrets Manager ARNs the execution role may fetch at task-start."
  type        = list(string)
  default     = []
}

variable "kms_key_arns" {
  description = "KMS key ARNs the execution role may Decrypt (for SecretsManager-stored secrets)."
  type        = list(string)
  default     = []
}

variable "tags" {
  description = "Tags merged into IAM resources."
  type        = map(string)
  default     = {}
}
