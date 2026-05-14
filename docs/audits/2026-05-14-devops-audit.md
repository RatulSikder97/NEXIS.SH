# DevOps Audit — 2026-05-14

## Summary

The local docker-compose stack boots and the Terraform skeleton for VPC/RDS/ECS/KMS/S3 is in place, but the path to production has substantial structural gaps. The compose file has zero restart policies, zero resource limits, no log driver rotation, and uses a single shared dev password (`nexis_dev_password`) across Postgres, MinIO, Neo4j and Temporal. The control-plane has 100+ environment variables wired by hand in `docker-compose.yml` with hardcoded fallbacks for production secrets (`MASTER_KEY`, `SESSION_SECRET`, `AUDIT_SECRET`). On the Phase 7 side the Terraform tree only stops at networking + data-plane — no ALB, no ACM, no Route 53, no ECR repos, no CloudWatch log groups, no ECS cluster, no service modules wired, no Secrets Manager resources, and `FatalIfLocalInCloud()` is defined but never invoked in `main.go`. CI builds Docker images but never pushes, scans, or caches them. Counts: **3 Critical, 11 High, 12 Medium, 8 Low**.

---

## Critical (blocker / data-loss / security)

### F-1: Production secrets fall through to hardcoded dev defaults
**Where:** `docker-compose.yml:220-229,237,273,313,327` and `services/control-plane/internal/platform/config/config.go:273,287,291,329`
**Impact:** Every secret in the control-plane has a hardcoded production-shaped default. `SESSION_SECRET=${SESSION_SECRET:-dev-session-secret-replace-in-prod-32}`, `AUDIT_SECRET=${...:-dev-audit-secret-replace-in-prod-32}`, `MASTER_KEY=${MASTER_KEY:-dGVzdC1tYXN0ZXIta2V5LTMyLWJ5dGVzLWxvbmctYWFh}` (which is the base64 string "test-master-key-32-bytes-long-aaa"), `VALIDATOR_TOKEN=${...:-dev-validator-token-32byte}`, `GITHUB_WEBHOOK_SECRET=${...:-dev-github-webhook-secret-32-byte}`, `GITOPS_TOKEN=${...:-dev-gitops-token-32byte}`. The same defaults exist in `config.go:273` (`GitHubDefaultWebhookSecret`) and `config.go:287` (`ValidatorToken`). If anyone copies this compose file into a non-dev environment with `.env` missing — or `docker compose up` is run by a CI job that forgets to pre-seed env — they boot with publicly-known crypto keys that an attacker can forge sessions / encrypted secrets against.
**Recommendation:**
1. Remove every `:-<default>` from `docker-compose.yml` for security-sensitive vars; let `docker compose` error on the unset variable.
2. In `config.Load()` change `MasterKey`, `SessionSecret`, `AuditSecret`, `ValidatorToken`, `GitHubDefaultWebhookSecret`, `GitOpsToken` to error or zero-default. Then add a startup assertion: if `cfg.AppEnv != "dev"` and any of these are empty or match a published default, `log.Fatal`.
3. Ship a `.env.local.example` with non-secret defaults and a `make bootstrap` target that generates fresh secrets via `openssl rand -base64 32`.

### F-2: `FatalIfLocalInCloud` is defined but never called
**Where:** `services/control-plane/internal/platform/config/config.go:415` (defined); not invoked in `services/control-plane/cmd/server/main.go`
**Impact:** The Phase 7 cutover contract's "first line of defence (Risk 16.14)" — per the comment in `config.go:412` — exists as dead code. A staging/prod deploy where `AUTH_PROVIDER=local`, `KEYVAULT=local`, `SECRETS=env`, `VALIDATOR_RUNNER=docker`, `TEMPORAL_CLOUD=0`, or `OTLP_TARGET=local` will boot silently and pretend to be production. This is the same class of bug as F-1 but harder to spot.
**Recommendation:** In `cmd/server/main.go` right after `cfg := config.Load()`, call `if err := cfg.FatalIfLocalInCloud(); err != nil { log.Fatalf("local provider in cloud env: %v", err) }`. Repeat in `services/validator/cmd/server/main.go` and `services/gitops/cmd/server/main.go` once they grow a `Config` of their own.

### F-3: `secrets/github-app.pem` is tracked in git
**Where:** `secrets/github-app.pem` (0-byte placeholder, `git ls-files secrets/` returns it)
**Impact:** It is currently empty, so the immediate blast radius is zero — but the file is wired into the docker-compose `secrets:` block (`docker-compose.yml:352-354`), wired into both `control-plane` and `gitops` services, and `loadGitHubAppPEM()` (`config.go:512`) will pick it up. The moment any contributor runs `make seed-github-app` (or the equivalent the README implies) and forgets to git-rm it first, a real GitHub App private key lands in git history. `.gitignore` does not exclude `secrets/*.pem` and `.dockerignore` does not exclude `secrets/`.
**Recommendation:**
1. `git rm secrets/github-app.pem` and add `secrets/*` (with an allowed `!secrets/.gitkeep`) to `.gitignore`.
2. Document that the file must be created out-of-band via `op read` / SOPS / `aws secretsmanager get-secret-value`.
3. In Phase 7 wire the control-plane and gitops Fargate task definitions to read the PEM from Secrets Manager (via `GITHUB_APP_PRIVATE_KEY_PEM` base64 path — that branch is already in `config.go:512-525`).

---

## High (production-blocker, fix before Phase 7)

### F-4: No `restart:` policy on any service
**Where:** `docker-compose.yml` (entire file — 0 `restart:` lines)
**Impact:** When `temporal-ui`, `causal-inference`, `validator`, `gitops`, or any built service crashes (panic, OOM, depend on something flapping) compose leaves it dead. Devs assume the stack is "up" and silently get 500s from the gateway. CI runs that boot the stack for e2e are especially vulnerable. The cluster-level `unless-stopped` is needed.
**Recommendation:** Add `restart: unless-stopped` to every service. Use `restart: on-failure` only for one-shot init jobs (none exist today).

### F-5: No resource limits anywhere
**Where:** `docker-compose.yml` (entire file)
**Impact:** Temporal+Postgres+Neo4j+OTel+Prometheus+Loki+Tempo+Grafana on one laptop will balloon to 8-16 GB RSS. Neo4j alone defaults to 1 GB heap; without `NEO4J_server_memory_heap_max__size` it grabs whatever it wants. A runaway control-plane goroutine will hog CPU. On a CI runner this OOM-kills sibling jobs.
**Recommendation:** Add `mem_limit` + `cpus` (compose v2 syntax: `deploy.resources.limits` under each service). Suggested floor:
- `postgres`: 1g/1cpu
- `neo4j`: 1g/1cpu (also set `NEO4J_server_memory_heap_max__size=512m`)
- `temporal` + `temporal-ui`: 768m/0.5cpu each
- `prometheus`/`loki`/`tempo`/`otel-collector`: 256m/0.25cpu each
- `control-plane`: 512m/1cpu
- `web`: 512m/1cpu

### F-6: No log driver rotation
**Where:** `docker-compose.yml` (no `logging:` block on any service)
**Impact:** Default `json-file` driver with unbounded size. A chatty `slog` line at INFO + the `verbosity: basic` debug exporter in `infra/observability/otel-collector.yaml:23` will fill `/var/lib/docker/containers/*/...-json.log` until the disk fills. Already saw this with the Temporal autoSetup which logs at startup.
**Recommendation:** Add a YAML anchor at the top:
```yaml
x-logging: &default-logging
  logging:
    driver: json-file
    options: { max-size: "10m", max-file: "3" }
```
Then `<<: *default-logging` on every service.

### F-7: Redis has no persistence and no password
**Where:** `docker-compose.yml:23-32`
**Impact:** Redis is used by the GitHub installation-token cache (`internal/adapter/integration/github/installation_token_cache.go:20`). It has (a) no volume — a `docker compose down` wipes the token cache and forces re-mint of every JWT; (b) no `requirepass`; (c) is published on `0.0.0.0:6379` (see F-9). Locally the cache loss is annoying; the open-port story is the bigger risk.
**Recommendation:** Add `volumes: [redis_data:/data]`, `command: ["redis-server", "--appendonly", "yes", "--requirepass", "${REDIS_PASSWORD:-nexis_dev_password}"]`, and either drop the `ports:` mapping or bind to `127.0.0.1:6379:6379`.

### F-8: MinIO bucket lifecycle is never created
**Where:** `docker-compose.yml:34-50` (no companion `mc` init container)
**Impact:** The control-plane is configured with `PATCH_STORE: minio` (`docker-compose.yml:238`) and expects buckets to exist for patch storage. There is no `mc mb` init job, so the first `PUT /patches/...` either creates an unversioned bucket (data loss on overwrite) or 404s depending on the SDK. Phase 7 mirrors this on S3 with `S3PatchBucket` but the local-dev round-trip is broken until someone runs `mc` by hand.
**Recommendation:** Add a `minio-init` service:
```yaml
minio-init:
  image: minio/mc:latest
  depends_on: { minio: { condition: service_healthy } }
  entrypoint: >-
    /bin/sh -c "
    mc alias set local http://minio:9000 nexis nexis_dev_password &&
    mc mb -p local/nexis-patches local/nexis-audit-export &&
    mc version enable local/nexis-audit-export"
  networks: [nexis]
```

### F-9: Every internal service is bound to `0.0.0.0`
**Where:** `docker-compose.yml` — `5432`, `6379`, `7233`, `7474`, `7687`, `8090`, `8233`, `9000`, `9001`, `1025`, `8025`, `4317`, `4318`, `9090`, `3030`, `3100`, `3200`, `8025` are all `"X:X"` (== `0.0.0.0:X:X`)
**Impact:** Any contributor on a coffee-shop Wi-Fi exposes Postgres, Redis, Temporal, Neo4j, MinIO, MailHog, Prometheus, Loki, Tempo, Grafana, OTLP, validator, and gitops to the LAN — most with weak shared dev creds. Neo4j browser at `7474` is unauthenticated to anyone on the LAN with the password `nexis_dev_password`. Grafana at `3030` is `GF_AUTH_ANONYMOUS_ENABLED=true` + `GF_AUTH_ANONYMOUS_ORG_ROLE=Admin` (`docker-compose.yml:166-167`).
**Recommendation:** Prefix internal-only services with `127.0.0.1:`. Keep only `web:3000`, `control-plane:8080` (and arguably grafana:3030) on `0.0.0.0`. Even `temporal-ui:8233` should be loopback-only.

### F-10: Grafana is admin-anonymous
**Where:** `docker-compose.yml:165-169`
**Impact:** `GF_AUTH_ANONYMOUS_ENABLED=true` + `GF_AUTH_ANONYMOUS_ORG_ROLE=Admin` means anyone who can reach `:3030` can edit dashboards, datasources, and exfiltrate Postgres credentials embedded in Grafana datasource configs. The hardcoded `admin/admin` admin user makes this worse.
**Recommendation:** Drop `GF_AUTH_ANONYMOUS_*` entirely; set `GF_SECURITY_ADMIN_PASSWORD: ${GRAFANA_ADMIN_PASSWORD:?must be set}`. Bind to `127.0.0.1:3030:3000`.

### F-11: Web service depends only on Postgres, not on the API
**Where:** `docker-compose.yml:200-203`
**Impact:** `web` boots while `control-plane` is still building/dialing Temporal/Neo4j. SSR fetches to `API_URL_INTERNAL=http://control-plane:8080` return ECONNREFUSED for the first 30-90 seconds, the Next.js server logs panic noise, and the first dashboard SSR snapshot caches an error response. Latest commit `5029759` ("server-side fetches now respect API_URL_INTERNAL — fixes /console/projects empty list inside docker") is symptomatic of the same race.
**Recommendation:** Add a control-plane healthcheck (currently has none!) and `depends_on: control-plane: { condition: service_healthy }` on `web`. Also add a healthcheck to `gitops` and `validator`.

### F-12: control-plane / gitops / validator have no healthchecks
**Where:** `docker-compose.yml:205-340` (control-plane block, gitops block, validator block)
**Impact:** They expose `/healthz` (verified in `services/control-plane/internal/transport/http/server.go:162`, `services/validator/cmd/server/main.go:38`, `services/gitops/internal/transport/http/handler.go:37`) but compose never probes them. `depends_on: service_started` from one of them only waits for the container to exist, not for the HTTP server to bind. This combined with F-11 is the root cause of boot races.
**Recommendation:** Add to each (distroless images don't have `curl`, so use `wget -qO-` from the Go binary's BusyBox — or better, build a tiny `/healthz` Go subcommand and use `CMD ["/app/server", "healthz"]`):
```yaml
healthcheck:
  test: ["CMD-SHELL", "wget -qO- http://localhost:8080/healthz || exit 1"]
  interval: 10s
  timeout: 3s
  retries: 10
```
Note: distroless static images have no shell or wget — see F-22 for the proper fix.

### F-13: Postgres init script puts both DB owner + RLS role passwords in plaintext SQL
**Where:** `infra/postgres/init/01-nexis-app-role.sql:6` and `docker-compose.yml:328`
**Impact:** `nexis_app` password (`nexis_app_dev_password`) is baked into the init SQL. There is also a third role `nexis_gitops` referenced in `docker-compose.yml:328` (`DATABASE_URL: postgres://nexis_gitops:nexis_dev_password@...`) which is *not* created by the init script — gitops will fail to connect on first boot until someone runs `CREATE ROLE nexis_gitops ...` by hand.
**Recommendation:**
1. Add `02-nexis-gitops-role.sql` with `CREATE ROLE nexis_gitops LOGIN PASSWORD :gitops_password`.
2. Pull passwords from env at init time: postgres' `docker-entrypoint-initdb.d` can execute `.sh` scripts that `envsubst < tmpl.sql | psql`. Or use the `POSTGRES_INITDB_ARGS` hook.
3. In Phase 7 these roles are created by an RDS bootstrap job that pulls from Secrets Manager.

### F-14: Image tags are pinned but `minio/minio:latest` and `mailhog/mailhog:latest` are not
**Where:** `docker-compose.yml:35` (minio:latest), `:53` (mailhog:latest)
**Impact:** A breaking minio change (and there have been several CLI breaking changes in 2025) will silently land on the next `docker compose pull`. Reproducibility goal is broken.
**Recommendation:** Pin: `minio/minio:RELEASE.2025-03-12T18-04-18Z` (or whatever the last verified tag is). `mailhog/mailhog:v1.0.1` is the last release — pin that.

---

## Medium (quality-of-life, dev experience)

### F-15: control-plane Dockerfile rebuilds Go modules on every source change
**Where:** `services/control-plane/Dockerfile:4-9`
**Impact:** `COPY . .` invalidates the Go build cache on any source change. With four `go build` invocations the cold-build is ~3-5x slower than necessary.
**Recommendation:** Use BuildKit cache mounts:
```dockerfile
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
```
Same applies to `services/validator/Dockerfile:23-28` and `services/gitops/Dockerfile`.

### F-16: Four separate binaries in one image
**Where:** `services/control-plane/Dockerfile:7-17`
**Impact:** `server`, `seed-pgvector`, `seed-neo4j`, `eval` are all in the runtime image. Final image carries ~80 MB of dead code in prod. Seed/eval should be a separate "tools" image or built via `docker compose run --rm tools <cmd>`.
**Recommendation:** Use Go's `-tags` to compile a multi-command binary with subcommands, or split the Dockerfile into two stages and use the `target` field in compose.

### F-17: Validator Dockerfile installs `docker.io` apt package — large + unstable
**Where:** `services/validator/Dockerfile:40-42`
**Impact:** `docker.io` from Debian repos is a 200 MB monster; the comment ("we pin nothing here") is honest but means a `docker compose build --no-cache` on a different day produces a different image with different CLI semantics. Also: the docker-cli signature surface that gets installed is huge for what is just `docker run --rm`.
**Recommendation:** Replace with `docker-ce-cli` from the official Docker apt repo, pinned: `apt-get install -y docker-ce-cli=5:24.0.7-1~debian.12~bookworm` — or even simpler, the static binary download from `https://download.docker.com/linux/static/`.

### F-18: causal-inference is a Python service with no healthcheck *that uses Python*
**Where:** `docker-compose.yml:117-121`
**Impact:** The healthcheck shells out to Python with a gRPC channel-ready probe, but the service runs FastAPI/uvicorn (not gRPC — see `services/causal-inference/Dockerfile:27`: `CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8090"]`). The probe will always fail/never succeed because there's no gRPC server. The config has `CausalGRPCEndpoint: "causal-inference:8090"` (`config.go:323`) implying gRPC. Wire mismatch.
**Recommendation:** Either (a) switch the service to gRPC (the Phase 7 plan per Dockerfile comments) and verify the probe works, or (b) update the compose healthcheck to `curl -fsS http://localhost:8090/healthz` and add a `/healthz` route to the FastAPI app.

### F-19: web Dockerfile does `pnpm install` across the full workspace
**Where:** `apps/web/Dockerfile:9` (followed by `COPY . .` at `:17`)
**Impact:** `pnpm install` runs once but `COPY . .` busts the deps layer cache on any file change. The build context also includes `node_modules` and `.next` unless `.dockerignore` (which it does — but contributors often forget). Cold build ~120 s.
**Recommendation:** Use `pnpm fetch` + `pnpm install --offline` pattern, and add a `.next` exclusion to `.dockerignore` (already present per line 4).

### F-20: `.dockerignore` excludes `*.md` — a footgun
**Where:** `.dockerignore:14`
**Impact:** `*.md` is excluded from build context. Most builds don't care, but the moment a docs subapp embeds README content or a build script reads `CHANGELOG.md`, it silently breaks. The `apps/docs` app exists already (`apps/docs/`) — confirm it doesn't `import` markdown at build time.
**Recommendation:** Replace `*.md` with explicit exclusions: `README.md`, `CHANGELOG.md`, `docs/**/*.md` (this audit lives in `docs/` so the latter is fine for prod images).

### F-21: Temporal storage lives in shared Postgres with no separation
**Where:** `docker-compose.yml:59-77`
**Impact:** `temporalio/auto-setup:1.25.2` puts Temporal's own metadata, visibility, and history tables in the same `nexis` database as the app. Migrations or backups treat the two as one corpus. In Phase 7 this is moot (Temporal Cloud), but for dev/test it makes `pg_dump nexis` produce a 100 MB dump full of Temporal noise.
**Recommendation:** Set `POSTGRES_DB: temporal` and `POSTGRES_VISIBILITY_DB: temporal_visibility` (Temporal auto-setup respects both). The app's `nexis` DB then needs to be created by an init script or by passing multiple init SQLs.

### F-22: Distroless image cannot satisfy a curl/wget healthcheck
**Where:** `services/control-plane/Dockerfile:12`, `services/gitops/Dockerfile:9` (both `gcr.io/distroless/static-debian12:nonroot`)
**Impact:** Distroless has no shell and no curl. Any compose healthcheck using `CMD-SHELL` or `wget` will not work. The recommendation in F-12 needs the build to ship a tiny healthcheck binary.
**Recommendation:** Add `RUN CGO_ENABLED=0 go build -o /out/healthcheck ./cmd/healthcheck` (which exits 0 if `/healthz` responds 200), `COPY --from=builder /out/healthcheck /app/healthcheck`, then `HEALTHCHECK CMD ["/app/healthcheck"]`. Or use grpc_health_probe style binary.

### F-23: OTel collector debug exporter is on for metrics/logs
**Where:** `infra/observability/otel-collector.yaml:22-23,34,38`
**Impact:** `debug: { verbosity: basic }` writes a JSON line per span/log to the collector container's stdout. With ~100 logs/sec this fills the container log + drives stdout I/O. Metrics pipeline has *only* the debug exporter (`exporters: [debug]` at line 34) — no metrics are actually exported anywhere.
**Recommendation:** Remove `debug` from production pipelines. Wire the metrics pipeline to `prometheusremotewrite` (or `prometheus` scrape). Note: `prometheus.yml` doesn't even scrape the otel-collector's metrics endpoint at `:8889` (only `:8888` Prometheus self-metrics) — the OTel metric pipeline is not wired end-to-end.

### F-24: Prometheus has only 24 h retention and no remote_write
**Where:** `docker-compose.yml:138` (`--storage.tsdb.retention.time=24h`)
**Impact:** 24 h is fine for dev; in CI a flaky test from yesterday is unprovable. No retention configured for Loki (which by default keeps ~3 weeks under `tsdb` for filesystem — fine for dev).
**Recommendation:** Bump dev to 7d (`--storage.tsdb.retention.time=7d`) and document that prod uses Grafana Cloud (`OTLP_TARGET=grafana_cloud`).

### F-25: No `docker-compose.override.yml` pattern
**Where:** Only one compose file exists; no override layer.
**Impact:** Devs who want to swap `OPENAI_API_KEY` between Ollama and OpenAI, or run only data-plane services, must edit the canonical compose file (risk of accidental commit) or live with `OPENAI_API_KEY=...` env at every command.
**Recommendation:** Ship `docker-compose.override.yml.example` and document the pattern. Also a `docker-compose.minimal.yml` (just postgres+redis+control-plane+web) for new contributors.

### F-26: CI builds Docker images but does not push, tag, or scan
**Where:** `.github/workflows/ci.yml:50-62`
**Impact:** `docker build -t nexis/X:ci` runs on every PR. The images are thrown away. No SBOM, no Trivy/grype, no `docker push` to ECR even for `main`. Phase 7 cutover has no artifact provenance.
**Recommendation:**
- Add a `release` job triggered on tag `v*` that logs in to ECR via OIDC (`aws-actions/configure-aws-credentials@v4`), builds with `--platform linux/amd64,linux/arm64`, pushes with semver + git SHA tags.
- Add `aquasecurity/trivy-action` on PRs (fail on HIGH/CRITICAL).
- Cache buildx layers across CI runs via `actions/cache` or `--cache-from=type=gha`.
- Generate SBOMs with `anchore/sbom-action`.

---

## Low (nice-to-have)

### F-27: No `network: external: true` story for sharing observability with other stacks
**Where:** `docker-compose.yml:342-344`
**Impact:** Other compose stacks on the same machine can't share Prometheus/Loki.
**Recommendation:** Document, don't fix.

### F-28: Hardcoded `mkcert` TLS in `infra/tls/`
**Where:** `infra/tls/cert.pem`, `key.pem` (gitignored — good, but file presence implies a workflow nobody documents)
**Impact:** No script regenerates them; new contributors hit "file not found" if compose later mounts them.
**Recommendation:** Add `make tls` target invoking `mkcert -install && mkcert -cert-file infra/tls/cert.pem -key-file infra/tls/key.pem localhost`.

### F-29: Terraform staging/dev are not symmetric with prod
**Where:** `infra/terraform/envs/{dev,staging,prod}/main.tf`
**Impact:** Prod has `multi_region = true` on `kms_data`; staging does not. Backup retention `35` prod vs `14` staging — fine, but inconsistencies are hidden across separate `main.tf` files. There is no `ecs_cluster`, `alb`, `route53`, `acm`, `ecr`, `cloudwatch_log_group`, `secretsmanager_secret`, `iam_oidc` modules — the Phase 7 cutover would require ~12 more modules.
**Recommendation:** Adopt a single `main.tf` per env that calls one `module "platform"`; per-env differences live entirely in `terraform.tfvars`. Add stub modules for the missing pieces now so the topology is reviewable before Phase 7 build-out.

### F-30: No remote state lock backend visible
**Where:** `infra/terraform/envs/*/backend.tf` (referenced; not read but `backend.tf` presence implies S3 + DynamoDB lock).
**Impact:** Audit didn't verify — recommend confirming `dynamodb_table` is set for state lock, otherwise concurrent applies will corrupt state.
**Recommendation:** Verify `infra/terraform/envs/*/backend.tf` declares both `bucket` and `dynamodb_table`.

### F-31: No `.tool-versions` / `asdf` / `mise` config
**Where:** Repo root
**Impact:** `.nvmrc` pins Node only. Go 1.25, pnpm 9.12.0, Python 3.12 are pinned only inside Dockerfiles. A native `go test` on a contributor with Go 1.22 silently breaks.
**Recommendation:** Add `.tool-versions`:
```
nodejs 22.x
golang 1.25.0
python 3.12
pnpm 9.12.0
```

### F-32: README is 1.4 KB — no boot story documented
**Where:** `README.md` (1.4 KB)
**Impact:** A new contributor has no `git clone && make bootstrap && docker compose up && open http://localhost:3000` recipe. The reproducibility goal stated in the audit scope is unmet without it. (The audit did not need to read README content per the brief — observation is the file size.)
**Recommendation:** Expand README with a "Quick start" section: prerequisites (Docker Desktop, mkcert, direnv), bootstrap, env, ports, health checks, and troubleshooting.

### F-33: `docker compose down` deletes Temporal data — no `temporal_data` volume
**Where:** `docker-compose.yml:60-77` (no volume for Temporal)
**Impact:** Temporal stores history in Postgres, which *is* volumed — so this is mostly fine. But `temporal-ui` browser settings/preferences are wiped on every restart.
**Recommendation:** Minor — add a volume for `temporal-ui` if it ever stores state. Otherwise document.

### F-34: `loki-config.yaml` writes chunks to `/tmp/loki`
**Where:** `infra/observability/loki-config.yaml:11`
**Impact:** Logs disappear on `docker compose down` because `/tmp` is inside the container's ephemeral fs (no volume mounted on the loki service in compose). Same for `tempo-config.yaml:24` (`/tmp/tempo/blocks`).
**Recommendation:** Add `loki_data` and `tempo_data` named volumes, mount at `/tmp/loki` and `/tmp/tempo` respectively.

---

## Phase 7 readiness checklist

The Terraform tree has 6 modules (`vpc`, `rds`, `kms-key`, `s3-bucket`, `iam-task-role`, `ecs-service`) and 3 envs (`dev`, `staging`, `prod`). The following pieces are required for a Phase 7 cutover and are **missing**:

- [ ] **ECR repos** — no `aws_ecr_repository` for `control-plane`, `validator`, `gitops`, `web`, `causal-inference`
- [ ] **ECS cluster** — no `aws_ecs_cluster` resource anywhere
- [ ] **ALB + ACM + listener rules** — no module for `api.nexis.dev` or `app.nexis.dev` ingress
- [ ] **Route 53 hosted zone + records** — `betterstack/checks.yml` references `api.nexis.dev` and `app.nexis.dev` but nothing provisions them
- [ ] **Secrets Manager secret resources** — `secrets/factory.go` reads them but Terraform never creates them (`SECRETS_PREFIX=nexis-<env>/control-plane/`). The bootstrap fails on first apply because the secrets don't exist yet.
- [ ] **CloudWatch log groups** — `ecs-service` module references `var.log_group_name` but no module creates the group
- [ ] **`aws_iam_openid_connect_provider`** for GHA → AWS OIDC role assumption (required by F-26 CI/CD push step)
- [ ] **`FatalIfLocalInCloud()` invoked at boot** (F-2)
- [ ] **Modal validator runner adapter wired** — `MODAL_APP_URL` / `MODAL_TOKEN` only in config; no Go adapter visible at `internal/adapter/validator/modal/`
- [ ] **Temporal Cloud mTLS dial** — env vars present (`TEMPORAL_CLOUD_TLS_CERT_PATH`, etc.) but no adapter switch in `cmd/server/main.go` for `cfg.TemporalCloud == true`
- [ ] **Neo4j AuraDB connection string** — local compose uses `bolt://neo4j:7687`; no Phase 7 selector (`NEO4J_PROVIDER=auradb` / `+s://...` URI handling)
- [ ] **KMS key policies** — `kms-key` module exists but didn't verify it grants ECS task role `kms:Decrypt`/`kms:Encrypt` on the data key
- [ ] **RDS proxy + reader endpoint** — Multi-AZ is configured but no `aws_db_proxy` for connection pooling. Fargate cold-starts will exhaust Postgres connections fast.
- [ ] **VPC interface endpoints** — no `aws_vpc_endpoint` for `secretsmanager`, `kms`, `s3`, `ecr.api`, `ecr.dkr`, `logs`. Without these every ECS task egresses through NAT (cost + bandwidth + Phase 7 budget exposure).
- [ ] **NAT gateway count = 3 in prod** — fine for HA but $135/month just for NAT. Consider VPC endpoints (above) to drop traffic.
- [ ] **WAF / Shield association on ALB** — not configured
- [ ] **GuardDuty + Security Hub + Config rules** — not configured
- [ ] **Backup vault for cross-region snapshot copy** — RDS automated backups exist (`backup_retention_period: 35` in prod) but no cross-region copy
- [ ] **Cost alarms** — no `aws_budgets_budget` for the env-level spend
- [ ] **Service modules not invoked** — `module "ecs-service"` for each of `control-plane`, `validator`, `gitops`, `web` is never instantiated in `envs/prod/main.tf` (the prod main only declares VPC, KMS, S3, RDS)
- [ ] **`aws_lb_target_group_attachment` / listener rule routing** — needs ALB module first
- [ ] **Blue/green deployment** — `ecs-service` uses `deployment_controller { type = "ECS" }`; for blue/green it must be `"CODE_DEPLOY"` + a `aws_codedeploy_app` resource
- [ ] **`HEALTHCHECK` in distroless containers** — see F-22; without a healthcheck binary, ECS target-group health is the only liveness signal
- [ ] **GitHub Actions OIDC role + push step** — see F-26
- [ ] **`docker-compose.prod.yml`** — there is no overlay that demonstrates what changes between local and Fargate task defs (a generator/translator script would be ideal)
- [ ] **Secret rotation lambda for `MASTER_KEY`** — encrypted-at-rest secrets in DB need re-encryption when MASTER_KEY rotates; no rotation cron visible
- [ ] **Audit anchor cron** — `AUDIT_ANCHOR_CRON` is in config but no EventBridge schedule / Lambda exists
