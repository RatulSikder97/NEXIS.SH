# Nexis ECS Fargate cluster.
# - One cluster per environment
# - Capacity providers: FARGATE + FARGATE_SPOT with a configurable split
#   (default 80% on-demand, 20% spot — keeps p99 stable while saving on
#   off-peak workers like the validator)
# - One CloudWatch log group per service with 30-day retention, encrypted
#   with the data-plane KMS key
# - Container Insights enabled for cluster-level metrics

locals {
  name = "nexis-${var.env}"
}

resource "aws_ecs_cluster" "this" {
  name = local.name

  configuration {
    execute_command_configuration {
      logging = "DEFAULT"
    }
  }

  setting {
    name  = "containerInsights"
    value = "enabled"
  }

  tags = merge(var.tags, { Name = local.name })
}

resource "aws_ecs_cluster_capacity_providers" "this" {
  cluster_name       = aws_ecs_cluster.this.name
  capacity_providers = ["FARGATE", "FARGATE_SPOT"]

  default_capacity_provider_strategy {
    capacity_provider = "FARGATE"
    weight            = var.fargate_weight
    base              = var.fargate_base
  }

  default_capacity_provider_strategy {
    capacity_provider = "FARGATE_SPOT"
    weight            = var.fargate_spot_weight
    base              = 0
  }
}

# One log group per service. ECS task definitions reference these by name.
resource "aws_cloudwatch_log_group" "service" {
  for_each = toset(var.services)

  name              = "/nexis/${var.env}/${each.value}"
  retention_in_days = var.log_retention_days
  kms_key_id        = var.kms_key_arn

  tags = merge(var.tags, {
    Name    = "/nexis/${var.env}/${each.value}"
    Service = each.value
  })
}
