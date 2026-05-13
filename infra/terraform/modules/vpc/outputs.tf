output "vpc_id" {
  description = "VPC ID."
  value       = aws_vpc.this.id
}

output "vpc_cidr" {
  description = "VPC CIDR block."
  value       = aws_vpc.this.cidr_block
}

output "private_subnet_ids" {
  description = "Private subnet IDs (one per AZ)."
  value       = [for s in aws_subnet.private : s.id]
}

output "public_subnet_ids" {
  description = "Public subnet IDs (max two)."
  value       = [for s in aws_subnet.public : s.id]
}

output "nat_gateway_ids" {
  description = "NAT gateway IDs (one per public subnet)."
  value       = [for n in aws_nat_gateway.this : n.id]
}
