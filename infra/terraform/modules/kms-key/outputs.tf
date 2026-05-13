output "key_id" {
  description = "KMS key UUID."
  value       = aws_kms_key.this.id
}

output "key_arn" {
  description = "KMS key ARN."
  value       = aws_kms_key.this.arn
}

output "alias_name" {
  description = "KMS key alias (alias/nexis-<env>-<purpose>)."
  value       = aws_kms_alias.this.name
}
