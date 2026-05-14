output "primary_endpoint" {
  description = "Primary endpoint (host) — connect as rediss://:<auth>@<host>:6379."
  value       = aws_elasticache_replication_group.this.primary_endpoint_address
}

output "reader_endpoint" {
  description = "Reader endpoint (load-balanced across replicas)."
  value       = aws_elasticache_replication_group.this.reader_endpoint_address
}

output "port" {
  description = "Redis port."
  value       = aws_elasticache_replication_group.this.port
}

output "security_group_id" {
  description = "Redis security group ID."
  value       = aws_security_group.redis.id
}
