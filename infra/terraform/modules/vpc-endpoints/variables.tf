variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "vpc_id" {
  description = "VPC ID."
  type        = string
}

variable "vpc_cidr" {
  description = "VPC CIDR for the endpoint security group ingress rule."
  type        = string
}

variable "subnet_ids" {
  description = "Private subnet IDs for interface endpoint ENIs."
  type        = list(string)
}

variable "private_route_table_ids" {
  description = "Private route table IDs for the S3 gateway endpoint."
  type        = list(string)
}

variable "tags" {
  description = "Tags merged into endpoint resources."
  type        = map(string)
  default     = {}
}
