output "proxy_arn" {
  description = "RDS Proxy ARN."
  value       = aws_db_proxy.this.arn
}

output "proxy_endpoint" {
  description = "RDS Proxy connection endpoint (host:port)."
  value       = aws_db_proxy.this.endpoint
}

output "proxy_name" {
  description = "RDS Proxy name."
  value       = aws_db_proxy.this.name
}

output "security_group_id" {
  description = "RDS Proxy security group ID."
  value       = aws_security_group.proxy.id
}
