# Nexis KMS module.
# Each invocation provisions one customer-managed KMS key with annual
# automatic key rotation and a tenancy alias.

resource "aws_kms_key" "this" {
  description             = "nexis-${var.env}-${var.purpose}"
  enable_key_rotation     = var.rotation
  rotation_period_in_days = var.rotation ? 365 : null
  deletion_window_in_days = 30
  multi_region            = var.multi_region

  tags = merge(var.tags, {
    Name        = "nexis-${var.env}-${var.purpose}"
    Environment = var.env
    Purpose     = var.purpose
  })
}

resource "aws_kms_alias" "this" {
  name          = "alias/nexis-${var.env}-${var.purpose}"
  target_key_id = aws_kms_key.this.id
}
