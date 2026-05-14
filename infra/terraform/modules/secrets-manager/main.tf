# Nexis Secrets Manager module.
#
# Creates one secret per logical name in var.secret_names. Each secret is:
# - KMS-encrypted via the per-env data-plane CMK
# - Tagged with Environment + Service (the consuming task)
# - Created with no value; operators populate values via aws secretsmanager
#   put-secret-value (or the bootstrap step in the README) — Terraform
#   intentionally does NOT manage the secret material.
#
# The module returns a map of logical_name -> ARN, which the env root passes
# into ecs-service.secrets so the values are injected at task start via the
# task definition's `secrets[]` block.

locals {
  name_prefix = "nexis/${var.env}"
}

resource "aws_secretsmanager_secret" "this" {
  for_each = toset(var.secret_names)

  name                    = "${local.name_prefix}/${each.value}"
  description             = "${each.value} for nexis-${var.env}. Managed by Terraform — values rotated out of band."
  kms_key_id              = var.kms_key_arn
  recovery_window_in_days = var.recovery_window_in_days

  tags = merge(var.tags, {
    Name        = "${local.name_prefix}/${each.value}"
    Environment = var.env
    Service     = lookup(var.service_for_secret, each.value, "shared")
  })
}
