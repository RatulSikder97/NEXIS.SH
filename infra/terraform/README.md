# Nexis Terraform

This tree provisions the Nexis production stack on AWS. It's structured as a
small set of reusable modules under `modules/` consumed by per-environment
roots under `envs/`.

## Prerequisites

- Terraform `>= 1.10` (OpenTofu `>= 1.8` also works)
- AWS CLI `>= 2.15` with credentials for the target account
- A Route 53 public hosted zone for your `root_domain` (e.g. `nexis.dev`),
  pre-created in the same account
- jq (for the bootstrap helpers in `scripts/`, if you use them)

## Bootstrap (the chicken-and-egg of S3 state)

The `envs/*/backend.tf` files declare an `s3` backend that does not exist
yet on first apply. Bootstrap it once per account:

```bash
# 1. Create the state bucket + lock table out-of-band (one-shot).
aws s3api create-bucket \
  --bucket nexis-terraform-state \
  --region us-east-1
aws s3api put-bucket-versioning \
  --bucket nexis-terraform-state \
  --versioning-configuration Status=Enabled
aws s3api put-bucket-encryption \
  --bucket nexis-terraform-state \
  --server-side-encryption-configuration '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}'
aws s3api put-public-access-block \
  --bucket nexis-terraform-state \
  --public-access-block-configuration BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true

aws dynamodb create-table \
  --table-name nexis-terraform-locks \
  --attribute-definitions AttributeName=LockID,AttributeType=S \
  --key-schema AttributeName=LockID,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST \
  --region us-east-1
```

That's the only out-of-band step. Everything else is Terraform-managed.

## Apply order

Per environment:

```bash
cd infra/terraform/envs/staging   # or envs/prod
terraform init                    # downloads providers + connects to S3 state
terraform plan -out=tfplan
terraform apply tfplan
```

The first apply takes ~25 minutes (RDS + ACM DNS validation dominate). The
plan is intentionally idempotent — re-running emits no changes.

### Required vars per env

`envs/prod/terraform.tfvars` and `envs/staging/terraform.tfvars` contain
the non-sensitive bits. Provide the sensitive ones via:

- a separate ungitted `terraform.auto.tfvars` (preferred for humans), or
- `TF_VAR_<name>` environment variables (preferred for CI)

Sensitive vars:

| Var                          | What                                                 |
|------------------------------|------------------------------------------------------|
| `root_domain`                | e.g. `nexis.dev` (prod) or `staging.nexis.dev`       |
| `github_owner`               | e.g. `nexis-eco`                                     |
| `github_repo`                | e.g. `nexis`                                         |
| `redis_bootstrap_auth_token` | `openssl rand -base64 32`; rotated post-bootstrap    |

Secrets created by the `secrets-manager` module are **empty** by Terraform
design — populate values after `apply`:

```bash
aws secretsmanager put-secret-value --secret-id nexis/prod/MASTER_KEY \
  --secret-string "$(openssl rand -base64 32)"
aws secretsmanager put-secret-value --secret-id nexis/prod/JWT_SIGNING_KEY \
  --secret-string "$(openssl genrsa 4096 | base64)"
# ... and so on for the rest
```

## What's deployed

```
envs/prod                       envs/staging
├── VPC (3 AZs, 3 NAT)          ├── VPC (2 AZs, 1 NAT)
├── KMS data + audit             ├── KMS data + audit
├── S3 patches + audit + alb-logs├── S3 patches + audit + alb-logs
├── Secrets Manager (8)         ├── Secrets Manager (8)
├── RDS Postgres (Multi-AZ)     ├── RDS Postgres (single-AZ)
├── RDS Proxy                   │   (no proxy — cost optimisation)
├── ElastiCache Redis (2-node)  ├── ElastiCache Redis (1-node)
├── ECR x 5                     ├── ECR x 5
├── ECS Fargate cluster         ├── ECS Fargate cluster
├── 5 ECS services              ├── 5 ECS services
├── ALB + ACM + Route 53        ├── ALB + ACM + Route 53
├── GHA OIDC role               ├── GHA OIDC role (reuses provider)
├── VPC endpoints (5 + S3 gw)   ├── VPC endpoints (5 + S3 gw)
└── CloudWatch dashboard        └── CloudWatch dashboard
```

The 5 ECS services are: `control-plane`, `web`, `validator`, `gitops`,
`causal-inference`.

## Wiring the app config to AWS adapters

After `apply`, set these per-env config flags to switch the running app
from local-stub providers to AWS-backed ones (the audit Wave 1 work landed
the switches; this is just the prod values):

```
cfg.SecretsProvider = "aws"            # uses Secrets Manager via task secrets[]
cfg.StorageProvider = "s3"             # uses module.s3_patches.bucket_name
cfg.AuditExportBucket = <output>       # module.s3_audit_export.bucket_name
cfg.DBEndpoint = <rds_proxy_endpoint>  # via module.rds_proxy.proxy_endpoint
cfg.RedisEndpoint = <redis_endpoint>   # via module.redis.primary_endpoint
cfg.KMSDataKeyArn = <kms_data_key_arn> # via module.kms_data.key_arn
```

These are surfaced as Terraform outputs (`terraform output -json`); the
GHA release workflow plugs them into the task definition's `environment[]`
block at deploy time. The Terraform-rendered task definition in the
`ecs-service` module already wires the static ones via `local.service_env`
in `envs/prod/main.tf`.

## Module reference

| Module                   | Purpose                                              |
|--------------------------|------------------------------------------------------|
| `vpc`                    | VPC + subnets + NAT + route tables + flow log hooks  |
| `kms-key`                | Customer-managed KMS key with rotation               |
| `s3-bucket`              | Encrypted S3 bucket with optional Object Lock        |
| `secrets-manager`        | Empty Secrets Manager secrets, KMS-encrypted         |
| `rds`                    | Postgres 16 + pgvector, master pwd in Secrets Mgr    |
| `rds-proxy`              | RDS Proxy fronting the RDS instance                  |
| `elasticache-redis`      | Redis 7.1 replication group with TLS + AUTH          |
| `ecr`                    | ECR repos (one per service, immutable tags, scan-on-push) |
| `ecs-cluster`            | Fargate cluster + capacity providers + log groups    |
| `ecs-service`            | One Fargate service + ALB target group + autoscaling |
| `alb`                    | ALB + ACM cert + Route 53 records                    |
| `iam-task-role`          | Task + execution roles for one ECS service           |
| `vpc-endpoints`          | Interface endpoints (ECR/SM/KMS/Logs) + S3 gateway   |
| `gha-oidc`               | GitHub Actions OIDC trust + deploy role              |
| `cloudwatch-dashboard`   | Per-env operational dashboard                        |

## Verification (no AWS calls)

```bash
cd infra/terraform/envs/prod
terraform fmt -recursive ..        # format check
terraform init -backend=false      # local init without S3 state
terraform validate                  # syntax + type check
```

`validate` should pass with zero errors.

## Per-org KMS alias strategy (data plane)

Wave 7 of the audit specifies per-org KMS aliases (`alias/nexis-org-<id>`)
sharing a single underlying CMK. The control-plane creates these aliases
at org-create time via the AWS SDK — Terraform does not manage them
because the org list is dynamic. The CMK they target is `module.kms_data`
in this tree. To migrate to per-org dedicated CMKs (BYOK) later, swap
the alias target in the control-plane's KMS adapter; no Terraform change
needed.

## Cost guard rails

- `FARGATE_SPOT` weight is 20% in prod, 50% in staging (recreates on
  reclamation; ECS handles it gracefully because each service is multi-task)
- VPC interface endpoints break even ~12 GB/mo against NAT egress
- Audit export bucket uses Glacier Deep Archive after 90 days
- RDS Proxy is prod-only (~$80/mo) — staging connects directly to RDS

## What this scaffold does NOT do

Documented out of scope here, tracked in the audit:

- Log shipping to Grafana Cloud (CloudWatch is the only sink today)
- Blue/green deploy controller (services use the rolling ECS deployment)
- Temporal Cloud mTLS provisioning (out-of-AWS)
- Modal validator hand-off (out-of-AWS API)
- SOC2 control mappings (IAM scoping is least-priv but not control-mapped)
- Per-org dedicated CMKs (we use a shared data CMK with per-org aliases)
- WAF (add `aws_wafv2_web_acl` association on the ALB when ready)
- CloudFront in front of the web app (add for global users)
