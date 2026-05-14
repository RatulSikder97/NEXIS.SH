output "deploy_role_arn" {
  description = "ARN of the IAM role GitHub Actions assumes via OIDC. Configure as AWS_ROLE_TO_ASSUME in the workflow."
  value       = aws_iam_role.deploy.arn
}

output "deploy_role_name" {
  description = "Deploy role name."
  value       = aws_iam_role.deploy.name
}

output "oidc_provider_arn" {
  description = "GitHub Actions OIDC provider ARN (created here or imported)."
  value       = local.oidc_provider_arn
}
