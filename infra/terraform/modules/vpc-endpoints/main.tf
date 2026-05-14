# Nexis VPC endpoints — interface + gateway.
#
# Avoid NAT egress for high-volume control-plane traffic to AWS APIs:
# - Interface endpoints: ECR api/dkr, Secrets Manager, KMS, CloudWatch Logs
# - Gateway endpoint: S3 (free, no per-GB cost)
#
# Interface endpoints have an hourly + per-GB cost but drastically reduce
# NAT throughput — the break-even is around ~12 GB/mo per endpoint.

locals {
  name = "nexis-${var.env}"

  interface_services = toset([
    "ecr.api",
    "ecr.dkr",
    "secretsmanager",
    "kms",
    "logs",
  ])
}

data "aws_region" "current" {}

resource "aws_security_group" "endpoints" {
  name        = "${local.name}-vpce-sg"
  description = "Allow HTTPS from VPC to interface endpoints."
  vpc_id      = var.vpc_id

  ingress {
    description = "HTTPS from VPC CIDR"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.tags, { Name = "${local.name}-vpce-sg" })
}

resource "aws_vpc_endpoint" "interface" {
  for_each = local.interface_services

  vpc_id              = var.vpc_id
  service_name        = "com.amazonaws.${data.aws_region.current.name}.${each.value}"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = var.subnet_ids
  security_group_ids  = [aws_security_group.endpoints.id]
  private_dns_enabled = true

  tags = merge(var.tags, {
    Name    = "${local.name}-vpce-${replace(each.value, ".", "-")}"
    Service = each.value
  })
}

# S3 gateway endpoint — attached to the route tables of private subnets.
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = var.vpc_id
  service_name      = "com.amazonaws.${data.aws_region.current.name}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = var.private_route_table_ids

  tags = merge(var.tags, { Name = "${local.name}-vpce-s3" })
}
