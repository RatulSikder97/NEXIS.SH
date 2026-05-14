# Nexis ElastiCache Redis (replication-group flavor) for session + rate-limit
# state. Encryption-at-rest with the data-plane CMK; in-transit TLS enforced;
# AUTH token stored in Secrets Manager and injected by the control-plane via
# the same secrets[] mechanism as other secrets (see secrets-manager module).
#
# For dev we run a single node; for staging/prod a multi-AZ replication group
# with automatic failover. The Postgres-style "subnet group + parameter
# group + SG" wiring mirrors the rds module.

locals {
  name = "nexis-${var.env}-redis"
}

resource "aws_elasticache_subnet_group" "this" {
  name       = local.name
  subnet_ids = var.subnet_ids
  tags       = merge(var.tags, { Name = local.name })
}

resource "aws_elasticache_parameter_group" "this" {
  name        = "${local.name}-params"
  family      = var.parameter_group_family
  description = "Nexis ${var.env} Redis parameter group."

  parameter {
    name  = "maxmemory-policy"
    value = "allkeys-lru"
  }

  tags = merge(var.tags, { Name = "${local.name}-params" })
}

resource "aws_security_group" "redis" {
  name        = "${local.name}-sg"
  description = "Redis ingress from ECS task SGs only."
  vpc_id      = var.vpc_id

  ingress {
    description     = "Redis from ECS tasks"
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = var.client_security_group_ids
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.tags, { Name = "${local.name}-sg" })
}

resource "aws_elasticache_replication_group" "this" {
  replication_group_id       = local.name
  description                = "Nexis ${var.env} Redis (sessions + rate limits)."
  node_type                  = var.node_type
  engine_version             = var.engine_version
  port                       = 6379
  parameter_group_name       = aws_elasticache_parameter_group.this.name
  subnet_group_name          = aws_elasticache_subnet_group.this.name
  security_group_ids         = [aws_security_group.redis.id]
  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  kms_key_id                 = var.kms_key_arn
  auth_token                 = var.auth_token
  num_cache_clusters         = var.num_cache_clusters
  automatic_failover_enabled = var.num_cache_clusters > 1
  multi_az_enabled           = var.num_cache_clusters > 1
  snapshot_retention_limit   = var.snapshot_retention_limit
  apply_immediately          = false

  tags = merge(var.tags, { Name = local.name })

  lifecycle {
    ignore_changes = [auth_token]
  }
}
