# Nexis prod env root.
# Multi-region-ready data-plane KMS, 7-year audit Object Lock, larger RDS,
# 3 NAT gateways. Manual-gated terraform apply via GHA per spec §12.

provider "aws" {
  region = var.region

  default_tags {
    tags = var.tags
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

module "rds" {
  source = "../../modules/rds"

  env                    = var.env
  vpc_id                 = module.vpc.vpc_id
  subnet_ids             = module.vpc.private_subnet_ids
  ecs_security_group_ids = []
  kms_key_arn            = module.kms_data.key_arn
  instance_class         = var.rds_instance_class
  storage_gb             = var.rds_storage_gb
  multi_az               = var.rds_multi_az
  backup_retention_days  = 35
  tags                   = var.tags
}
