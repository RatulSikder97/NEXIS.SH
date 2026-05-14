# Nexis RDS module — Postgres 16 with pgvector + pg_stat_statements.
# Multi-AZ in staging/prod; storage encrypted with the data-plane KMS key.
# Master password managed by AWS Secrets Manager.

resource "aws_db_subnet_group" "this" {
  name       = "nexis-${var.env}-rds"
  subnet_ids = var.subnet_ids
  tags       = merge(var.tags, { Name = "nexis-${var.env}-rds" })
}

resource "aws_db_parameter_group" "this" {
  name   = "nexis-${var.env}-pg16-pgvector"
  family = "postgres16"

  parameter {
    name         = "shared_preload_libraries"
    value        = "pg_stat_statements"
    apply_method = "pending-reboot"
  }

  parameter {
    name  = "log_min_duration_statement"
    value = "500"
  }

  tags = merge(var.tags, { Name = "nexis-${var.env}-pg16-pgvector" })
}

resource "aws_security_group" "rds" {
  name        = "nexis-${var.env}-rds-sg"
  description = "RDS Postgres access from ECS task SGs only."
  vpc_id      = var.vpc_id

  ingress {
    description     = "Postgres from ECS tasks"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = var.ecs_security_group_ids
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.tags, { Name = "nexis-${var.env}-rds-sg" })
}

resource "aws_db_instance" "this" {
  identifier = "nexis-${var.env}-rds"

  engine         = "postgres"
  engine_version = var.engine_version
  instance_class = var.instance_class

  allocated_storage     = var.storage_gb
  max_allocated_storage = var.storage_gb * 4
  storage_type          = "gp3"
  storage_encrypted     = true
  kms_key_id            = var.kms_key_arn

  db_name                       = "nexis"
  username                      = "nexis_admin"
  manage_master_user_password   = true
  master_user_secret_kms_key_id = var.kms_key_arn

  multi_az            = var.multi_az
  publicly_accessible = false

  vpc_security_group_ids = [aws_security_group.rds.id]
  db_subnet_group_name   = aws_db_subnet_group.this.name
  parameter_group_name   = aws_db_parameter_group.this.name

  backup_retention_period         = var.backup_retention_days
  backup_window                   = "06:00-06:30"
  copy_tags_to_snapshot           = true
  deletion_protection             = var.env == "prod"
  skip_final_snapshot             = var.env == "dev"
  final_snapshot_identifier       = var.env == "dev" ? null : "nexis-${var.env}-rds-final-${formatdate("YYYYMMDD-hhmmss", timestamp())}"
  performance_insights_enabled    = true
  performance_insights_kms_key_id = var.kms_key_arn

  enabled_cloudwatch_logs_exports = ["postgresql"]

  tags = merge(var.tags, { Name = "nexis-${var.env}-rds" })

  lifecycle {
    ignore_changes = [final_snapshot_identifier]
  }
}
