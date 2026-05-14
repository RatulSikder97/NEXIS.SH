# Nexis prod env root.
# Multi-region-ready data-plane KMS, 7-year audit Object Lock, larger RDS,
# 3 NAT gateways, RDS Proxy, ECS Fargate cluster + 5 services behind ALB.
# Manual-gated terraform apply via GHA per spec §12.
#
# Boot order (because of cross-module deps):
#   vpc -> kms -> s3 -> secrets -> rds -> rds-proxy -> redis -> ecr ->
#   ecs-cluster -> alb -> iam-task-roles -> ecs-services -> listener rules ->
#   vpc-endpoints -> gha-oidc -> dashboards
#
# Terraform handles ordering by reference; this listing is just orientation.

provider "aws" {
  region = var.region

  default_tags {
    tags = var.tags
  }
}

data "aws_caller_identity" "current" {}

locals {
  services = ["control-plane", "web", "validator", "gitops", "causal-inference"]

  # Per-service ALB host -> upstream mapping. Anything that needs an
  # external hostname appears here; internal-only services (validator,
  # gitops worker, causal-inference batch) get listener rules by path
  # under api.nexis.dev or no ALB attachment at all.
  alb_routes = {
    web             = { host = "app.${var.root_domain}", path = "/*" }
    "control-plane" = { host = "api.${var.root_domain}", path = "/*" }
    # status.nexis.dev is currently served by web; switch the rule below
    # when we land a dedicated status service.
  }

  # Per-service env vars wired from the platform.
  service_env = {
    "control-plane" = {
      AWS_REGION             = var.region
      DB_PROXY_ENDPOINT      = module.rds_proxy.proxy_endpoint
      DB_NAME                = module.rds.db_name
      REDIS_PRIMARY_ENDPOINT = module.redis.primary_endpoint
      S3_PATCHES_BUCKET      = module.s3_patches.bucket_name
      S3_AUDIT_EXPORT_BUCKET = module.s3_audit_export.bucket_name
      SECRETS_PROVIDER       = "aws"
      STORAGE_PROVIDER       = "s3"
    }
    web = {
      API_BASE_URL = "https://api.${var.root_domain}"
    }
    validator          = { AWS_REGION = var.region }
    gitops             = { AWS_REGION = var.region }
    "causal-inference" = { AWS_REGION = var.region }
  }
}

module "vpc" {
  source = "../../modules/vpc"

  env               = var.env
  vpc_cidr          = var.vpc_cidr
  az_count          = var.az_count
  nat_gateway_count = var.nat_gateway_count
  tags              = var.tags
}

module "kms_data" {
  source = "../../modules/kms-key"

  env          = var.env
  purpose      = "data"
  multi_region = true
  tags         = var.tags
}

module "kms_audit" {
  source = "../../modules/kms-key"

  env     = var.env
  purpose = "audit"
  tags    = var.tags
}

module "s3_patches" {
  source = "../../modules/s3-bucket"

  env         = var.env
  name        = "patches"
  kms_key_arn = module.kms_data.key_arn
  tags        = var.tags
}

module "s3_audit_export" {
  source = "../../modules/s3-bucket"

  env               = var.env
  name              = "audit-export"
  kms_key_arn       = module.kms_audit.key_arn
  object_lock       = true
  object_lock_years = var.audit_object_lock_years
  tags              = var.tags
}

# S3 bucket for ALB access logs. AWS requires AES256 SSE (KMS not supported
# for ALB logs); we leave kms_key_arn null for that reason.
module "s3_alb_logs" {
  source = "../../modules/s3-bucket"

  env  = var.env
  name = "alb-logs"
  tags = var.tags
}

module "secrets" {
  source = "../../modules/secrets-manager"

  env         = var.env
  kms_key_arn = module.kms_data.key_arn
  secret_names = [
    "MASTER_KEY",
    "AUDIT_SECRET",
    "SESSION_SECRET",
    "JWT_SIGNING_KEY",
    "STRIPE_SECRET_KEY",
    "GITHUB_APP_PRIVATE_KEY_PEM",
    "OPENAI_API_KEY",
    "REDIS_AUTH_TOKEN",
  ]

  # Tag each secret with the primary owning service.
  service_for_secret = {
    MASTER_KEY                 = "control-plane"
    AUDIT_SECRET               = "control-plane"
    SESSION_SECRET             = "control-plane"
    JWT_SIGNING_KEY            = "control-plane"
    STRIPE_SECRET_KEY          = "control-plane"
    GITHUB_APP_PRIVATE_KEY_PEM = "gitops"
    OPENAI_API_KEY             = "causal-inference"
    REDIS_AUTH_TOKEN           = "shared"
  }

  recovery_window_in_days = 7
  tags                    = var.tags
}

module "rds" {
  source = "../../modules/rds"

  env        = var.env
  vpc_id     = module.vpc.vpc_id
  subnet_ids = module.vpc.private_subnet_ids
  # Service SGs are wired separately via aws_security_group_rule below to
  # avoid a cycle (services depend on rds_proxy.endpoint via env_vars).
  ecs_security_group_ids = []
  kms_key_arn            = module.kms_data.key_arn
  instance_class         = var.rds_instance_class
  storage_gb             = var.rds_storage_gb
  multi_az               = var.rds_multi_az
  backup_retention_days  = 35
  tags                   = var.tags
}

module "rds_proxy" {
  source = "../../modules/rds-proxy"

  env                   = var.env
  region                = var.region
  vpc_id                = module.vpc.vpc_id
  subnet_ids            = module.vpc.private_subnet_ids
  rds_security_group_id = module.rds.security_group_id
  # Likewise, the proxy SG ingress from service SGs is added by a separate
  # resource (rds_proxy_ingress_from_services) to keep the dep graph acyclic.
  client_security_group_ids = []
  db_instance_identifier    = module.rds.instance_id
  master_user_secret_arn    = module.rds.master_user_secret_arn
  kms_key_arn               = module.kms_data.key_arn
  tags                      = var.tags
}

module "redis" {
  source = "../../modules/elasticache-redis"

  env        = var.env
  vpc_id     = module.vpc.vpc_id
  subnet_ids = module.vpc.private_subnet_ids
  # Same cycle-avoidance pattern as rds_proxy.
  client_security_group_ids = []
  kms_key_arn               = module.kms_data.key_arn
  # Bootstrap-only placeholder. Lifecycle ignores changes; rotate via
  # `aws elasticache modify-replication-group --auth-token-update-strategy ROTATE`.
  auth_token               = var.redis_bootstrap_auth_token
  node_type                = var.redis_node_type
  num_cache_clusters       = 2
  snapshot_retention_limit = 7
  tags                     = var.tags
}

module "ecr" {
  source = "../../modules/ecr"

  services     = local.services
  kms_key_arn  = module.kms_data.key_arn
  force_delete = false
  tags         = var.tags
}

module "ecs_cluster" {
  source = "../../modules/ecs-cluster"

  env                 = var.env
  services            = local.services
  kms_key_arn         = module.kms_data.key_arn
  log_retention_days  = 30
  fargate_weight      = 80
  fargate_base        = 2
  fargate_spot_weight = 20
  tags                = var.tags
}

module "alb" {
  source = "../../modules/alb"

  env                = var.env
  vpc_id             = module.vpc.vpc_id
  public_subnet_ids  = module.vpc.public_subnet_ids
  root_domain        = var.root_domain
  access_logs_bucket = module.s3_alb_logs.bucket_name
  tags               = var.tags

  depends_on = [aws_s3_bucket_policy.alb_logs]
}

# ELB service account per region (us-east-1: 127311923021, see
# https://docs.aws.amazon.com/elasticloadbalancing/latest/application/enable-access-logging.html).
# Allow the regional ELB service account to PutObject into the log bucket.
data "aws_elb_service_account" "main" {}

data "aws_iam_policy_document" "alb_logs" {
  statement {
    sid     = "AllowELBAccessLogs"
    effect  = "Allow"
    actions = ["s3:PutObject"]
    principals {
      type        = "AWS"
      identifiers = [data.aws_elb_service_account.main.arn]
    }
    resources = ["${module.s3_alb_logs.bucket_arn}/alb/${var.env}/AWSLogs/${data.aws_caller_identity.current.account_id}/*"]
  }

  statement {
    sid     = "AllowELBLogDelivery"
    effect  = "Allow"
    actions = ["s3:PutObject"]
    principals {
      type        = "Service"
      identifiers = ["logdelivery.elasticloadbalancing.amazonaws.com"]
    }
    resources = ["${module.s3_alb_logs.bucket_arn}/alb/${var.env}/AWSLogs/${data.aws_caller_identity.current.account_id}/*"]
  }
}

resource "aws_s3_bucket_policy" "alb_logs" {
  bucket = module.s3_alb_logs.bucket_name
  policy = data.aws_iam_policy_document.alb_logs.json
}

# Per-service task + execution roles.
module "iam" {
  source   = "../../modules/iam-task-role"
  for_each = toset(local.services)

  env          = var.env
  service      = each.value
  secrets_arns = module.secrets.secret_arn_list
  kms_key_arns = [module.kms_data.key_arn]
  tags         = var.tags
}

# ECS services — one per logical service name. The healthcheck command uses
# the binary's --healthcheck flag because the images are distroless.
module "service" {
  source   = "../../modules/ecs-service"
  for_each = toset(local.services)

  env                    = var.env
  service                = each.value
  region                 = var.region
  vpc_id                 = module.vpc.vpc_id
  subnet_ids             = module.vpc.private_subnet_ids
  cluster_arn            = module.ecs_cluster.cluster_arn
  cluster_name           = module.ecs_cluster.cluster_name
  image                  = "${module.ecr.repository_urls[each.value]}:${var.image_tag}"
  container_port         = 8080
  cpu                    = lookup(var.service_cpu, each.value, 512)
  memory                 = lookup(var.service_memory, each.value, 1024)
  min_count              = lookup(var.service_min_count, each.value, 2)
  max_count              = lookup(var.service_max_count, each.value, 10)
  task_role_arn          = module.iam[each.value].task_role_arn
  execution_role_arn     = module.iam[each.value].execution_role_arn
  alb_security_group_ids = [module.alb.alb_security_group_id]
  log_group_name         = module.ecs_cluster.log_group_names[each.value]
  env_vars               = lookup(local.service_env, each.value, {})
  secrets                = module.secrets.secret_arns
  health_check_command   = ["CMD", "/${each.value}", "--healthcheck"]
  tags                   = var.tags
}

# ALB listener rules — one per public-facing service.
resource "aws_lb_listener_rule" "service" {
  for_each = local.alb_routes

  listener_arn = module.alb.https_listener_arn
  priority     = 100 + index(keys(local.alb_routes), each.key)

  action {
    type             = "forward"
    target_group_arn = module.service[each.key].target_group_arn
  }

  condition {
    host_header {
      values = [each.value.host]
    }
  }

  condition {
    path_pattern {
      values = [each.value.path]
    }
  }

  tags = var.tags
}

module "vpc_endpoints" {
  source = "../../modules/vpc-endpoints"

  env                     = var.env
  vpc_id                  = module.vpc.vpc_id
  vpc_cidr                = var.vpc_cidr
  subnet_ids              = module.vpc.private_subnet_ids
  private_route_table_ids = module.vpc.private_route_table_ids
  tags                    = var.tags
}

module "gha_oidc" {
  source = "../../modules/gha-oidc"

  env                  = var.env
  github_owner         = var.github_owner
  github_repo          = var.github_repo
  allowed_environments = ["prod"]
  allowed_refs         = ["refs/heads/main", "refs/tags/v*"]
  create_oidc_provider = true
  ecr_repository_arns  = values(module.ecr.repository_arns)
  ecs_cluster_arns     = [module.ecs_cluster.cluster_arn]
  passable_role_arns = concat(
    [for s in module.iam : s.task_role_arn],
    [for s in module.iam : s.execution_role_arn],
  )
  secret_arns = module.secrets.secret_arn_list
  tags        = var.tags
}

module "dashboard" {
  source = "../../modules/cloudwatch-dashboard"

  env                        = var.env
  region                     = var.region
  services                   = local.services
  cluster_name               = module.ecs_cluster.cluster_name
  rds_instance_id            = module.rds.instance_id
  redis_replication_group_id = "nexis-${var.env}-redis"
  alb_arn_suffix             = module.alb.alb_arn_suffix
}

# ---------------------------------------------------------------------------
# Cross-module SG wiring (post-services).
#
# Cycle-break: services need rds_proxy.endpoint at plan time (env_vars),
# so rds/rds_proxy/redis modules accept empty client_security_group_ids
# and we attach the actual ingress here, after the service SGs exist.
# ---------------------------------------------------------------------------

resource "aws_security_group_rule" "rds_from_services" {
  for_each = toset(local.services)

  type                     = "ingress"
  description              = "Postgres from ${each.value}"
  from_port                = 5432
  to_port                  = 5432
  protocol                 = "tcp"
  security_group_id        = module.rds.security_group_id
  source_security_group_id = module.service[each.value].security_group_id
}

resource "aws_security_group_rule" "rds_proxy_from_services" {
  for_each = toset(local.services)

  type                     = "ingress"
  description              = "RDS Proxy from ${each.value}"
  from_port                = 5432
  to_port                  = 5432
  protocol                 = "tcp"
  security_group_id        = module.rds_proxy.security_group_id
  source_security_group_id = module.service[each.value].security_group_id
}

resource "aws_security_group_rule" "redis_from_services" {
  for_each = toset(local.services)

  type                     = "ingress"
  description              = "Redis from ${each.value}"
  from_port                = 6379
  to_port                  = 6379
  protocol                 = "tcp"
  security_group_id        = module.redis.security_group_id
  source_security_group_id = module.service[each.value].security_group_id
}
