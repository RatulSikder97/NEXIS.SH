env                     = "staging"
region                  = "us-east-1"
vpc_cidr                = "10.20.0.0/16"
az_count                = 2
nat_gateway_count       = 1
rds_instance_class      = "db.t4g.medium"
rds_storage_gb          = 100
rds_multi_az            = false
redis_node_type         = "cache.t4g.small"
audit_object_lock_years = 7
image_tag               = "bootstrap"

tags = {
  Environment = "staging"
  ManagedBy   = "terraform"
  Project     = "nexis"
}
