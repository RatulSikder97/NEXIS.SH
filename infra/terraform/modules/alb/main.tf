# Nexis Application Load Balancer + ACM + Route 53.
#
# Provisions:
# - One ALB in public subnets, internet-facing
# - An ACM cert covering app/api/status.<root> (DNS-validated against the
#   pre-existing Route 53 zone)
# - Two listeners: :80 -> permanent redirect to :443, :443 -> default 404
# - Route 53 A-alias records for each hostname pointing at the ALB
#
# Listener rules per service (host-header match) are wired by the env root
# via aws_lb_listener_rule resources that reference the target groups
# exported by ecs-service.

locals {
  name = "nexis-${var.env}-alb"

  hostnames = {
    app    = "app.${var.root_domain}"
    api    = "api.${var.root_domain}"
    status = "status.${var.root_domain}"
  }
}

data "aws_route53_zone" "root" {
  name         = "${var.root_domain}."
  private_zone = false
}

resource "aws_security_group" "alb" {
  name        = "${local.name}-sg"
  description = "ALB ingress — public 80/443."
  vpc_id      = var.vpc_id

  ingress {
    description = "HTTP from internet (redirected to HTTPS)"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "HTTPS from internet"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.tags, { Name = "${local.name}-sg" })
}

resource "aws_lb" "this" {
  name                       = local.name
  load_balancer_type         = "application"
  internal                   = false
  security_groups            = [aws_security_group.alb.id]
  subnets                    = var.public_subnet_ids
  drop_invalid_header_fields = true
  enable_deletion_protection = var.env == "prod"

  # Only attach access_logs when a bucket is supplied; the AWS provider
  # treats the block's `bucket` attribute as required if the block is set.
  dynamic "access_logs" {
    for_each = var.access_logs_bucket == null ? [] : [1]
    content {
      bucket  = var.access_logs_bucket
      prefix  = "alb/${var.env}"
      enabled = true
    }
  }

  tags = merge(var.tags, { Name = local.name })
}

# DNS-validated ACM cert covering all three hostnames.
resource "aws_acm_certificate" "this" {
  domain_name               = local.hostnames.app
  subject_alternative_names = [local.hostnames.api, local.hostnames.status]
  validation_method         = "DNS"

  lifecycle {
    create_before_destroy = true
  }

  tags = merge(var.tags, { Name = "${local.name}-cert" })
}

# One validation record per FQDN. domain_validation_options is a set keyed
# by domain_name, so we project it into a map for for_each.
resource "aws_route53_record" "cert_validation" {
  for_each = {
    for dvo in aws_acm_certificate.this.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      record = dvo.resource_record_value
      type   = dvo.resource_record_type
    }
  }

  allow_overwrite = true
  zone_id         = data.aws_route53_zone.root.zone_id
  name            = each.value.name
  records         = [each.value.record]
  type            = each.value.type
  ttl             = 60
}

resource "aws_acm_certificate_validation" "this" {
  certificate_arn         = aws_acm_certificate.this.arn
  validation_record_fqdns = [for r in aws_route53_record.cert_validation : r.fqdn]
}

# :80 HTTP listener — permanent redirect to HTTPS.
resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"
    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }

  tags = merge(var.tags, { Name = "${local.name}-http" })
}

# :443 HTTPS listener — defaults to 404 so unmatched host headers don't reach
# any service. Listener rules registered by env root supply the real routing.
resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.this.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = aws_acm_certificate_validation.this.certificate_arn

  default_action {
    type = "fixed-response"
    fixed_response {
      content_type = "text/plain"
      message_body = "Not Found"
      status_code  = "404"
    }
  }

  tags = merge(var.tags, { Name = "${local.name}-https" })
}

# A-record aliases for each hostname.
resource "aws_route53_record" "alias" {
  for_each = local.hostnames

  zone_id = data.aws_route53_zone.root.zone_id
  name    = each.value
  type    = "A"

  alias {
    name                   = aws_lb.this.dns_name
    zone_id                = aws_lb.this.zone_id
    evaluate_target_health = true
  }
}
