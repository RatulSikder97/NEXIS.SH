variable "env" {
  description = "Environment name (dev, staging, prod)."
  type        = string
}

variable "github_owner" {
  description = "GitHub org/user that owns the repo."
  type        = string
}

variable "github_repo" {
  description = "GitHub repo name (without owner)."
  type        = string
}

variable "allowed_environments" {
  description = "GitHub Environments whose runs may assume this role (e.g. [\"prod\"] or [\"staging\"])."
  type        = list(string)
  default     = []
}

variable "allowed_refs" {
  description = "Git refs whose runs may assume this role (e.g. [\"refs/heads/main\", \"refs/tags/v*\"])."
  type        = list(string)
  default     = []
}

variable "create_oidc_provider" {
  description = "Whether to create the OIDC provider. Set false in additional envs after the first env creates it."
  type        = bool
  default     = true
}

variable "oidc_thumbprints" {
  description = "Thumbprints for token.actions.githubusercontent.com. Default is GitHub's current root CA."
  type        = list(string)
  default     = ["6938fd4d98bab03faadb97b34396831e3780aea1"]
}

variable "ecr_repository_arns" {
  description = "ECR repo ARNs the deploy role may push to."
  type        = list(string)
  default     = []
}

variable "ecs_cluster_arns" {
  description = "ECS cluster ARNs the deploy role may update services on."
  type        = list(string)
  default     = []
}

variable "passable_role_arns" {
  description = "Task/execution role ARNs the deploy role can pass when registering task definitions."
  type        = list(string)
  default     = []
}

variable "secret_arns" {
  description = "Secrets Manager ARNs the deploy role may read (optional)."
  type        = list(string)
  default     = []
}

variable "tags" {
  description = "Tags merged into the IAM role + OIDC provider."
  type        = map(string)
  default     = {}
}
