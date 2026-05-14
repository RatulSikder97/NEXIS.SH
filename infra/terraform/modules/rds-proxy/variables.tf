variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "region" {
  description = "AWS region (used in the kms:ViaService condition for the secret-read policy)."
  type        = string
}

variable "vpc_id" {
  description = "VPC ID."
  type        = string
}

variable "subnet_ids" {
  description = "Private subnet IDs for the proxy (same set used by RDS)."
  type        = list(string)
}

variable "rds_security_group_id" {
  description = "RDS security group ID (proxy egresses to this)."
  type        = string
}

variable "client_security_group_ids" {
  description = "ECS task security group IDs allowed to reach the proxy."
  type        = list(string)
}

variable "db_instance_identifier" {
  description = "Identifier of the RDS instance the proxy fronts."
  type        = string
}

variable "master_user_secret_arn" {
  description = "ARN of the AWS-managed master-user secret (from rds.master_user_secret_arn)."
  type        = string
}

variable "kms_key_arn" {
  description = "KMS key ARN that encrypts the master-user secret (for kms:Decrypt scoping)."
  type        = string
}

variable "idle_client_timeout" {
  description = "Idle client connection timeout in seconds."
  type        = number
  default     = 1800
}

variable "max_connections_percent" {
  description = "Max percent of the DB's max_connections RDS Proxy may open."
  type        = number
  default     = 90
}

variable "max_idle_connections_percent" {
  description = "Max percent of connections that may sit idle in the pool."
  type        = number
  default     = 50
}

variable "tags" {
  description = "Tags merged into RDS Proxy resources."
  type        = map(string)
  default     = {}
}
