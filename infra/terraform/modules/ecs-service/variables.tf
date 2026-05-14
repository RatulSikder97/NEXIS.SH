variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "service" {
  description = "Service short name (control-plane, validator, gitops, web)."
  type        = string
}

variable "region" {
  description = "AWS region (used in awslogs config)."
  type        = string
}

variable "vpc_id" {
  description = "VPC ID for the service security group."
  type        = string
}

variable "subnet_ids" {
  description = "Private subnet IDs for the Fargate ENIs."
  type        = list(string)
}

variable "cluster_arn" {
  description = "ECS cluster ARN."
  type        = string
}

variable "cluster_name" {
  description = "ECS cluster name (for autoscaling resource_id)."
  type        = string
}

variable "image" {
  description = "Container image (e.g. <acct>.dkr.ecr.<region>.amazonaws.com/nexis/control-plane:<sha>)."
  type        = string
}

variable "container_port" {
  description = "Container listen port."
  type        = number
  default     = 8080
}

variable "cpu" {
  description = "Task CPU units (1024 = 1 vCPU)."
  type        = number
  default     = 512
}

variable "memory" {
  description = "Task memory (MB)."
  type        = number
  default     = 1024
}

variable "min_count" {
  description = "Minimum service count (autoscaling lower bound)."
  type        = number
  default     = 2
}

variable "max_count" {
  description = "Maximum service count (autoscaling upper bound)."
  type        = number
  default     = 10
}

variable "task_role_arn" {
  description = "Task IAM role ARN."
  type        = string
}

variable "execution_role_arn" {
  description = "Task execution IAM role ARN (ECR pull + log writes)."
  type        = string
}

variable "alb_security_group_ids" {
  description = "ALB security group IDs allowed to reach the service."
  type        = list(string)
}

variable "log_group_name" {
  description = "CloudWatch log group name for awslogs."
  type        = string
}

variable "env_vars" {
  description = "Plain env var map injected into the container."
  type        = map(string)
  default     = {}
}

variable "secrets" {
  description = "Secrets injected into the container — map of env-var name to Secrets Manager ARN."
  type        = map(string)
  default     = {}
}

variable "health_check_command" {
  description = "Container HEALTHCHECK command (CMD-SHELL form). Distroless images should pass [\"CMD\", \"/<binary>\", \"--healthcheck\"]; images with curl can use the default."
  type        = list(string)
  default     = ["CMD-SHELL", "curl -fsS http://localhost:8080/healthz || exit 1"]
}

variable "tags" {
  description = "Tags merged into all ECS resources."
  type        = map(string)
  default     = {}
}
