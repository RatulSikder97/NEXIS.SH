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

Once healthy:

| URL | What |
|---|---|
| http://localhost:3000 | Landing + console |
| http://localhost:8080 | Control plane API |
| http://localhost:3001 | Grafana (anonymous Admin) |
| http://localhost:8025 | MailHog |
| http://localhost:9001 | MinIO console (nexis / nexis_dev_password) |
| http://localhost:8233 | Temporal UI |
| http://localhost:7474 | Neo4j browser (neo4j / nexis_dev_password) |

Phase 2 adds Caddy + mkcert local TLS so the URLs become `https://*.nexis.local` — needed for WorkOS auth, passkeys, and `Secure` cookies.

## Repo layout

- `apps/web/` — Next.js 16.2.2 landing + console (light mode default)
- `services/control-plane/` — Go API + Temporal worker + LLM gateway
- `services/validator/` — sandbox dispatcher
- `services/gitops/` — GitHub App PR handler
- `packages/db/` — shared Drizzle schema (TS) + migrations
- `packages/ui/` — shared UI primitives
- `infra/observability/` — Grafana / Loki / Tempo / Prometheus / OTel collector configs
