# Nexis CloudWatch dashboard.
#
# One dashboard per env summarising:
# - ALB request volume + 5xx rate + p99 target response time
# - ECS service CPU + Memory utilisation (per service)
# - RDS CPU, FreeStorageSpace, DatabaseConnections
# - Redis CPU + CurrConnections
#
# Tiles are sized to a 24-column grid; expand by editing the dashboard_body
# locals if you want per-service drill-downs.

locals {
  name = "nexis-${var.env}"

  # Each service contributes one mini-tile with CPU + Mem on the same chart.
  service_widgets = [
    for i, svc in var.services : {
      type   = "metric"
      x      = (i % 3) * 8
      y      = 12 + floor(i / 3) * 6
      width  = 8
      height = 6
      properties = {
        title   = "ECS ${svc}"
        region  = var.region
        view    = "timeSeries"
        stacked = false
        metrics = [
          ["AWS/ECS", "CPUUtilization", "ServiceName", "nexis-${var.env}-${svc}", "ClusterName", var.cluster_name],
          [".", "MemoryUtilization", ".", ".", ".", "."],
        ]
      }
    }
  ]

  rds_widget = {
    type   = "metric"
    x      = 0
    y      = 6
    width  = 12
    height = 6
    properties = {
      title   = "RDS Postgres"
      region  = var.region
      view    = "timeSeries"
      stacked = false
      metrics = [
        ["AWS/RDS", "CPUUtilization", "DBInstanceIdentifier", var.rds_instance_id],
        [".", "DatabaseConnections", ".", "."],
        [".", "FreeStorageSpace", ".", "."],
      ]
    }
  }

  redis_widget = var.redis_replication_group_id == null ? null : {
    type   = "metric"
    x      = 12
    y      = 6
    width  = 12
    height = 6
    properties = {
      title   = "ElastiCache Redis"
      region  = var.region
      view    = "timeSeries"
      stacked = false
      metrics = [
        ["AWS/ElastiCache", "EngineCPUUtilization", "ReplicationGroupId", var.redis_replication_group_id],
        [".", "CurrConnections", ".", "."],
      ]
    }
  }

  alb_widget = {
    type   = "metric"
    x      = 0
    y      = 0
    width  = 24
    height = 6
    properties = {
      title   = "ALB Traffic + Errors"
      region  = var.region
      view    = "timeSeries"
      stacked = false
      metrics = [
        ["AWS/ApplicationELB", "RequestCount", "LoadBalancer", var.alb_arn_suffix, { stat = "Sum" }],
        [".", "HTTPCode_Target_5XX_Count", ".", ".", { stat = "Sum" }],
        [".", "TargetResponseTime", ".", ".", { stat = "p99" }],
      ]
    }
  }

  widgets = concat(
    [local.alb_widget, local.rds_widget],
    local.redis_widget == null ? [] : [local.redis_widget],
    local.service_widgets,
  )
}

resource "aws_cloudwatch_dashboard" "this" {
  dashboard_name = local.name
  dashboard_body = jsonencode({ widgets = local.widgets })
}
