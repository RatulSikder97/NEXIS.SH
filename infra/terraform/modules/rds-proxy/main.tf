# Nexis RDS Proxy module.
#
# Front the RDS Postgres with RDS Proxy for connection multiplexing so the
# control-plane can scale to high task counts without exhausting Postgres's
# max_connections. The proxy authenticates to RDS using the AWS-managed
# master-user secret created by the rds module, then exposes a single TLS
# endpoint that the application connects to.
#
# - IAM auth disabled (we use the master credentials secret already)
# - require_tls = true
# - idle_client_timeout = 1800s
# - target group binds to a single aws_db_instance

locals {
  name = "nexis-${var.env}-rdsproxy"
}

data "aws_iam_policy_document" "proxy_assume" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["rds.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "proxy" {
  name               = "${local.name}-role"
  assume_role_policy = data.aws_iam_policy_document.proxy_assume.json
  tags               = merge(var.tags, { Name = "${local.name}-role" })
}

data "aws_iam_policy_document" "proxy_secret_read" {
  statement {
    effect    = "Allow"
    actions   = ["secretsmanager:GetSecretValue", "secretsmanager:DescribeSecret"]
    resources = [var.master_user_secret_arn]
  }

  statement {
    effect    = "Allow"
    actions   = ["kms:Decrypt"]
    resources = [var.kms_key_arn]
    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["secretsmanager.${var.region}.amazonaws.com"]
    }
  }
}

resource "aws_iam_role_policy" "proxy_secret_read" {
  name   = "secret-read"
  role   = aws_iam_role.proxy.id
  policy = data.aws_iam_policy_document.proxy_secret_read.json
}

# Proxy lives in the same private subnets as RDS; ECS tasks reach it via
# its own security group (ingress from var.client_security_group_ids).
resource "aws_security_group" "proxy" {
  name        = "${local.name}-sg"
  description = "RDS Proxy ingress from ECS task SGs only."
  vpc_id      = var.vpc_id

  ingress {
    description     = "Postgres from ECS tasks"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = var.client_security_group_ids
  }

  egress {
    description     = "To RDS"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [var.rds_security_group_id]
  }

  tags = merge(var.tags, { Name = "${local.name}-sg" })
}

resource "aws_db_proxy" "this" {
  name                   = local.name
  engine_family          = "POSTGRESQL"
  role_arn               = aws_iam_role.proxy.arn
  vpc_subnet_ids         = var.subnet_ids
  vpc_security_group_ids = [aws_security_group.proxy.id]
  require_tls            = true
  idle_client_timeout    = var.idle_client_timeout
  debug_logging          = false

  auth {
    auth_scheme = "SECRETS"
    iam_auth    = "DISABLED"
    secret_arn  = var.master_user_secret_arn
    description = "Master user secret from rds module."
  }

  tags = merge(var.tags, { Name = local.name })
}

resource "aws_db_proxy_default_target_group" "this" {
  db_proxy_name = aws_db_proxy.this.name

  connection_pool_config {
    connection_borrow_timeout    = 120
    max_connections_percent      = var.max_connections_percent
    max_idle_connections_percent = var.max_idle_connections_percent
  }
}

resource "aws_db_proxy_target" "this" {
  db_proxy_name          = aws_db_proxy.this.name
  target_group_name      = aws_db_proxy_default_target_group.this.name
  db_instance_identifier = var.db_instance_identifier
}
