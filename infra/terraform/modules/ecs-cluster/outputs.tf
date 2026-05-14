output "cluster_arn" {
  description = "ECS cluster ARN."
  value       = aws_ecs_cluster.this.arn
}

output "cluster_name" {
  description = "ECS cluster name (used in autoscaling resource_id)."
  value       = aws_ecs_cluster.this.name
}

output "log_group_names" {
  description = "Map of service short name -> CloudWatch log group name."
  value       = { for k, lg in aws_cloudwatch_log_group.service : k => lg.name }
}

output "log_group_arns" {
  description = "Map of service short name -> CloudWatch log group ARN."
  value       = { for k, lg in aws_cloudwatch_log_group.service : k => lg.arn }
}
