# Nexis ECR module.
# Provisions one ECR repo per service with:
# - immutable tags (prod image tags must never be overwritten)
# - AES256 SSE (KMS optional via var.kms_key_arn)
# - scan-on-push enabled
# - lifecycle policy: keep last 30 tagged, expire untagged after 7 days
#
# Repos are namespaced under "nexis/<service>" so a single registry account
# can host multiple environments by tag (e.g. nexis/control-plane:prod-<sha>).

locals {
  encryption_type = var.kms_key_arn == null ? "AES256" : "KMS"
}

resource "aws_ecr_repository" "this" {
  for_each = toset(var.services)

  name                 = "nexis/${each.value}"
  image_tag_mutability = "IMMUTABLE"
  force_delete         = var.force_delete

  image_scanning_configuration {
    scan_on_push = true
  }

  encryption_configuration {
    encryption_type = local.encryption_type
    kms_key         = var.kms_key_arn
  }

  tags = merge(var.tags, {
    Name    = "nexis/${each.value}"
    Service = each.value
  })
}

# Lifecycle policy: keep 30 most recent tagged images, expire untagged after 7d.
# Rule priorities are evaluated in ascending order; lower priority wins.
resource "aws_ecr_lifecycle_policy" "this" {
  for_each   = aws_ecr_repository.this
  repository = each.value.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Expire untagged images after 7 days"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 7
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Keep the last 30 tagged images"
        selection = {
          tagStatus   = "any"
          countType   = "imageCountMoreThan"
          countNumber = 30
        }
        action = { type = "expire" }
      },
    ]
  })
}
