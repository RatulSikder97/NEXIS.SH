output "repository_urls" {
  description = "Map of service short name -> repository URL (account.dkr.ecr.region.amazonaws.com/nexis/<service>)."
  value       = { for k, r in aws_ecr_repository.this : k => r.repository_url }
}

output "repository_arns" {
  description = "Map of service short name -> ECR repository ARN."
  value       = { for k, r in aws_ecr_repository.this : k => r.arn }
}

output "registry_id" {
  description = "ECR registry account ID (same for all repos in the account)."
  value       = values(aws_ecr_repository.this)[0].registry_id
}

output "registry_url" {
  description = "ECR registry URL (account.dkr.ecr.region.amazonaws.com), stripped of the repo path."
  # repository_url is <account>.dkr.ecr.<region>.amazonaws.com/<repo_name>;
  # split on the first "/" and keep the host portion.
  value = split("/", values(aws_ecr_repository.this)[0].repository_url)[0]
}
