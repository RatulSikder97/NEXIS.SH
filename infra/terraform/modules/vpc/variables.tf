variable "env" {
  description = "Environment name (dev, staging, prod). Used in resource Name tags."
  type        = string
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC. Subnets carved with cidrsubnet(vpc_cidr, 4, n)."
  type        = string
}

variable "az_count" {
  description = "Number of availability zones to provision private subnets across."
  type        = number
  default     = 2
}

variable "nat_gateway_count" {
  description = "Number of NAT gateways (1 for dev cost; az_count for prod HA)."
  type        = number
  default     = 1
}

variable "tags" {
  description = "Tags merged into every resource."
  type        = map(string)
  default     = {}
}
