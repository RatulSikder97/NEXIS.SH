output "interface_endpoint_ids" {
  description = "Map of AWS service name -> VPC endpoint ID."
  value       = { for k, e in aws_vpc_endpoint.interface : k => e.id }
}

output "s3_gateway_endpoint_id" {
  description = "S3 gateway endpoint ID."
  value       = aws_vpc_endpoint.s3.id
}

output "endpoint_security_group_id" {
  description = "Security group ID used by interface endpoints."
  value       = aws_security_group.endpoints.id
}
