variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "services" {
  description = "Service short names — one log group is created per name."
  type        = list(string)
}

variable "kms_key_arn" {
  description = "KMS key ARN used to encrypt the CloudWatch log groups (data-plane CMK)."
  type        = string
}

variable "log_retention_days" {
  description = "CloudWatch log retention. 30d default — dial up to 90+ for prod compliance if needed."
  type        = number
  default     = 30
}

variable "fargate_weight" {
  description = "Weight for the FARGATE on-demand capacity provider (default 80)."
  type        = number
  default     = 80
}

variable "fargate_base" {
  description = "Base for FARGATE on-demand. Tasks <= base always run on-demand; above base mixes by weight."
  type        = number
  default     = 1
}

variable "fargate_spot_weight" {
  description = "Weight for FARGATE_SPOT (default 20 -> 80/20 mix)."
  type        = number
  default     = 20
}

variable "tags" {
  description = "Tags merged into cluster + log groups."
  type        = map(string)
  default     = {}
}
