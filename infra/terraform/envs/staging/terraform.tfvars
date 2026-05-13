env                     = "staging"
region                  = "us-east-1"
vpc_cidr                = "10.20.0.0/16"
az_count                = 3
nat_gateway_count       = 3
rds_instance_class      = "db.m6g.large"
rds_storage_gb          = 200
rds_multi_az            = true
audit_object_lock_years = 7

tags = {
  Environment = "staging"
  ManagedBy   = "terraform"
  Project     = "nexis"
}
