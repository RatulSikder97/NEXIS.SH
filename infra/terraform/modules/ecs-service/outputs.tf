output "service_name" {
  description = "ECS service name."
  value       = aws_ecs_service.this.name
}

output "task_definition_arn" {
  description = "ECS task definition ARN."
  value       = aws_ecs_task_definition.this.arn
}

output "target_group_arn" {
  description = "ALB target group ARN (attach to listener rules)."
  value       = aws_lb_target_group.this.arn
}

output "security_group_id" {
  description = "Service security group ID (used by RDS + Redis ingress)."
  value       = aws_security_group.service.id
}
