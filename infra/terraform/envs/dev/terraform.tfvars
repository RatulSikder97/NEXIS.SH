env                     = "dev"
region                  = "us-east-1"
vpc_cidr                = "10.10.0.0/20"
az_count                = 2
nat_gateway_count       = 1
rds_instance_class      = "db.t4g.medium"
rds_storage_gb          = 50
rds_multi_az            = false
audit_object_lock_years = 1

tags = {
  Environment = "dev"
  ManagedBy   = "terraform"
  Project     = "nexis"
}
