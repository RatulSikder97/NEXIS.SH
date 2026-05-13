output "task_role_arn" {
  description = "Task role ARN."
  value       = aws_iam_role.task.arn
}

output "task_role_name" {
  description = "Task role name."
  value       = aws_iam_role.task.name
}

output "execution_role_arn" {
  description = "Execution role ARN."
  value       = aws_iam_role.execution.arn
}

output "execution_role_name" {
  description = "Execution role name."
  value       = aws_iam_role.execution.name
}
