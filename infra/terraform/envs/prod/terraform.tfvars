env                     = "prod"
region                  = "us-east-1"
vpc_cidr                = "10.30.0.0/16"
az_count                = 3
nat_gateway_count       = 3
rds_instance_class      = "db.m6g.large"
rds_storage_gb          = 500
rds_multi_az            = true
redis_node_type         = "cache.r7g.large"
audit_object_lock_years = 7

# Provide root_domain + github_* + redis_bootstrap_auth_token via:
#   - environment overrides: TF_VAR_root_domain=nexis.dev
#   - or a separate ungitted terraform.auto.tfvars with the sensitive bits.
# A reasonable default for the boostrap auth token: openssl rand -base64 32.

# image_tag is overridden by the GHA release workflow per deploy.
image_tag = "bootstrap"

tags = {
  Environment = "prod"
  ManagedBy   = "terraform"
  Project     = "nexis"
}
