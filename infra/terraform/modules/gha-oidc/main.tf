# Nexis GitHub Actions OIDC trust + deploy role.
#
# Federates the GitHub Actions OIDC provider into AWS so workflows in the
# repo can assume an env-scoped role WITHOUT long-lived access keys.
#
# Permissions on the assumed role:
# - ECR push (per the repo ARNs passed in)
# - ECS describe + update-service (per cluster)
# - Secrets Manager read (per secret ARNs)
# - PassRole on the task + execution roles so ECS can register revised task
#   definitions
#
# Trust policy restricts assumption to the configured repo + branch/env
# refs to prevent token reuse from forks or unrelated repos.

locals {
  name = "nexis-${var.env}-gha-deploy"

  # `repo:<owner>/<repo>:environment:<env>` is the canonical pattern; we also
  # accept ref:refs/heads/<branch> via var.allowed_refs for branch-pinned jobs.
  allowed_subs = concat(
    [for env in var.allowed_environments : "repo:${var.github_owner}/${var.github_repo}:environment:${env}"],
    [for ref in var.allowed_refs : "repo:${var.github_owner}/${var.github_repo}:ref:${ref}"],
  )
}

# Single OIDC provider per AWS account. If it already exists (managed by
# another env's bootstrap), set var.create_oidc_provider = false.
resource "aws_iam_openid_connect_provider" "github" {
  count = var.create_oidc_provider ? 1 : 0

  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = var.oidc_thumbprints

  tags = merge(var.tags, { Name = "github-actions-oidc" })
}

data "aws_iam_openid_connect_provider" "github" {
  count = var.create_oidc_provider ? 0 : 1
  url   = "https://token.actions.githubusercontent.com"
}

locals {
  oidc_provider_arn = var.create_oidc_provider ? aws_iam_openid_connect_provider.github[0].arn : data.aws_iam_openid_connect_provider.github[0].arn
}

data "aws_iam_policy_document" "assume" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [local.oidc_provider_arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      values   = local.allowed_subs
    }
  }
}

resource "aws_iam_role" "deploy" {
  name                 = local.name
  assume_role_policy   = data.aws_iam_policy_document.assume.json
  max_session_duration = 3600
  tags                 = merge(var.tags, { Name = local.name })
}

# ECR push — scoped to the repo ARNs passed in.
data "aws_iam_policy_document" "ecr_push" {
  statement {
    effect    = "Allow"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }

  statement {
    effect = "Allow"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:CompleteLayerUpload",
      "ecr:InitiateLayerUpload",
      "ecr:PutImage",
      "ecr:UploadLayerPart",
      "ecr:BatchGetImage",
      "ecr:DescribeImages",
      "ecr:DescribeRepositories",
    ]
    resources = var.ecr_repository_arns
  }
}

resource "aws_iam_role_policy" "ecr_push" {
  name   = "ecr-push"
  role   = aws_iam_role.deploy.id
  policy = data.aws_iam_policy_document.ecr_push.json
}

# ECS update — describe + update-service on the cluster's services.
data "aws_iam_policy_document" "ecs_update" {
  statement {
    effect = "Allow"
    actions = [
      "ecs:DescribeServices",
      "ecs:DescribeTaskDefinition",
      "ecs:DescribeTasks",
      "ecs:ListTasks",
      "ecs:RegisterTaskDefinition",
      "ecs:UpdateService",
      "ecs:DeregisterTaskDefinition",
    ]
    resources = ["*"]
    condition {
      test     = "StringEquals"
      variable = "ecs:cluster"
      values   = var.ecs_cluster_arns
    }
  }

  # RegisterTaskDefinition has no cluster condition — restrict separately.
  statement {
    effect    = "Allow"
    actions   = ["ecs:RegisterTaskDefinition", "ecs:DescribeTaskDefinition"]
    resources = ["*"]
  }

  statement {
    effect    = "Allow"
    actions   = ["iam:PassRole"]
    resources = var.passable_role_arns
    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values   = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role_policy" "ecs_update" {
  name   = "ecs-update"
  role   = aws_iam_role.deploy.id
  policy = data.aws_iam_policy_document.ecs_update.json
}

# Secrets Manager read (CI sometimes needs to read for integration smoke).
data "aws_iam_policy_document" "secrets_read" {
  count = length(var.secret_arns) > 0 ? 1 : 0

  statement {
    effect    = "Allow"
    actions   = ["secretsmanager:GetSecretValue", "secretsmanager:DescribeSecret"]
    resources = var.secret_arns
  }
}

resource "aws_iam_role_policy" "secrets_read" {
  count  = length(var.secret_arns) > 0 ? 1 : 0
  name   = "secrets-read"
  role   = aws_iam_role.deploy.id
  policy = data.aws_iam_policy_document.secrets_read[0].json
}
