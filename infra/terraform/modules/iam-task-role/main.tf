# Nexis IAM task role module — pairs task role + execution role per service.
# var.policies is a list of {name, document} that get attached as inline
# policies to the task role; the execution role gets the AWS-managed
# AmazonECSTaskExecutionRolePolicy plus an optional ECR pull policy for
# Secrets Manager / KMS access during container start.

locals {
  name = "nexis-${var.env}-${var.service}"
}

data "aws_iam_policy_document" "task_assume" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "task" {
  name               = "${local.name}-task"
  assume_role_policy = data.aws_iam_policy_document.task_assume.json
  tags               = merge(var.tags, { Name = "${local.name}-task" })
}

resource "aws_iam_role_policy" "task" {
  count  = length(var.policies)
  name   = var.policies[count.index].name
  role   = aws_iam_role.task.id
  policy = var.policies[count.index].document
}

resource "aws_iam_role" "execution" {
  name               = "${local.name}-exec"
  assume_role_policy = data.aws_iam_policy_document.task_assume.json
  tags               = merge(var.tags, { Name = "${local.name}-exec" })
}

resource "aws_iam_role_policy_attachment" "ecr_pull" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# Execution role also needs to pull secrets at task-start (the `secrets` block
# in task definitions). Scoped to the per-service prefix to keep blast radius
# tight.
data "aws_iam_policy_document" "exec_secrets" {
  count = length(var.secrets_arns) > 0 ? 1 : 0

  statement {
    effect    = "Allow"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = var.secrets_arns
  }

  dynamic "statement" {
    for_each = var.kms_key_arns
    content {
      effect    = "Allow"
      actions   = ["kms:Decrypt"]
      resources = [statement.value]
    }
  }
}

resource "aws_iam_role_policy" "exec_secrets" {
  count  = length(var.secrets_arns) > 0 ? 1 : 0
  name   = "secrets-fetch"
  role   = aws_iam_role.execution.id
  policy = data.aws_iam_policy_document.exec_secrets[0].json
}
