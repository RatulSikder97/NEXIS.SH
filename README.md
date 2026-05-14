# NEXIS

Multi-tenant SaaS for autonomous fault recovery via 9 AI agents. See [`docs/PROJECT_PLAN.md`](docs/PROJECT_PLAN.md) for the locked spec.

## Local development

Prereqs: Docker Desktop (≥ 8 GiB / 4 CPU), Node 22, Go 1.22, pnpm, direnv, Ollama with `llama3.1:8b` pulled.

```bash
# one-time setup
cp .env.example .env.local && direnv allow
pnpm install

# bring everything up
docker compose up --build -d
```

### Compose profiles

The stack is split into three layers so devs can boot only what they need:

| Profile | Services | When to use |
|---|---|---|
| **default** (unprofiled) | postgres, redis, minio, minio-init, mailhog, temporal, temporal-ui, neo4j, control-plane, web | The bare minimum to log in, run a workflow, and exercise the dashboard. Boots in ~30 s on a warm cache. |
| `observability` | prometheus, grafana, loki, tempo, otel-collector | Opt in when you actually need to inspect traces, dashboards, or log queries. Adds ~1.5 GiB RAM. |
| `agents` | validator, gitops, causal-inference | Opt in when exercising the recovery pipeline end-to-end (PR creation, sandbox tests, causal inference). Adds ~1 GiB RAM and pulls a Python image. |

```bash
# slim — default only
docker compose up -d

# + observability
docker compose --profile observability up -d

# everything
docker compose --profile observability --profile agents up -d
```

The control-plane tolerates each opt-in service being absent and logs a warning at startup — there are no hard `depends_on` edges across profiles.

### Reachable URLs

| URL | What | Profile |
|---|---|---|
| http://localhost:3000 | Landing + console | default |
| http://localhost:8080 | Control plane API | default |
| http://localhost:8025 | MailHog | default |
| http://localhost:9001 | MinIO console (minioadmin / minioadmin) | default |
| http://localhost:8233 | Temporal UI | default |
| http://localhost:7474 | Neo4j browser (neo4j / nexis_dev_password) | default |
| http://localhost:3030 | Grafana (anonymous Admin) | observability |
| http://localhost:9090 | Prometheus | observability |

Phase 2 adds Caddy + mkcert local TLS so the URLs become `https://*.nexis.local` — needed for WorkOS auth, passkeys, and `Secure` cookies.

### Database backup / restore

The Postgres volume (`postgres_data`) is wiped by `docker compose down -v`. Two helper scripts make logical dumps cheap to take and trivial to replay:

```bash
# capture a timestamped dump in ./backups/nexis_<ts>.sql.gz (keeps last 30)
./scripts/backup_db.sh

# restore a specific dump
./scripts/restore_db.sh backups/nexis_20260514_021500.sql.gz

# or just restore the most recent one
./scripts/restore_db.sh latest
```

Both scripts go through `docker compose exec postgres` so no `pg_dump`/`psql` is required on the host.

Recommended cron (daily at 02:00, log to `backups/cron.log`):

```cron
0 2 * * * cd /path/to/web_app && ./scripts/backup_db.sh >>backups/cron.log 2>&1
```

## Repo layout

- `apps/web/` — Next.js 16.2.2 landing + console (light mode default)
- `services/control-plane/` — Go API + Temporal worker + LLM gateway
- `services/validator/` — sandbox dispatcher
- `services/gitops/` — GitHub App PR handler
- `packages/db/` — shared Drizzle schema (TS) + migrations
- `packages/ui/` — shared UI primitives
- `infra/observability/` — Grafana / Loki / Tempo / Prometheus / OTel collector configs
