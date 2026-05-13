variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "purpose" {
  description = "Logical purpose of this key (e.g. data, audit, ses). Used in alias + Name tag."
  type        = string
}

variable "rotation" {
  description = "Enable AWS-managed annual key rotation."
  type        = bool
  default     = true
}

variable "multi_region" {
  description = "Multi-region key (set to true only for prod DR keys)."
  type        = bool
  default     = false
}

variable "tags" {
  description = "Tags merged into the key."
  type        = map(string)
  default     = {}
}
