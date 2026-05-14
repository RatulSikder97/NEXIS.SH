output "alb_arn" {
  description = "ALB ARN."
  value       = aws_lb.this.arn
}

output "alb_arn_suffix" {
  description = "ALB ARN suffix (the LoadBalancer dimension value used in CloudWatch metrics)."
  value       = aws_lb.this.arn_suffix
}

output "alb_dns_name" {
  description = "ALB DNS name (alias target)."
  value       = aws_lb.this.dns_name
}

output "alb_zone_id" {
  description = "ALB hosted zone ID (for Route 53 alias records elsewhere)."
  value       = aws_lb.this.zone_id
}

output "alb_security_group_id" {
  description = "ALB security group ID (passed to ecs-service as alb_security_group_ids)."
  value       = aws_security_group.alb.id
}

output "https_listener_arn" {
  description = "HTTPS listener ARN — used by env root to attach per-service listener rules."
  value       = aws_lb_listener.https.arn
}

output "certificate_arn" {
  description = "Validated ACM certificate ARN."
  value       = aws_acm_certificate_validation.this.certificate_arn
}

output "hostnames" {
  description = "Map of logical name -> fully-qualified hostname (app/api/status)."
  value = {
    app    = "app.${var.root_domain}"
    api    = "api.${var.root_domain}"
    status = "status.${var.root_domain}"
  }
}

output "route53_zone_id" {
  description = "Route 53 hosted zone ID for the root domain."
  value       = data.aws_route53_zone.root.zone_id
}
