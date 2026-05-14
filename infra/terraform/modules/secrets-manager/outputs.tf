output "secret_arns" {
  description = "Map of logical secret name -> ARN."
  value       = { for k, s in aws_secretsmanager_secret.this : k => s.arn }
}

output "secret_names" {
  description = "Map of logical secret name -> fully-qualified Secrets Manager name."
  value       = { for k, s in aws_secretsmanager_secret.this : k => s.name }
}

output "secret_arn_list" {
  description = "Flat list of secret ARNs (convenient for iam-task-role.secrets_arns)."
  value       = [for s in aws_secretsmanager_secret.this : s.arn]
}
