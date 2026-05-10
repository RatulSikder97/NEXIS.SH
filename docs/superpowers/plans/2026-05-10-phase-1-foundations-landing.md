# Phase 1 — Foundations & Landing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the NEXIS monorepo locally with `docker compose up` bringing the entire stack healthy in < 60s, the LIGHT-mode landing page rendering at `http://localhost:3000`, and the LLM `Provider` abstraction switchable between OpenAI and local Ollama via env var — per `docs/PROJECT_PLAN.md` §4.1, §3.6, §1, §5 Phase 1.

**Architecture:** pnpm + turborepo monorepo. Three Go services (control-plane, validator, gitops) under `services/`, shared web app under `apps/web/`, shared packages under `packages/{ui,db}`, infra config under `infra/`, all wired via root `docker-compose.yml`. **Plain HTTP via host port mappings** for Phase 1 — Caddy + mkcert + `*.nexis.local` HTTPS lands in Phase 2 alongside WorkOS auth, where `Secure` cookies, OAuth callbacks, and passkey enrollment make HTTPS a hard requirement. Existing `site/` (Next 16.2.2) and `backend/` (Go 1.22 + Chi) are salvaged in place into the new layout; everything else moves to `_legacy/`.

**Tech Stack:** Node 22 + pnpm + turborepo + Next.js 16.2.2 + React 19 + Tailwind v4 + shadcn/ui + Framer Motion 12 + next-themes; Go 1.22 + Chi v5 + pgx v5 + sqlc + golang-migrate; Postgres 16 + pgvector + Redis 7 + MinIO + MailHog + Temporal dev server + Neo4j community + OTel collector + Grafana + Loki + Tempo + Prometheus; Drizzle ORM in the web app for the waitlist + audit_log tables.

---

## Salvage / Migration Map

These existing paths are MOVED, not deleted. Each is mapped to its destination:

| Source (existing) | Destination | Action |
|---|---|---|
| `site/` (Next 16.2.2) | `apps/web/` | `mv` then restructure (rebrand to light tokens, add waitlist API, add LLM diag proxy) |
| `backend/` (Go 1.22 + Chi) | `services/control-plane/` | `mv` then restructure into `internal/{domain,usecase,adapter,transport,platform,workflow}` per §3.6 |
| `internal/` (top-level Go) | `_legacy/internal-old/` | preserve only (older skeleton, superseded) |
| `scripts/verify-stack.py` | `_legacy/scripts/` | preserve (refers to legacy 9-service map) |
| `docs/IMPLEMENTATION_PLAN.md` | `_legacy/docs/` | preserve (older 9-service plan) |
| `docs/microservice-orchestration.md` | `_legacy/docs/` | preserve |
| `docs/production-architecture.md` | `_legacy/docs/` | preserve |
| `comprehensive-pan.md` | `_legacy/` | preserve |
| `initial_plan.md` | `_legacy/` | preserve |
| `index.html` | `_legacy/` | preserve (old SPA prototype) |
| `plantt.html` | `_legacy/` | preserve |
| `docker-compose.yml` | `_legacy/docker-compose.old.yml` | preserve (full rewrite incoming) |
| `.dockerignore` | `_legacy/.dockerignore.old` | preserve (rewrite at root) |
| `README.md` | `_legacy/README.old.md` | preserve (full rewrite) |
| `site/.git` | `_legacy/site-history.git.tar.gz` | tar-archive (history not lost; new monorepo .git starts fresh) |
| `docs/PROJECT_PLAN.md` | KEEP IN PLACE | the locked spec — never moved |
| `docs/superpowers/` | KEEP IN PLACE | this plan lives here |

`_legacy/` is added to `.gitignore` after `git init` so it never enters source control.

---

## File Structure (target monorepo after Phase 1)

```
web_app/
├── .git/                                    # initialized at Task 0.4
├── .github/workflows/ci.yml                 # T9.1 — lint+typecheck+test+docker-build on PR
├── .gitignore                               # T1.1
├── .dockerignore                            # T1.1
├── .env.example                             # T1.1
├── .envrc                                   # T1.1 — direnv loader (sources .env.local)
├── .nvmrc                                   # T1.1 — pin Node 22
├── README.md                                # T1.1 — replaces old
├── package.json                             # T1.2 — workspace root
├── pnpm-workspace.yaml                      # T1.2
├── turbo.json                               # T1.2
├── docker-compose.yml                       # T2.1 — full infra + app services
│                                            # (Caddyfile + infra/tls — Phase 2)
├── _legacy/                                 # T0.3 — stashed prior work, gitignored
├── docs/
│   ├── PROJECT_PLAN.md                      # source of truth (kept)
│   └── superpowers/plans/                   # plans live here
├── infra/
│   └── observability/
│       ├── otel-collector.yaml              # T2.2
│       ├── prometheus.yml                   # T2.2
│       ├── loki-config.yaml                 # T2.2
│       ├── tempo-config.yaml                # T2.2
│       └── grafana/
│           ├── datasources.yaml             # T2.2
│           └── dashboards/.gitkeep          # T2.2
├── apps/
│   └── web/                                 # T6.1 — moved from site/
│       ├── package.json                     # T6.1 — adds drizzle-orm, postgres, next-themes, shadcn deps
│       ├── next.config.ts                   # T6.1
│       ├── tsconfig.json                    # T6.1
│       ├── tailwind.config.ts               # T6.2 — light token palette
│       ├── postcss.config.mjs               # kept
│       ├── components.json                  # T6.3 — shadcn config
│       ├── drizzle.config.ts                # T8.1
│       ├── Dockerfile                       # T6.4
│       ├── app/
│       │   ├── layout.tsx                   # T6.5 — wraps ThemeProvider + light defaults
│       │   ├── page.tsx                     # T7.0 — composes 9 landing sections
│       │   ├── globals.css                  # T6.2 — REWRITTEN to light tokens per §4.1
│       │   ├── not-found.tsx                # kept
│       │   ├── robots.ts                    # kept
│       │   ├── sitemap.ts                   # kept
│       │   └── api/
│       │       ├── waitlist/route.ts        # T8.2 — POST persists row
│       │       └── _diag/llm/route.ts       # T5.4 — proxies to control-plane /v1/_diag/llm
│       ├── components/
│       │   ├── ThemeAwareLogo.tsx         # T6.6
│       │   ├── ThemeProvider.tsx           # T6.5 — next-themes wrapper
│       │   ├── ThemeToggle.tsx             # T6.5
│       │   ├── ui/                          # shadcn primitives + existing UI (Button, Badge, Logo, etc.)
│       │   ├── layout/
│       │   │   ├── Navbar.tsx                # T7.1 — sticky, scroll-aware, light (rewrite existing)
│       │   │   └── Footer.tsx                # T7.9 — re-skinned existing
│       │   └── sections/
│       │       ├── Hero.tsx                  # T7.2 — re-skinned existing
│       │       ├── TrustedStrip.tsx          # T7.3 — NEW
│       │       ├── Problem.tsx               # T7.3 — re-skinned existing
│       │       ├── HowItWorks.tsx            # T7.4 — re-skinned existing
│       │       ├── Agents.tsx                # T7.5 — re-skinned existing (3×3 grid)
│       │       ├── LiveDemoEmbed.tsx         # T7.6 — NEW (placeholder)
│       │       ├── Metrics.tsx               # T7.7 — re-skinned existing
│       │       └── CTA.tsx                   # T7.8 — re-skinned existing
│       ├── lib/
│       │   ├── db.ts                        # T8.1 — drizzle client (pg)
│       │   ├── tokens.ts                    # T6.2 — design tokens TS export
│       │   └── content.ts                   # kept (already present)
│       ├── public/
│       │   ├── logo.svg                     # kept (dark variant)
│       │   ├── logo-light.svg               # T6.6 — NEW per §4.2
│       │   └── favicon.ico                  # kept
│       └── tests/
│           └── waitlist.test.ts             # T8.2 — Vitest API route test
├── packages/
│   ├── ui/                                  # T6.7 — placeholder for future shared components
│   │   └── package.json                     # T6.7
│   └── db/                                  # T8.1
│       ├── package.json                     # T8.1
│       ├── schema.ts                        # T8.1 — drizzle schema: organizations, users, audit_log, waitlist
│       └── migrations/                      # T8.1 — drizzle-generated SQL migrations
└── services/
    ├── control-plane/                       # T3.1 — moved from backend/
    │   ├── go.mod                           # T3.1
    │   ├── cmd/server/main.go               # T3.1
    │   ├── Dockerfile                       # T3.1
    │   ├── Makefile                         # T3.1
    │   ├── sqlc.yaml                        # T8.3
    │   ├── .arch.yaml                       # T3.6 — go-arch-lint rules per §3.7
    │   ├── migrations/                      # T8.3 — golang-migrate SQL files
    │   ├── internal/
    │   │   ├── domain/                      # T3.2 — pure types + ports
    │   │   ├── usecase/                     # T3.2 — placeholder
    │   │   ├── adapter/
    │   │   │   ├── llm/                     # T5.1-T5.3 — Provider + OpenAI + Ollama
    │   │   │   └── repo/                    # T8.3 — sqlc-generated query code
    │   │   ├── transport/
    │   │   │   └── http/
    │   │   │       ├── server.go            # T3.4 — Chi router
    │   │   │       ├── middleware/          # T3.4 — RequestID, Logger, Recoverer, CORS stubs
    │   │   │       ├── handler/
    │   │   │       │   ├── healthz.go       # T3.4
    │   │   │       │   └── llm_diag.go      # T5.4
    │   │   │       └── dto/                 # T5.4
    │   │   ├── platform/
    │   │   │   ├── config/config.go         # T3.3 — env loader
    │   │   │   ├── db/db.go                 # T3.3 — pgx pool stub
    │   │   │   └── observability/otel.go    # T3.3 — OTel SDK init stub
    │   │   └── workflow/                    # T3.2 — placeholder
    │   └── tests/integration/healthz_test.go # T3.4
    ├── validator/                           # T4.1 — new minimal
    │   ├── go.mod
    │   ├── cmd/server/main.go
    │   ├── Dockerfile
    │   └── internal/transport/http/server.go
    └── gitops/                              # T4.1 — new minimal
        ├── go.mod
        ├── cmd/server/main.go
        ├── Dockerfile
        └── internal/transport/http/server.go
```

---

## Stage 0 — Pre-flight (tools, legacy stash, git init)

### Task 0.1: Install missing dev tooling

**Files:** none (system install)

- [ ] **Step 1: Verify what's missing**

```bash
which go pnpm mkcert sqlc buf migrate direnv 2>&1 | grep -E "not found|^/"
```

Expected: prints `not found` lines for go, pnpm, mkcert, sqlc, buf, migrate, direnv (if any are already installed they'll print a path instead — skip those).

- [ ] **Step 2: Install via Homebrew**

```bash
brew install go pnpm mkcert sqlc bufbuild/buf/buf golang-migrate direnv nss
```

Note: `nss` is required by `mkcert` for Firefox cert trust on macOS. `bufbuild/buf/buf` is the official tap.

- [ ] **Step 3: Verify all tools resolve**

```bash
go version && pnpm --version && mkcert -version && sqlc version && buf --version && migrate -version 2>&1 && direnv --version
```

Expected: each prints a version. If `pnpm` still fails because of the `_lazy_nvm` zsh wrapper, run `unset -f pnpm` in the same shell session before re-checking, and add an alias hook in `.envrc` later (T1.1).

- [ ] **Step 4: Pull the default Ollama model required by Phase 1 acceptance**

```bash
ollama pull llama3.1:8b
```

Expected: download completes (~4.7 GB; ~2-5 min on broadband). Then:

```bash
curl -s http://localhost:11434/api/tags | python3 -c "import sys,json; print([m['name'] for m in json.load(sys.stdin)['models']])"
```

Expected output: `['llama3.1:8b']`

> **Optional (skip if disk < 20 GB free):** `ollama pull qwen2.5-coder:14b` — used by Phase 5 for code synthesis comparisons; not required for Phase 1.

- [ ] **Step 5: Confirm Docker Desktop has ≥ 8 GB RAM allocated**

```bash
docker system info 2>/dev/null | grep -E "Total Memory|CPUs"
```

Expected: `Total Memory: 8GiB` or higher, `CPUs: 4` or higher. If lower, open Docker Desktop → Settings → Resources → bump to ≥ 8 GiB / 4 CPU before continuing — Phase 1 stack needs the headroom.

---

### Task 0.2: Inventory existing work that will be touched

**Files:** none (inspection only)

- [ ] **Step 1: Confirm site/ is on Next 16.2.2 with the expected stack**

```bash
grep -E '"next"|"react"|"tailwindcss"|"framer-motion"' /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/site/package.json
```

Expected: `"next": "16.2.2"`, `"react": "19.2.4"`, `"tailwindcss": "^4"`, `"framer-motion": "^12.38.0"`. If any drift, STOP and reconcile with the user before continuing — the plan assumes these versions.

- [ ] **Step 2: Confirm backend/ is on Go 1.22 + Chi v5 + pgx v5**

```bash
cat /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/backend/go.mod
```

Expected: `go 1.22`, `github.com/go-chi/chi/v5`, `github.com/jackc/pgx/v5`. If anything older, STOP and reconcile.

- [ ] **Step 3: Note site/'s own .git so we preserve its history**

```bash
ls -la /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/site/.git/HEAD 2>&1 && \
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/site && git log --oneline | head -5 && cd -
```

Expected: HEAD exists, recent commit log prints. Capture that log to confirm history is real (not a stub `.git`).

---

### Task 0.3: Stash legacy files into `_legacy/` (preserve, don't delete)

**Files:**
- Move: `site/.git`, all listed in the salvage table → `_legacy/`

- [ ] **Step 1: Create `_legacy/` skeleton**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
mkdir -p _legacy/{docs,scripts,internal-old,site}
```

- [ ] **Step 2: Archive site/.git so the new monorepo can start a fresh git history without losing the old**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/site && \
tar czf /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/_legacy/site-history.git.tar.gz .git && \
rm -rf .git && cd -
```

Expected: `_legacy/site-history.git.tar.gz` exists; `site/.git` removed.

- [ ] **Step 3: Move planning + legacy artifacts**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && \
mv internal _legacy/internal-old && \
mv scripts/verify-stack.py _legacy/scripts/ && rmdir scripts && \
mv docs/IMPLEMENTATION_PLAN.md docs/microservice-orchestration.md docs/production-architecture.md _legacy/docs/ && \
mv comprehensive-pan.md initial_plan.md index.html plantt.html _legacy/ && \
mv docker-compose.yml _legacy/docker-compose.old.yml && \
mv .dockerignore _legacy/.dockerignore.old && \
mv README.md _legacy/README.old.md
```

- [ ] **Step 4: Verify only the expected files remain at root**

```bash
ls -la /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/
```

Expected (excluding hidden + `.claude/.superpowers`): `backend/`, `site/`, `docs/`, `_legacy/`. No old README, compose, planning files at root. Dotfiles (`.claude`, `.superpowers`, `.DS_Store`) are fine.

---

### Task 0.4: `git init` the monorepo

**Files:**
- Create: `.git/`

- [ ] **Step 1: Initialize repo, set default branch to main**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && \
git init -b main && \
git config user.name "$(git config --global user.name || echo 'Ratul Sikder')" && \
git config user.email "$(git config --global user.email || echo 'ratulsikder.research@gmail.com')"
```

- [ ] **Step 2: Verify**

```bash
git -C /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app status
```

Expected: `On branch main`, lots of untracked files. Don't commit yet — `.gitignore` lands in T1.1 first.

---

### Task 0.5: ~~mkcert + /etc/hosts~~ — DEFERRED to Phase 2

> **Phase 1 runs on plain HTTP** (`http://localhost:3000` for web, `http://localhost:8080` for API, etc.). HTTPS + custom hostnames land in Phase 2 alongside WorkOS auth, where `Secure` cookies, OAuth callbacks, and passkey/WebAuthn enrollment make HTTPS a hard requirement.
>
> `mkcert` and `nss` are still installed via TC1 (cheap, no harm) — they're staged for Phase 2.

---

## Stage 1 — Repo skeleton (root tooling files)

### Task 1.1: Root dotfiles + README

**Files:**
- Create: `.gitignore`, `.dockerignore`, `.env.example`, `.envrc`, `.nvmrc`, `README.md`

- [ ] **Step 1: `.gitignore`**

```gitignore
# build artifacts
node_modules/
.next/
out/
dist/
.turbo/
*.log

# go
/services/*/bin/
/services/*/coverage.out
/services/*/.air.tmp/

# env / secrets
.env
.env.local
.env.*.local
.envrc.local

# OS / editor
.DS_Store
.idea/
.vscode/
*.swp

# legacy + local TLS
_legacy/
infra/tls/cert.pem
infra/tls/key.pem
infra/tls/*.crt
infra/tls/*.key

# docker volumes
.docker-data/
```

- [ ] **Step 2: `.dockerignore`** (build context hygiene — every Dockerfile in the monorepo benefits)

```
.git
node_modules
**/node_modules
.next
**/.next
.turbo
**/.turbo
_legacy
docs
.env
.env.local
.env.*.local
infra/tls
*.md
```

- [ ] **Step 3: `.env.example`** (committed; real values go in `.env.local`)

```bash
# === LLM ===
LLM_PROVIDER=openai                              # openai | ollama
OPENAI_API_KEY=sk-replace-me
OPENAI_MODEL_SYNTHESIS=gpt-4o
OPENAI_MODEL_CHEAP=gpt-4o-mini
OLLAMA_BASE_URL=http://host.docker.internal:11434
OLLAMA_MODEL_GENERAL=llama3.1:8b
OLLAMA_MODEL_CODE=qwen2.5-coder:14b              # optional

# === Database ===
DATABASE_URL=postgres://nexis:nexis_dev_password@postgres:5432/nexis?sslmode=disable

# === Service URLs (inside docker network) ===
CONTROL_PLANE_URL=http://control-plane:8080
VALIDATOR_URL=http://validator:8081
GITOPS_URL=http://gitops:8082

# === Web ===
NEXT_PUBLIC_APP_URL=http://localhost:3000
NEXT_PUBLIC_API_URL=http://localhost:8080

# === Observability ===
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
OTEL_SERVICE_NAME=control-plane
LOG_LEVEL=info
```

- [ ] **Step 4: `.envrc`** (direnv loader — auto-loads `.env.local` per shell `cd`)

```bash
# direnv loader — install direnv + run `direnv allow` once per fresh clone
[ -f .env.local ] && dotenv .env.local

# Workaround for nvm lazy-loader colliding with pnpm in zsh
unalias pnpm 2>/dev/null
unset -f pnpm 2>/dev/null

# Use repo-pinned Node via nvmrc if nvm is installed
[ -s "$HOME/.nvm/nvm.sh" ] && \. "$HOME/.nvm/nvm.sh" && nvm use 2>/dev/null
```

- [ ] **Step 5: `.nvmrc`**

```
22
```

- [ ] **Step 6: `README.md`** (root)

```markdown
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

> Phase 2 adds Caddy + mkcert local TLS so the URLs become `https://*.nexis.local` — needed for WorkOS auth, passkeys, and `Secure` cookies.

## Repo layout

- `apps/web/` — Next.js 16.2.2 landing + console (light mode default)
- `services/control-plane/` — Go API + Temporal worker + LLM gateway
- `services/validator/` — sandbox dispatcher
- `services/gitops/` — GitHub App PR handler
- `packages/db/` — shared Drizzle schema (TS) + migrations
- `packages/ui/` — shared UI primitives
- `infra/observability/` — Grafana / Loki / Tempo / Prometheus / OTel collector configs
- `infra/tls/` — local mkcert certs (gitignored)
```
```

---

### Task 1.2: pnpm workspace + turborepo + root package.json

**Files:**
- Create: `package.json`, `pnpm-workspace.yaml`, `turbo.json`

- [ ] **Step 1: `package.json` (root)**

```json
{
  "name": "nexis-eco",
  "version": "0.0.0",
  "private": true,
  "packageManager": "pnpm@9.12.0",
  "engines": {
    "node": ">=22"
  },
  "scripts": {
    "build": "turbo run build",
    "dev": "turbo run dev --parallel",
    "lint": "turbo run lint",
    "typecheck": "turbo run typecheck",
    "test": "turbo run test",
    "format": "prettier --write \"**/*.{ts,tsx,md,json,yml,yaml}\""
  },
  "devDependencies": {
    "prettier": "^3.3.3",
    "turbo": "^2.3.3"
  }
}
```

- [ ] **Step 2: `pnpm-workspace.yaml`**

```yaml
packages:
  - "apps/*"
  - "packages/*"
```

- [ ] **Step 3: `turbo.json`**

```json
{
  "$schema": "https://turbo.build/schema.json",
  "globalDependencies": [".env.example", ".env.local"],
  "ui": "stream",
  "tasks": {
    "build": {
      "dependsOn": ["^build"],
      "outputs": [".next/**", "!.next/cache/**", "dist/**"]
    },
    "dev": {
      "cache": false,
      "persistent": true
    },
    "lint": { "dependsOn": ["^lint"] },
    "typecheck": { "dependsOn": ["^typecheck"] },
    "test": { "dependsOn": ["^build"] }
  }
}
```

- [ ] **Step 4: Verify pnpm install works on the empty workspace**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && pnpm install
```

Expected: `Lockfile is up to date, resolution step is skipped` then `Done in <Xs>`. A `pnpm-lock.yaml` is created at root.

---

## Stage 2 — Infra docker-compose (no app services yet)

### Task 2.1: Write `docker-compose.yml` with infra services only

**Files:**
- Create: `docker-compose.yml`

> **Note:** App services (`web`, `control-plane`, `validator`, `gitops`) are added in T7.0 + T9.0 once each has a Dockerfile. Caddy is added in T7.1.

- [ ] **Step 1: Write the infra-only compose file**

```yaml
# docker-compose.yml — infra services. App services + caddy added in later tasks.
name: nexis

services:
  postgres:
    image: pgvector/pgvector:pg16
    environment:
      POSTGRES_DB: nexis
      POSTGRES_USER: nexis
      POSTGRES_PASSWORD: nexis_dev_password
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U nexis -d nexis"]
      interval: 5s
      timeout: 3s
      retries: 10
    networks: [nexis]

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 10
    networks: [nexis]

  minio:
    image: minio/minio:latest
    command: server /data --console-address ":9001"
    environment:
      MINIO_ROOT_USER: nexis
      MINIO_ROOT_PASSWORD: nexis_dev_password
    ports:
      - "9000:9000"
      - "9001:9001"
    volumes:
      - minio_data:/data
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9000/minio/health/live"]
      interval: 10s
      timeout: 3s
      retries: 5
    networks: [nexis]

  mailhog:
    image: mailhog/mailhog:latest
    ports:
      - "1025:1025"   # SMTP
      - "8025:8025"   # web UI
    networks: [nexis]

  temporal:
    image: temporalio/auto-setup:1.25.2
    environment:
      DB: postgres12
      DB_PORT: "5432"
      POSTGRES_USER: nexis
      POSTGRES_PWD: nexis_dev_password
      POSTGRES_SEEDS: postgres
      DYNAMIC_CONFIG_FILE_PATH: config/dynamicconfig/development-sql.yaml
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "7233:7233"
    healthcheck:
      test: ["CMD-SHELL", "tctl --address temporal:7233 cluster health 2>/dev/null | grep -q SERVING || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 12
    networks: [nexis]

  temporal-ui:
    image: temporalio/ui:2.31.2
    environment:
      TEMPORAL_ADDRESS: temporal:7233
      TEMPORAL_CORS_ORIGINS: http://localhost:8233
    depends_on:
      temporal:
        condition: service_healthy
    ports:
      - "8233:8080"
    networks: [nexis]

  neo4j:
    image: neo4j:5-community
    environment:
      NEO4J_AUTH: neo4j/nexis_dev_password
      NEO4J_PLUGINS: '["apoc"]'
    ports:
      - "7474:7474"
      - "7687:7687"
    volumes:
      - neo4j_data:/data
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://localhost:7474 >/dev/null 2>&1 || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 10
    networks: [nexis]

  otel-collector:
    image: otel/opentelemetry-collector-contrib:0.115.0
    command: ["--config=/etc/otel-collector.yaml"]
    volumes:
      - ./infra/observability/otel-collector.yaml:/etc/otel-collector.yaml:ro
    ports:
      - "4317:4317"   # OTLP gRPC
      - "4318:4318"   # OTLP HTTP
    networks: [nexis]

  prometheus:
    image: prom/prometheus:v3.0.1
    command:
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--storage.tsdb.retention.time=24h"
    volumes:
      - ./infra/observability/prometheus.yml:/etc/prometheus/prometheus.yml:ro
    ports:
      - "9090:9090"
    networks: [nexis]

  loki:
    image: grafana/loki:3.3.2
    command: ["-config.file=/etc/loki/local-config.yaml"]
    volumes:
      - ./infra/observability/loki-config.yaml:/etc/loki/local-config.yaml:ro
    ports:
      - "3100:3100"
    networks: [nexis]

  tempo:
    image: grafana/tempo:2.7.0
    command: ["-config.file=/etc/tempo.yaml"]
    volumes:
      - ./infra/observability/tempo-config.yaml:/etc/tempo.yaml:ro
    ports:
      - "3200:3200"
    networks: [nexis]

  grafana:
    image: grafana/grafana:11.4.0
    environment:
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: Admin
      GF_SECURITY_ADMIN_USER: admin
      GF_SECURITY_ADMIN_PASSWORD: admin
    volumes:
      - ./infra/observability/grafana/datasources.yaml:/etc/grafana/provisioning/datasources/datasources.yaml:ro
      - grafana_data:/var/lib/grafana
    ports:
      - "3001:3000"
    depends_on:
      - prometheus
      - loki
      - tempo
    networks: [nexis]

networks:
  nexis:
    driver: bridge

volumes:
  postgres_data:
  minio_data:
  neo4j_data:
  grafana_data:
```

---

### Task 2.2: Write observability configs

**Files:**
- Create: `infra/observability/otel-collector.yaml`, `infra/observability/prometheus.yml`, `infra/observability/loki-config.yaml`, `infra/observability/tempo-config.yaml`, `infra/observability/grafana/datasources.yaml`

- [ ] **Step 1: `infra/observability/otel-collector.yaml`**

```yaml
receivers:
  otlp:
    protocols:
      grpc: { endpoint: 0.0.0.0:4317 }
      http: { endpoint: 0.0.0.0:4318 }

processors:
  batch: {}
  memory_limiter:
    check_interval: 5s
    limit_mib: 256

exporters:
  otlphttp/tempo:
    endpoint: http://tempo:4318
    tls: { insecure: true }
  loki:
    endpoint: http://loki:3100/loki/api/v1/push
  prometheusremotewrite:
    endpoint: http://prometheus:9090/api/v1/write
    tls: { insecure: true }
  debug:
    verbosity: basic

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [otlphttp/tempo]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [debug]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [loki, debug]
```

- [ ] **Step 2: `infra/observability/prometheus.yml`**

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: ["localhost:9090"]
  - job_name: otel-collector
    static_configs:
      - targets: ["otel-collector:8888"]
```

- [ ] **Step 3: `infra/observability/loki-config.yaml`** (single-binary dev mode)

```yaml
auth_enabled: false

server:
  http_listen_port: 3100
  grpc_listen_port: 9096

common:
  instance_addr: 127.0.0.1
  path_prefix: /tmp/loki
  storage:
    filesystem:
      chunks_directory: /tmp/loki/chunks
      rules_directory: /tmp/loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory

schema_config:
  configs:
    - from: 2020-10-24
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

limits_config:
  metric_aggregation_enabled: true
  allow_structured_metadata: true

ruler:
  alertmanager_url: http://localhost:9093
```

- [ ] **Step 4: `infra/observability/tempo-config.yaml`**

```yaml
server:
  http_listen_port: 3200

distributor:
  receivers:
    otlp:
      protocols:
        http:
          endpoint: 0.0.0.0:4318

ingester:
  trace_idle_period: 10s
  max_block_bytes: 1_000_000
  max_block_duration: 5m

compactor:
  compaction:
    block_retention: 24h

storage:
  trace:
    backend: local
    local:
      path: /tmp/tempo/blocks
    wal:
      path: /tmp/tempo/wal
```

- [ ] **Step 5: `infra/observability/grafana/datasources.yaml`**

```yaml
apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
  - name: Loki
    type: loki
    access: proxy
    url: http://loki:3100
  - name: Tempo
    type: tempo
    access: proxy
    url: http://tempo:3200
```

- [ ] **Step 6: `.gitkeep` for the dashboards dir**

```bash
mkdir -p /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/infra/observability/grafana/dashboards && \
touch /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/infra/observability/grafana/dashboards/.gitkeep
```

---

### Task 2.3: Verify infra-only stack comes up healthy

**Files:** none (verification)

- [ ] **Step 1: Bring up infra**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && docker compose up -d
```

- [ ] **Step 2: Wait for everything to settle and inspect health**

```bash
sleep 45 && docker compose ps --format "table {{.Service}}\t{{.Status}}"
```

Expected: every service is `running` or `healthy`. The Temporal `auto-setup` may take an extra ~30s on first boot — re-check after another 30s if it's still `starting`.

- [ ] **Step 3: Smoke test each endpoint**

```bash
curl -fsSI http://localhost:9000/minio/health/live | head -1     # MinIO → 200
curl -fsSI http://localhost:8025                                  | head -1     # MailHog UI → 200
curl -fsSI http://localhost:7474                                  | head -1     # Neo4j → 200
curl -fsSI http://localhost:9090/-/healthy                        | head -1     # Prometheus → 200
curl -fsSI http://localhost:3001/api/health                       | head -1     # Grafana → 200
curl -fsS  http://localhost:3100/ready                            ; echo        # Loki → "ready"
curl -fsS  http://localhost:3200/ready                            ; echo        # Tempo → "ready"
docker compose exec -T postgres psql -U nexis -d nexis -c "SELECT version();" | grep -q PostgreSQL  # Postgres alive
```

If any fails, `docker compose logs <service> --tail 80` and fix before continuing.

- [ ] **Step 4: Tear down (we'll bring it back up at T9.0)**

```bash
docker compose down
```

- [ ] **Step 5: Commit progress**

```bash
git -C /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app add -A && \
git -C /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app commit -m "chore: stage 0-2 — pre-flight + monorepo skeleton + infra docker-compose

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Stage 3 — Go services skeleton (control-plane, validator, gitops)

### Task 3.1: Move backend/ → services/control-plane/ and rebuild structure

**Files:**
- Move: `backend/*` → `services/control-plane/*`
- Create: `services/control-plane/cmd/server/main.go` (rewritten), `services/control-plane/Makefile`

- [ ] **Step 1: Move the Go module into the new layout**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && \
mkdir -p services && \
mv backend services/control-plane && \
ls services/control-plane/
```

- [ ] **Step 2: Inspect what cmd/ subcommands exist (the old Dockerfile expected `cmd/${SERVICE}`)**

```bash
ls /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/cmd/
```

Each existing subdirectory is a binary entrypoint from the old 9-service split. We collapse them into one `cmd/server/main.go` and stash the rest under `_legacy/` for reference (in case the old code is useful for the agent modules later):

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/cmd && \
mkdir -p /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/_legacy/control-plane-cmds && \
for d in *; do
  if [ "$d" != "server" ]; then mv "$d" /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/_legacy/control-plane-cmds/; fi
done && \
mkdir -p server && \
ls
```

Expected: only `server/` remains. Other subdirs preserved in `_legacy/control-plane-cmds/`.

- [ ] **Step 3: Update go.mod module path to match the new home**

Read first:

```bash
cat /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/go.mod
```

Then rewrite:

```
module github.com/nexis-eco/nexis/services/control-plane

go 1.22

require (
	github.com/go-chi/chi/v5 v5.2.5
	github.com/jackc/pgx/v5 v5.7.1
	github.com/google/uuid v1.6.0
	github.com/golang-jwt/jwt/v5 v5.2.1
	golang.org/x/crypto v0.27.0
)
```

Also: ANY existing `internal/` files under `services/control-plane/` that reference `nexis/backend/...` import paths need a global replacement to the new module path. Run:

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && \
grep -rn "nexis/backend" internal/ 2>/dev/null
```

If there are matches, rewrite them. If the old internal/ code is too coupled to the old 9-service shape, MOVE it to `_legacy/control-plane-internal-old/` and we will rebuild from scratch in T3.2.

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && \
mkdir -p /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/_legacy/control-plane-internal-old && \
mv internal /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/_legacy/control-plane-internal-old/ && \
mkdir -p internal/{domain,usecase,adapter/llm,adapter/repo,transport/http/middleware,transport/http/handler,transport/http/dto,platform/config,platform/db,platform/observability,workflow}
```

- [ ] **Step 4: Write `cmd/server/main.go`** (clean entrypoint that just wires HTTP server + slog)

```go
// services/control-plane/cmd/server/main.go
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config.Load()
	logger.Info("control-plane starting", "port", cfg.Port, "env", cfg.AppEnv)

	srv := httpserver.New(cfg, logger)
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()
	logger.Info("control-plane listening", "addr", httpServer.Addr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	logger.Info("control-plane shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}
```

- [ ] **Step 5: Write `Makefile`** so we can `make build|test|run` consistently

```makefile
.PHONY: build test lint run tidy

build:
	go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server

test:
	go test -race -timeout 120s ./...

lint:
	go vet ./...

run:
	go run ./cmd/server

tidy:
	go mod tidy
```

---

### Task 3.2: Domain + ports skeleton

**Files:**
- Create: `services/control-plane/internal/domain/errors.go`, `services/control-plane/internal/domain/ports.go`

- [ ] **Step 1: `internal/domain/errors.go`** (sentinel errors used by usecases)

```go
package domain

import "errors"

var (
	ErrNotFound   = errors.New("not found")
	ErrForbidden  = errors.New("forbidden")
	ErrConflict   = errors.New("conflict")
	ErrUnknown    = errors.New("unknown")
	ErrLLMRequest = errors.New("llm request failed")
)
```

- [ ] **Step 2: `internal/domain/ports.go`** (interfaces — concretes live in `adapter/`)

```go
// Package domain holds pure business types and the port interfaces
// the usecase layer depends on. Adapters in internal/adapter/* implement these.
package domain

import "context"

// LLMProvider is the only contract every agent module talks to.
// Switching providers (openai → ollama → anthropic) is one wire-up swap.
type LLMProvider interface {
	Name() string
	Info(ctx context.Context) (LLMInfo, error)
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

type LLMInfo struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
	BaseURL  string   `json:"base_url,omitempty"`
}

type CompletionRequest struct {
	Model        string  `json:"model"`
	System       string  `json:"system,omitempty"`
	Prompt       string  `json:"prompt"`
	MaxTokens    int     `json:"max_tokens,omitempty"`
	Temperature  float32 `json:"temperature,omitempty"`
	JSONResponse bool    `json:"json_response,omitempty"`
}

type CompletionResponse struct {
	Content      string `json:"content"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Model        string `json:"model"`
}
```

---

### Task 3.3: Platform — config + (stubs for) db + observability

**Files:**
- Create: `services/control-plane/internal/platform/config/config.go`

- [ ] **Step 1: `internal/platform/config/config.go`**

```go
package config

import "os"

type Config struct {
	Port              string
	AppEnv            string
	LLMProvider       string
	OpenAIAPIKey      string
	OpenAIModelSyn    string
	OpenAIModelCheap  string
	OllamaBaseURL     string
	OllamaModelGen    string
	OllamaModelCode   string
	DatabaseURL       string
}

func Load() Config {
	return Config{
		Port:             env("PORT", "8080"),
		AppEnv:           env("APP_ENV", "dev"),
		LLMProvider:      env("LLM_PROVIDER", "openai"),
		OpenAIAPIKey:     env("OPENAI_API_KEY", ""),
		OpenAIModelSyn:   env("OPENAI_MODEL_SYNTHESIS", "gpt-4o"),
		OpenAIModelCheap: env("OPENAI_MODEL_CHEAP", "gpt-4o-mini"),
		OllamaBaseURL:    env("OLLAMA_BASE_URL", "http://host.docker.internal:11434"),
		OllamaModelGen:   env("OLLAMA_MODEL_GENERAL", "llama3.1:8b"),
		OllamaModelCode:  env("OLLAMA_MODEL_CODE", "qwen2.5-coder:14b"),
		DatabaseURL:      env("DATABASE_URL", ""),
	}
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}
```

> **db** + **observability** stubs deferred to Phase 2 — Phase 1 only needs config to drive the LLM provider wiring.

---

### Task 3.4: Transport — Chi server + healthz handler (TDD)

**Files:**
- Create: `services/control-plane/internal/transport/http/server.go`, `services/control-plane/internal/transport/http/handler/healthz.go`, `services/control-plane/tests/integration/healthz_test.go`

- [ ] **Step 1: Write the failing test first**

`services/control-plane/tests/integration/healthz_test.go`:

```go
package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"log/slog"
	"os"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

func TestHealthz(t *testing.T) {
	srv := httpserver.New(config.Config{Port: "8080", AppEnv: "test"},
		slog.New(slog.NewTextHandler(os.Stdout, nil)))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf(`want status="ok", got %q`, body["status"])
	}
	if body["service"] != "control-plane" {
		t.Fatalf(`want service="control-plane", got %q`, body["service"])
	}
}
```

- [ ] **Step 2: Run test — verify it fails (no server.New yet)**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && go test ./tests/integration/... -run TestHealthz
```

Expected: compile error or test fail (`undefined: httpserver.New`).

- [ ] **Step 3: Implement the healthz handler**

`services/control-plane/internal/transport/http/handler/healthz.go`:

```go
package handler

import (
	"encoding/json"
	"net/http"
)

func Healthz(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": service,
		})
	}
}
```

- [ ] **Step 4: Implement the Chi server wiring**

`services/control-plane/internal/transport/http/server.go`:

```go
package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
)

func New(cfg config.Config, logger *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(60_000_000_000)) // 60s

	r.Get("/healthz", handler.Healthz("control-plane"))
	// /v1/_diag/llm wired in T5.4

	_ = cfg
	_ = logger
	return r
}
```

- [ ] **Step 5: Run test, verify pass**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && \
go mod tidy && \
go test ./tests/integration/... -run TestHealthz -v
```

Expected: `--- PASS: TestHealthz`.

- [ ] **Step 6: Verify the binary builds**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && go build -o /tmp/cp-test ./cmd/server && rm /tmp/cp-test
```

Expected: zero errors.

---

### Task 3.5: Dockerfile for control-plane (multi-stage, distroless)

**Files:**
- Create: `services/control-plane/Dockerfile`

- [ ] **Step 1: Replace the old multi-arg Dockerfile**

```dockerfile
# syntax=docker/dockerfile:1.7
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/server /app/server
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/server"]
```

- [ ] **Step 2: Build & smoke-run the image**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && \
docker build -t nexis/control-plane:dev . && \
docker run --rm -d --name cp-smoke -p 18080:8080 nexis/control-plane:dev && \
sleep 1 && curl -fs http://localhost:18080/healthz && echo && \
docker rm -f cp-smoke
```

Expected: `{"service":"control-plane","status":"ok"}`. Image size < 25 MB.

---

### Task 3.6: go-arch-lint config (architecture rules per §3.7)

**Files:**
- Create: `services/control-plane/.arch.yaml`

- [ ] **Step 1: Write the lint config**

```yaml
version: 2
workdir: .

components:
  domain:
    in: internal/domain
  usecase:
    in: internal/usecase
  adapter:
    in: internal/adapter/**
  transport:
    in: internal/transport/**
  platform:
    in: internal/platform/**
  workflow:
    in: internal/workflow/**

deps:
  domain:
    mayDependOn: []
  usecase:
    mayDependOn: [domain]
  adapter:
    mayDependOn: [domain, platform]
  transport:
    mayDependOn: [usecase, domain, platform]
  workflow:
    mayDependOn: [usecase, domain, adapter, platform]
  platform:
    mayDependOn: []
```

- [ ] **Step 2: Install + run go-arch-lint**

```bash
go install github.com/fe3dback/go-arch-lint@latest && \
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && \
$(go env GOPATH)/bin/go-arch-lint check
```

Expected: `OK` or no violations. Add a `lint` target to the Makefile that runs both `go vet ./...` and `go-arch-lint check`.

---

## Stage 4 — Validator + GitOps minimal services

### Task 4.1: Scaffold validator + gitops (parallel-safe)

**Files:**
- Create: `services/validator/{go.mod,cmd/server/main.go,Dockerfile,internal/transport/http/server.go}`
- Create: `services/gitops/{go.mod,cmd/server/main.go,Dockerfile,internal/transport/http/server.go}`

- [ ] **Step 1: Scaffold validator**

```bash
mkdir -p /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/validator/cmd/server \
         /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/validator/internal/transport/http
```

`services/validator/go.mod`:
```
module github.com/nexis-eco/nexis/services/validator

go 1.22

require github.com/go-chi/chi/v5 v5.2.5
```

`services/validator/cmd/server/main.go`:
```go
package main

import (
	"log/slog"
	"net/http"
	"os"

	httpsrv "github.com/nexis-eco/nexis/services/validator/internal/transport/http"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	port := os.Getenv("PORT")
	if port == "" { port = "8081" }
	logger.Info("validator starting", "port", port)
	if err := http.ListenAndServe(":"+port, httpsrv.New("validator")); err != nil {
		logger.Error("server error", "err", err); os.Exit(1)
	}
}
```

`services/validator/internal/transport/http/server.go`:
```go
package http

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func New(serviceName string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": serviceName})
	})
	return r
}
```

`services/validator/Dockerfile`:
```dockerfile
# syntax=docker/dockerfile:1.7
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/server /app/server
USER nonroot:nonroot
EXPOSE 8081
ENTRYPOINT ["/app/server"]
```

- [ ] **Step 2: Scaffold gitops** (identical shape, port 8082)

```bash
mkdir -p /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/gitops/cmd/server \
         /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/gitops/internal/transport/http
```

Same files as validator with `validator → gitops`, `8081 → 8082`. Module path `github.com/nexis-eco/nexis/services/gitops`.

- [ ] **Step 3: Build, smoke-run both**

```bash
for s in validator gitops; do
  cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/$s && go mod tidy && go build ./... && cd -;
done
```

Expected: zero errors for both.

---

## Stage 5 — LLM provider abstraction (TDD)

### Task 5.1: Provider interface lives in domain (already done T3.2). Write the OpenAI adapter (TDD)

**Files:**
- Create: `services/control-plane/internal/adapter/llm/openai.go`, `services/control-plane/internal/adapter/llm/openai_test.go`

- [ ] **Step 1: Write failing test using a mocked HTTP server**

`services/control-plane/internal/adapter/llm/openai_test.go`:

```go
package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIProvider_Complete(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing/invalid auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-x",
			"object":  "chat.completion",
			"model":   "gpt-4o-mini",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": "pong"},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 1, "total_tokens": 6},
		})
	}))
	defer mock.Close()

	p := NewOpenAIProvider(OpenAIConfig{
		BaseURL: mock.URL,
		APIKey:  "test-key",
		Default: "gpt-4o-mini",
	})
	resp, err := p.Complete(context.Background(), CompletionRequest{Prompt: "ping"})
	if err != nil { t.Fatalf("Complete: %v", err) }
	if resp.Content != "pong" { t.Fatalf(`want "pong", got %q`, resp.Content) }
	if resp.OutputTokens != 1 { t.Fatalf("want 1 output token, got %d", resp.OutputTokens) }
}

func TestOpenAIProvider_Info(t *testing.T) {
	p := NewOpenAIProvider(OpenAIConfig{
		BaseURL: "https://api.openai.com",
		Default: "gpt-4o",
	})
	info, err := p.Info(context.Background())
	if err != nil { t.Fatalf("Info: %v", err) }
	if info.Provider != "openai" { t.Fatalf(`want provider="openai", got %q`, info.Provider) }
	if len(info.Models) == 0 { t.Fatal("want at least one model in Info") }
}
```

> **Note:** The test imports `CompletionRequest` from THIS package, not from `domain`. We re-export the domain types into this package via type aliases so test + handler code reads naturally. See Step 3.

- [ ] **Step 2: Run — verify it fails**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && \
go test ./internal/adapter/llm/...
```

Expected: compile error (`undefined: NewOpenAIProvider`).

- [ ] **Step 3: Implement OpenAI provider**

`services/control-plane/internal/adapter/llm/openai.go`:

```go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Re-export domain types so callers stay in this package.
type (
	CompletionRequest  = domain.CompletionRequest
	CompletionResponse = domain.CompletionResponse
	Info               = domain.LLMInfo
)

type OpenAIConfig struct {
	BaseURL string
	APIKey  string
	Default string // default model id (e.g. gpt-4o-mini)
}

type OpenAIProvider struct {
	cfg  OpenAIConfig
	http *http.Client
}

func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com"
	}
	return &OpenAIProvider{
		cfg:  cfg,
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Info(_ context.Context) (Info, error) {
	return Info{
		Provider: "openai",
		BaseURL:  p.cfg.BaseURL,
		Models:   []string{p.cfg.Default},
	}, nil
}

func (p *OpenAIProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	model := req.Model
	if model == "" { model = p.cfg.Default }

	messages := []map[string]string{}
	if req.System != "" { messages = append(messages, map[string]string{"role": "system", "content": req.System}) }
	messages = append(messages, map[string]string{"role": "user", "content": req.Prompt})

	body := map[string]any{
		"model":    model,
		"messages": messages,
	}
	if req.MaxTokens > 0 { body["max_tokens"] = req.MaxTokens }
	if req.Temperature > 0 { body["temperature"] = req.Temperature }
	if req.JSONResponse {
		body["response_format"] = map[string]string{"type": "json_object"}
	}

	buf, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/v1/chat/completions", bytes.NewReader(buf))
	if err != nil { return CompletionResponse{}, fmt.Errorf("openai: build request: %w", err) }
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	resp, err := p.http.Do(httpReq)
	if err != nil { return CompletionResponse{}, fmt.Errorf("%w: openai http: %v", domain.ErrLLMRequest, err) }
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return CompletionResponse{}, fmt.Errorf("%w: openai status %d: %s", domain.ErrLLMRequest, resp.StatusCode, string(raw))
	}

	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct{ Content string `json:"content"` } `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return CompletionResponse{}, fmt.Errorf("%w: openai decode: %v", domain.ErrLLMRequest, err)
	}
	if len(parsed.Choices) == 0 {
		return CompletionResponse{}, fmt.Errorf("%w: openai returned no choices", domain.ErrLLMRequest)
	}
	return CompletionResponse{
		Content:      parsed.Choices[0].Message.Content,
		Model:        parsed.Model,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
	}, nil
}
```

- [ ] **Step 4: Run tests — verify pass**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && \
go mod tidy && \
go test ./internal/adapter/llm/... -v -run TestOpenAIProvider
```

Expected: `--- PASS: TestOpenAIProvider_Complete`, `--- PASS: TestOpenAIProvider_Info`.

---

### Task 5.2: Ollama adapter (TDD)

**Files:**
- Create: `services/control-plane/internal/adapter/llm/ollama.go`, `services/control-plane/internal/adapter/llm/ollama_test.go`

- [ ] **Step 1: Write failing test**

`services/control-plane/internal/adapter/llm/ollama_test.go`:

```go
package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaProvider_Info(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("want /api/tags, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{"name": "llama3.1:8b", "size": 4_700_000_000},
			},
		})
	}))
	defer mock.Close()

	p := NewOllamaProvider(OllamaConfig{BaseURL: mock.URL, Default: "llama3.1:8b"})
	info, err := p.Info(context.Background())
	if err != nil { t.Fatalf("Info: %v", err) }
	if info.Provider != "ollama" { t.Fatalf(`want provider="ollama", got %q`, info.Provider) }
	if len(info.Models) != 1 || info.Models[0] != "llama3.1:8b" {
		t.Fatalf("want [llama3.1:8b], got %v", info.Models)
	}
}

func TestOllamaProvider_Complete(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("want /api/chat, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":    "llama3.1:8b",
			"message":  map[string]string{"role": "assistant", "content": "pong"},
			"done":     true,
			"prompt_eval_count": 5,
			"eval_count":        1,
		})
	}))
	defer mock.Close()

	p := NewOllamaProvider(OllamaConfig{BaseURL: mock.URL, Default: "llama3.1:8b"})
	resp, err := p.Complete(context.Background(), CompletionRequest{Prompt: "ping"})
	if err != nil { t.Fatalf("Complete: %v", err) }
	if resp.Content != "pong" { t.Fatalf(`want "pong", got %q`, resp.Content) }
	if resp.InputTokens != 5 || resp.OutputTokens != 1 {
		t.Fatalf("want 5/1 tokens, got %d/%d", resp.InputTokens, resp.OutputTokens)
	}
}
```

- [ ] **Step 2: Run — fails**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && \
go test ./internal/adapter/llm/... -run TestOllamaProvider
```

Expected: undefined symbols.

- [ ] **Step 3: Implement Ollama provider**

`services/control-plane/internal/adapter/llm/ollama.go`:

```go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type OllamaConfig struct {
	BaseURL string
	Default string
}

type OllamaProvider struct {
	cfg  OllamaConfig
	http *http.Client
}

func NewOllamaProvider(cfg OllamaConfig) *OllamaProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://host.docker.internal:11434"
	}
	return &OllamaProvider{cfg: cfg, http: &http.Client{Timeout: 5 * time.Minute}}
}

func (p *OllamaProvider) Name() string { return "ollama" }

func (p *OllamaProvider) Info(ctx context.Context) (Info, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.BaseURL+"/api/tags", nil)
	if err != nil { return Info{}, fmt.Errorf("ollama: build tags request: %w", err) }
	resp, err := p.http.Do(httpReq)
	if err != nil { return Info{}, fmt.Errorf("%w: ollama tags: %v", domain.ErrLLMRequest, err) }
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return Info{}, fmt.Errorf("%w: ollama tags status %d: %s", domain.ErrLLMRequest, resp.StatusCode, string(raw))
	}
	var parsed struct {
		Models []struct{ Name string `json:"name"` } `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return Info{}, fmt.Errorf("%w: ollama tags decode: %v", domain.ErrLLMRequest, err)
	}
	models := make([]string, 0, len(parsed.Models))
	for _, m := range parsed.Models { models = append(models, m.Name) }
	return Info{Provider: "ollama", BaseURL: p.cfg.BaseURL, Models: models}, nil
}

func (p *OllamaProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	model := req.Model
	if model == "" { model = p.cfg.Default }

	messages := []map[string]string{}
	if req.System != "" { messages = append(messages, map[string]string{"role": "system", "content": req.System}) }
	messages = append(messages, map[string]string{"role": "user", "content": req.Prompt})

	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}
	if req.JSONResponse { body["format"] = "json" }

	buf, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/api/chat", bytes.NewReader(buf))
	if err != nil { return CompletionResponse{}, fmt.Errorf("ollama: build chat request: %w", err) }
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(httpReq)
	if err != nil { return CompletionResponse{}, fmt.Errorf("%w: ollama chat: %v", domain.ErrLLMRequest, err) }
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return CompletionResponse{}, fmt.Errorf("%w: ollama chat status %d: %s", domain.ErrLLMRequest, resp.StatusCode, string(raw))
	}
	var parsed struct {
		Model           string `json:"model"`
		Message         struct{ Content string `json:"content"` } `json:"message"`
		PromptEvalCount int    `json:"prompt_eval_count"`
		EvalCount       int    `json:"eval_count"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return CompletionResponse{}, fmt.Errorf("%w: ollama decode: %v", domain.ErrLLMRequest, err)
	}
	return CompletionResponse{
		Content:      parsed.Message.Content,
		Model:        parsed.Model,
		InputTokens:  parsed.PromptEvalCount,
		OutputTokens: parsed.EvalCount,
	}, nil
}
```

- [ ] **Step 4: Run — passes**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && \
go test ./internal/adapter/llm/... -v
```

Expected: all 4 tests pass.

---

### Task 5.3: Provider factory (LLM_PROVIDER env switch)

**Files:**
- Create: `services/control-plane/internal/adapter/llm/factory.go`, `services/control-plane/internal/adapter/llm/factory_test.go`

- [ ] **Step 1: Failing test**

`services/control-plane/internal/adapter/llm/factory_test.go`:

```go
package llm

import (
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

func TestNewFromConfig(t *testing.T) {
	cases := []struct {
		name     string
		cfg      config.Config
		wantName string
		wantErr  bool
	}{
		{"openai", config.Config{LLMProvider: "openai", OpenAIAPIKey: "k", OpenAIModelCheap: "gpt-4o-mini"}, "openai", false},
		{"ollama", config.Config{LLMProvider: "ollama", OllamaBaseURL: "http://x", OllamaModelGen: "llama3.1:8b"}, "ollama", false},
		{"unknown", config.Config{LLMProvider: "wat"}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := NewFromConfig(c.cfg)
			if c.wantErr {
				if err == nil { t.Fatal("want error, got nil") }
				return
			}
			if err != nil { t.Fatalf("unexpected err: %v", err) }
			if p.Name() != c.wantName { t.Fatalf("want %q, got %q", c.wantName, p.Name()) }
		})
	}
}
```

- [ ] **Step 2: Implement factory**

`services/control-plane/internal/adapter/llm/factory.go`:

```go
package llm

import (
	"fmt"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

func NewFromConfig(cfg config.Config) (domain.LLMProvider, error) {
	switch cfg.LLMProvider {
	case "openai":
		return NewOpenAIProvider(OpenAIConfig{
			APIKey:  cfg.OpenAIAPIKey,
			Default: cfg.OpenAIModelCheap,
		}), nil
	case "ollama":
		return NewOllamaProvider(OllamaConfig{
			BaseURL: cfg.OllamaBaseURL,
			Default: cfg.OllamaModelGen,
		}), nil
	default:
		return nil, fmt.Errorf("unknown LLM_PROVIDER %q (want openai|ollama)", cfg.LLMProvider)
	}
}
```

- [ ] **Step 3: Run test — passes**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && \
go test ./internal/adapter/llm/... -v
```

---

### Task 5.4: `/v1/_diag/llm` HTTP endpoint (TDD)

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/llm_diag.go`
- Modify: `services/control-plane/internal/transport/http/server.go` (add route)
- Add test: `services/control-plane/tests/integration/llm_diag_test.go`

- [ ] **Step 1: Failing integration test**

`services/control-plane/tests/integration/llm_diag_test.go`:

```go
package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"log/slog"
	"os"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

func TestLLMDiag_Ollama(t *testing.T) {
	// stand up a fake ollama
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"llama3.1:8b"}]}`))
	}))
	defer ollama.Close()

	srv := httpserver.New(config.Config{
		Port: "0", AppEnv: "test",
		LLMProvider: "ollama", OllamaBaseURL: ollama.URL, OllamaModelGen: "llama3.1:8b",
	}, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	req := httptest.NewRequest(http.MethodGet, "/v1/_diag/llm", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK { t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String()) }
	var info map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil { t.Fatalf("decode: %v", err) }
	if info["provider"] != "ollama" { t.Fatalf("want provider=ollama, got %v", info["provider"]) }
	models, _ := info["models"].([]any)
	if len(models) != 1 || !strings.Contains(models[0].(string), "llama3.1") {
		t.Fatalf("want llama3.1 model, got %v", info["models"])
	}
}
```

- [ ] **Step 2: Implement handler**

`services/control-plane/internal/transport/http/handler/llm_diag.go`:

```go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

func LLMDiag(p domain.LLMProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info, err := p.Info(r.Context())
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"provider": p.Name(),
				"error":    err.Error(),
			})
			return
		}
		_ = json.NewEncoder(w).Encode(info)
	}
}
```

- [ ] **Step 3: Wire factory + route in server.go**

Replace `internal/transport/http/server.go` with:

```go
package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/llm"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
)

func New(cfg config.Config, logger *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(60_000_000_000))

	r.Get("/healthz", handler.Healthz("control-plane"))

	if provider, err := llm.NewFromConfig(cfg); err == nil {
		r.Get("/v1/_diag/llm", handler.LLMDiag(provider))
	} else {
		logger.Warn("llm provider not configured", "err", err)
	}

	return r
}
```

- [ ] **Step 4: Run test — passes**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && \
go test ./... -v
```

Expected: all tests pass (Healthz + LLMDiag_Ollama + 5 LLM unit tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app add -A && \
git -C /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app commit -m "feat(control-plane): healthz + LLM provider abstraction (openai+ollama) + /v1/_diag/llm

- TDD: 5 unit tests + 2 integration tests pass.
- LLM_PROVIDER env switches openai|ollama with no code change.
- go-arch-lint enforces internal layering (domain/usecase/adapter/transport/platform/workflow).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Stage 6 — Web app (Next.js 16.2.2) — move + light tokens + shadcn

### Task 6.1: Move site/ → apps/web/ + dependency upgrade

**Files:**
- Move: `site/` → `apps/web/`
- Modify: `apps/web/package.json` (add deps)
- Create: `apps/web/Dockerfile`

- [ ] **Step 1: Move and verify nothing references the old path**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && \
mkdir -p apps && mv site apps/web && \
grep -rn "/site/" apps/web/ 2>/dev/null | head -20
```

Expected: no matches (the package was self-contained). If matches appear, fix imports.

- [ ] **Step 2: Read current package.json before editing**

```bash
cat /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/package.json
```

- [ ] **Step 3: Rewrite `apps/web/package.json` with the Phase 1 deps**

```json
{
  "name": "@nexis/web",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start",
    "lint": "eslint",
    "typecheck": "tsc --noEmit",
    "test": "vitest run"
  },
  "dependencies": {
    "@radix-ui/react-dialog": "^1.1.2",
    "@radix-ui/react-slot": "^1.1.0",
    "@radix-ui/react-toast": "^1.2.2",
    "class-variance-authority": "^0.7.1",
    "clsx": "^2.1.1",
    "drizzle-orm": "^0.36.4",
    "framer-motion": "^12.38.0",
    "lucide-react": "^0.460.0",
    "next": "16.2.2",
    "next-themes": "^0.4.3",
    "postgres": "^3.4.5",
    "react": "19.2.4",
    "react-dom": "19.2.4",
    "tailwind-merge": "^2.5.4"
  },
  "devDependencies": {
    "@tailwindcss/postcss": "^4",
    "@types/node": "^22",
    "@types/react": "^19",
    "@types/react-dom": "^19",
    "@vitejs/plugin-react": "^4.3.3",
    "drizzle-kit": "^0.28.0",
    "eslint": "^9",
    "eslint-config-next": "16.2.2",
    "tailwindcss": "^4",
    "tw-animate-css": "^1.4.7",
    "typescript": "^5",
    "vitest": "^2.1.8"
  }
}
```

- [ ] **Step 4: Read Next 16.2.2's local docs before writing any code that uses Next conventions**

This is mandatory — `apps/web/AGENTS.md` warns Next 16 has breaking changes vs training data:

```bash
ls /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/node_modules/next/dist/docs/ 2>/dev/null | head -20
```

If empty (because we haven't installed yet), do install first:

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && pnpm install
```

Then read the docs index for Next 16 routing/runtime/config gotchas before any handler/route work in T6.5/T8.2:

```bash
cat /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/node_modules/next/dist/docs/SUPPORTED_FEATURES.md 2>/dev/null | head -80
ls /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/node_modules/next/dist/docs/
```

- [ ] **Step 5: `apps/web/Dockerfile`** (multi-stage standalone Next build)

```dockerfile
# syntax=docker/dockerfile:1.7
FROM node:22-alpine AS deps
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@9.12.0 --activate
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json turbo.json ./
COPY apps/web/package.json ./apps/web/
RUN pnpm install --frozen-lockfile

FROM node:22-alpine AS builder
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@9.12.0 --activate
COPY --from=deps /app/node_modules ./node_modules
COPY --from=deps /app/apps/web/node_modules ./apps/web/node_modules
COPY . .
WORKDIR /app/apps/web
RUN pnpm build

FROM node:22-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production HOSTNAME=0.0.0.0 PORT=3000
COPY --from=builder /app/apps/web/.next/standalone ./
COPY --from=builder /app/apps/web/.next/static ./apps/web/.next/static
COPY --from=builder /app/apps/web/public ./apps/web/public
EXPOSE 3000
CMD ["node", "apps/web/server.js"]
```

> **Note:** This Dockerfile assumes `next.config.ts` sets `output: "standalone"`. We add that in T6.5.

---

### Task 6.2: Light-mode design tokens (rewrite `globals.css` + tailwind config)

**Files:**
- Modify: `apps/web/app/globals.css` (FULL REWRITE per §4.1)
- Create: `apps/web/tailwind.config.ts`
- Create: `apps/web/lib/tokens.ts`

- [ ] **Step 1: Rewrite `apps/web/app/globals.css`** with the light palette per §4.1

```css
@import "tailwindcss";
@import "tw-animate-css";

@custom-variant dark (&:where(.dark, .dark *));

/* === Light mode (default) — per docs/PROJECT_PLAN.md §4.1 === */
:root {
  --background: hsl(0 0% 100%);
  --foreground: hsl(222 47% 11%);            /* #0B1220 navy */
  --muted: hsl(210 20% 96%);
  --muted-foreground: hsl(215 16% 47%);
  --border: hsl(214 32% 91%);
  --card: hsl(0 0% 100%);
  --card-foreground: hsl(222 47% 11%);
  --popover: hsl(0 0% 100%);
  --popover-foreground: hsl(222 47% 11%);
  --primary: hsl(217 91% 60%);               /* #3B82F6 brand */
  --primary-foreground: hsl(0 0% 100%);
  --secondary: hsl(210 20% 96%);
  --secondary-foreground: hsl(222 47% 11%);
  --accent: hsl(217 60% 82%);                /* #BFCFE8 */
  --accent-foreground: hsl(222 47% 14%);
  --destructive: hsl(0 72% 51%);
  --destructive-foreground: hsl(0 0% 100%);
  --warning: hsl(38 92% 50%);
  --success: hsl(160 84% 39%);
  --ring: hsl(217 91% 60%);
  --radius: 0.625rem;
}

/* === Dark mode (opt-in via next-themes) === */
.dark {
  --background: hsl(222 47% 6%);             /* #0A0E1A */
  --foreground: hsl(210 20% 96%);
  --muted: hsl(222 40% 9%);
  --muted-foreground: hsl(215 16% 65%);
  --border: hsl(217 19% 20%);
  --card: hsl(222 40% 11%);
  --card-foreground: hsl(210 20% 96%);
  --popover: hsl(222 40% 9%);
  --popover-foreground: hsl(210 20% 96%);
  --primary: hsl(217 91% 65%);
  --primary-foreground: hsl(222 47% 6%);
  --secondary: hsl(222 40% 11%);
  --secondary-foreground: hsl(210 20% 96%);
  --accent: hsl(222 40% 14%);
  --accent-foreground: hsl(210 20% 96%);
  --destructive: hsl(0 72% 51%);
  --destructive-foreground: hsl(0 0% 100%);
  --warning: hsl(38 92% 50%);
  --success: hsl(160 84% 45%);
  --ring: hsl(217 91% 65%);
}

@theme inline {
  /* Canonical names (shadcn-style) */
  --color-background: var(--background);
  --color-foreground: var(--foreground);
  --color-muted: var(--muted);
  --color-muted-foreground: var(--muted-foreground);
  --color-border: var(--border);
  --color-card: var(--card);
  --color-card-foreground: var(--card-foreground);
  --color-popover: var(--popover);
  --color-popover-foreground: var(--popover-foreground);
  --color-primary: var(--primary);
  --color-primary-foreground: var(--primary-foreground);
  --color-secondary: var(--secondary);
  --color-secondary-foreground: var(--secondary-foreground);
  --color-accent: var(--accent);
  --color-accent-foreground: var(--accent-foreground);
  --color-destructive: var(--destructive);
  --color-destructive-foreground: var(--destructive-foreground);
  --color-warning: var(--warning);
  --color-success: var(--success);
  --color-ring: var(--ring);

  /* Backward-compat aliases — let existing components keep using their
     class names (text-text-primary, bg-surface, etc.) while re-pointing to
     the new light-default tokens. Lets Stage 7 be a token swap, not a rewrite. */
  --color-surface: var(--muted);
  --color-border-hover: var(--accent);
  --color-text-primary: var(--foreground);
  --color-text-secondary: var(--muted-foreground);
  --color-text-muted: var(--muted-foreground);
  --color-accent-alt: var(--accent);
  --color-primary-soft: color-mix(in srgb, var(--primary) 18%, transparent);
  --color-on-primary: var(--primary-foreground);
  --color-secondary-soft: color-mix(in srgb, var(--muted-foreground) 22%, transparent);
  --color-amber: var(--warning);
  --color-amber-soft: color-mix(in srgb, var(--warning) 18%, transparent);
  --color-green-signal: var(--success);
  --color-amber-signal: var(--warning);
  --color-red-signal: var(--destructive);
  --color-navbar-bg: color-mix(in srgb, var(--background) 88%, transparent);

  --radius-sm: calc(var(--radius) - 4px);
  --radius-md: calc(var(--radius) - 2px);
  --radius-lg: var(--radius);
  --radius-xl: calc(var(--radius) + 8px);

  --font-sans: var(--font-inter), ui-sans-serif, system-ui, sans-serif;
  --font-mono: var(--font-jetbrains-mono), ui-monospace, monospace;
}

/* Body baseline */
body {
  background: var(--background);
  color: var(--foreground);
  font-family: var(--font-sans);
  -webkit-font-smoothing: antialiased;
}

html {
  scroll-behavior: smooth;
  scroll-padding-top: 88px;
}

@media (prefers-reduced-motion: reduce) {
  html { scroll-behavior: auto; }
}
```

- [ ] **Step 2: `apps/web/tailwind.config.ts`** (Tailwind v4 still benefits from a config for content paths)

```ts
import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./app/**/*.{ts,tsx}",
    "./components/**/*.{ts,tsx}",
    "./lib/**/*.{ts,tsx}",
  ],
  darkMode: ["class"],
};

export default config;
```

- [ ] **Step 3: `apps/web/lib/tokens.ts`** — TypeScript export so JS code can read brand colors without parsing CSS

```ts
export const tokens = {
  brand: {
    primary: "#3B82F6",
    accent: "#BFCFE8",
    deep: "#0B1220",
  },
  motion: {
    durationMs: 600,
    ease: [0.22, 1, 0.36, 1] as const,
    staggerMs: 60,
  },
} as const;
```

---

### Task 6.3: shadcn/ui init (manual config — no CLI lock-in)

**Files:**
- Create: `apps/web/components.json`, `apps/web/lib/utils.ts`
- Modify: `apps/web/components/ui/Button.tsx` (rewrite in place — keep PascalCase)

- [ ] **Step 1: `apps/web/components.json`** (shadcn config — even though we install deps directly)

```json
{
  "$schema": "https://ui.shadcn.com/schema.json",
  "style": "new-york",
  "rsc": true,
  "tsx": true,
  "tailwind": {
    "config": "tailwind.config.ts",
    "css": "app/globals.css",
    "baseColor": "slate",
    "cssVariables": true
  },
  "aliases": {
    "components": "@/components",
    "utils": "@/lib/utils",
    "ui": "@/components/ui",
    "lib": "@/lib",
    "hooks": "@/hooks"
  },
  "iconLibrary": "lucide"
}
```

- [ ] **Step 2: `apps/web/lib/utils.ts`** (the standard shadcn utility)

```ts
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
```

- [ ] **Step 3: Rewrite `apps/web/components/ui/Button.tsx` IN PLACE (keep PascalCase to avoid macOS case-insensitive FS collisions and broken imports) with shadcn-style variant API**

First inspect the existing one to preserve any unique props/exports its callers depend on:

```bash
cat /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/ui/Button.tsx && \
grep -rn "from \"@/components/ui/Button\"" /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/
```

Then overwrite `apps/web/components/ui/Button.tsx` keeping the named export `Button` (matching every existing import):

```tsx
"use client";

import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)] disabled:pointer-events-none disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-[var(--color-primary)] text-[var(--color-primary-foreground)] hover:opacity-90",
        ghost: "hover:bg-[var(--color-muted)] text-[var(--color-foreground)]",
        outline: "border border-[var(--color-border)] hover:bg-[var(--color-muted)] text-[var(--color-foreground)]",
        link: "text-[var(--color-primary)] underline-offset-4 hover:underline",
      },
      size: {
        default: "h-10 px-4 py-2",
        sm: "h-9 rounded-md px-3",
        lg: "h-11 rounded-md px-6 text-base",
        icon: "h-10 w-10",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  }
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return <Comp ref={ref} className={cn(buttonVariants({ variant, size, className }))} {...props} />;
  }
);
Button.displayName = "Button";
```

- [ ] **Step 4: Smoke check — typecheck still passes**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && pnpm --filter @nexis/web typecheck
```

Expected: zero TS errors. (Existing imports still resolve since the file path didn't change.)

---

### Task 6.4: next-themes provider + theme toggle

**Files:**
- Create: `apps/web/components/ThemeProvider.tsx`, `apps/web/components/ThemeToggle.tsx`

- [ ] **Step 1: `apps/web/components/ThemeProvider.tsx`**

```tsx
"use client";

import * as React from "react";
import { ThemeProvider as NextThemesProvider } from "next-themes";

export function ThemeProvider({ children, ...props }: React.ComponentProps<typeof NextThemesProvider>) {
  return (
    <NextThemesProvider attribute="class" defaultTheme="light" enableSystem={false} {...props}>
      {children}
    </NextThemesProvider>
  );
}
```

- [ ] **Step 2: `apps/web/components/ThemeToggle.tsx`**

```tsx
"use client";

import * as React from "react";
import { Moon, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import { Button } from "@/components/ui/Button";

export function ThemeToggle() {
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = React.useState(false);
  React.useEffect(() => setMounted(true), []);
  if (!mounted) return <Button variant="ghost" size="icon" aria-label="Toggle theme" />;
  const isDark = theme === "dark";
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={isDark ? "Switch to light mode" : "Switch to dark mode"}
      onClick={() => setTheme(isDark ? "light" : "dark")}
    >
      {isDark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </Button>
  );
}
```

---

### Task 6.5: Rewrite `app/layout.tsx` for light-default + theme provider

**Files:**
- Modify: `apps/web/app/layout.tsx`
- Modify: `apps/web/next.config.ts`

- [ ] **Step 1: Read current layout (preserve metadata, JsonLd, MotionProvider, fonts)**

```bash
cat /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/layout.tsx
```

- [ ] **Step 2: Rewrite layout — remove dark `bg-background text-text-primary`, wrap with ThemeProvider, default `class="light"`**

```tsx
import type { Metadata, Viewport } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";

import { MotionProvider } from "@/components/motion/MotionProvider";
import { JsonLd } from "@/components/seo/JsonLd";
import { ThemeProvider } from "@/components/ThemeProvider";
import { getSiteUrl } from "@/lib/site";

import "./globals.css";

const inter = Inter({
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
  variable: "--font-inter",
  display: "swap",
});

const jetBrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  weight: ["400", "500"],
  variable: "--font-jetbrains-mono",
  display: "swap",
});

const siteUrl = getSiteUrl();
const defaultDescription =
  "Nine AI agents. One engineering team that ships fixes while you sleep. NEXIS detects pipeline failures, traces root cause, validates patches in isolation, and routes fixes through engineer approval.";

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#ffffff" },
    { media: "(prefers-color-scheme: dark)",  color: "#0A0E1A" },
  ],
};

export const metadata: Metadata = {
  metadataBase: new URL(siteUrl),
  title: { default: "NEXIS — Autonomous engineering, supervised by you.", template: "%s | NEXIS" },
  applicationName: "NEXIS",
  description: defaultDescription,
  alternates: { canonical: "/" },
  robots: { index: true, follow: true },
  openGraph: {
    type: "website", locale: "en_US", url: siteUrl, siteName: "NEXIS",
    title: "NEXIS — Autonomous engineering, supervised by you.",
    description: defaultDescription,
    images: [{ url: "/logo-light.svg", width: 540, height: 140, alt: "NEXIS" }],
  },
  twitter: { card: "summary_large_image", title: "NEXIS", description: defaultDescription, images: ["/logo-light.svg"] },
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" className={`${inter.variable} ${jetBrainsMono.variable} h-full antialiased`} suppressHydrationWarning>
      <body className="min-h-full flex flex-col font-sans">
        <ThemeProvider>
          <JsonLd />
          <MotionProvider>{children}</MotionProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
```

- [ ] **Step 3: `apps/web/next.config.ts`** — enable standalone output (required by Dockerfile)

```ts
import type { NextConfig } from "next";

const config: NextConfig = {
  output: "standalone",
  reactStrictMode: true,
  experimental: { typedRoutes: true },
};

export default config;
```

- [ ] **Step 4: Smoke-run dev server**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && \
pnpm --filter @nexis/web dev &
SERVER_PID=$!
sleep 8 && curl -fsI http://localhost:3000 | head -1 && \
kill $SERVER_PID 2>/dev/null
```

Expected: HTTP/1.1 200. Page renders without server errors. (May still look broken — sections come next.)

---

### Task 6.6: Logo light variant + ThemeAwareLogo component

**Files:**
- Create: `apps/web/public/logo-light.svg` (per §4.2 — generate by recoloring `public/logo.svg`)
- Create: `apps/web/components/ThemeAwareLogo.tsx`

- [ ] **Step 1: Inspect existing `logo.svg`**

```bash
cat /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/public/logo.svg
```

Identify all `stroke="#BFCFE8"` (light-blue) and `stroke="#3B82F6"` (brand-blue) elements. Per §4.2: light-blue → `#0B1220`, brand-blue → stay `#3B82F6`.

- [ ] **Step 2: Create the light variant by string replacement**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/public && \
sed 's/#BFCFE8/#0B1220/g; s/#bfcfe8/#0B1220/g' logo.svg > logo-light.svg
```

Inspect the result to make sure it's not mangled:

```bash
diff logo.svg logo-light.svg
```

- [ ] **Step 3: `apps/web/components/ThemeAwareLogo.tsx`**

```tsx
"use client";

import * as React from "react";
import Image from "next/image";
import { useTheme } from "next-themes";

export function ThemeAwareLogo({ className, width = 120, height = 32 }: {
  className?: string;
  width?: number;
  height?: number;
}) {
  const { resolvedTheme } = useTheme();
  const [mounted, setMounted] = React.useState(false);
  React.useEffect(() => setMounted(true), []);
  const src = !mounted || resolvedTheme === "light" ? "/logo-light.svg" : "/logo.svg";
  return <Image src={src} alt="NEXIS" width={width} height={height} className={className} priority />;
}
```

---

### Task 6.7: packages/ui placeholder

**Files:**
- Create: `packages/ui/package.json`, `packages/ui/index.ts`

> Phase 1 keeps web's components in `apps/web/components/`. `packages/ui/` is reserved for components that get shared with the future console app. This placeholder makes the workspace import resolvable now so we can extract later without a refactor.

- [ ] **Step 1: `packages/ui/package.json`**

```json
{
  "name": "@nexis/ui",
  "version": "0.0.0",
  "private": true,
  "main": "./index.ts",
  "types": "./index.ts"
}
```

- [ ] **Step 2: `packages/ui/index.ts`**

```ts
// Reserved for shared UI primitives extracted from apps/web in later phases.
export {};
```

- [ ] **Step 3: `pnpm install` to register the workspace package**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && pnpm install
```

---

## Stage 7 — Landing page (9 sections per §4.1)

> **Existing components from `apps/web/components/sections/` are re-skinned, not rewritten** — they already have the right structure but use dark tokens. Each task below opens the existing file, swaps tokens, and confirms it renders correctly against the new light palette.
>
> **Section IDs match the §4.1 spec:** `#features`, `#how-it-works`, `#agents`, `#metrics`, `#cta` — keep these so the existing nav links still resolve.

### Task 7.0: Compose the landing in `app/page.tsx`

**Files:**
- Modify: `apps/web/app/page.tsx`

- [ ] **Step 1: Replace `app/page.tsx` to render all 9 sections in order**

```tsx
import Footer from "@/components/layout/Footer";
import Navbar from "@/components/layout/Navbar";
import Hero from "@/components/sections/Hero";
import TrustedStrip from "@/components/sections/TrustedStrip";
import Problem from "@/components/sections/Problem";
import HowItWorks from "@/components/sections/HowItWorks";
import Agents from "@/components/sections/Agents";
import LiveDemoEmbed from "@/components/sections/LiveDemoEmbed";
import Metrics from "@/components/sections/Metrics";
import CTA from "@/components/sections/CTA";

export default function Page() {
  return (
    <>
      <Navbar />
      <main>
        <Hero />
        <TrustedStrip />
        <Problem />
        <HowItWorks />
        <Agents />
        <LiveDemoEmbed />
        <Metrics />
        <CTA />
      </main>
      <Footer />
    </>
  );
}
```

---

### Task 7.1: Re-skin Navbar (sticky + scroll-aware blur, light mode)

**Files:**
- Modify: `apps/web/components/layout/Navbar.tsx`

- [ ] **Step 1: Read existing Navbar**

```bash
cat /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/layout/Navbar.tsx
```

- [ ] **Step 2: Replace with light-mode 64px sticky + scroll-aware backdrop-blur + ThemeAwareLogo + 5 nav links + ghost Sign-in + primary Get-started**

```tsx
"use client";

import * as React from "react";
import Link from "next/link";
import { ThemeAwareLogo } from "@/components/ThemeAwareLogo";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Button } from "@/components/ui/Button";

const NAV = [
  { label: "Features",   href: "#features" },
  { label: "How it works", href: "#how-it-works" },
  { label: "Agents",     href: "#agents" },
  { label: "Pricing",    href: "#pricing" },
  { label: "Docs",       href: "#docs" },
];

export default function Navbar() {
  const [scrolled, setScrolled] = React.useState(false);
  React.useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 8);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);
  return (
    <header
      className={`sticky top-0 z-40 h-16 backdrop-blur-md transition-colors ${
        scrolled
          ? "bg-[color-mix(in_srgb,var(--color-background)_88%,transparent)] border-b border-[var(--color-border)]"
          : "bg-transparent"
      }`}
    >
      <div className="mx-auto flex h-full max-w-[1200px] items-center justify-between px-6">
        <Link href="/" aria-label="NEXIS home"><ThemeAwareLogo /></Link>
        <nav className="hidden md:flex items-center gap-6 text-sm text-[var(--color-muted-foreground)]">
          {NAV.map(n => (
            <Link key={n.href} href={n.href} className="hover:text-[var(--color-foreground)] transition-colors">{n.label}</Link>
          ))}
        </nav>
        <div className="flex items-center gap-2">
          <ThemeToggle />
          <Button variant="ghost" size="sm" asChild><Link href="/sign-in">Sign in</Link></Button>
          <Button size="sm" asChild><Link href="#waitlist">Get started</Link></Button>
        </div>
      </div>
    </header>
  );
}
```

- [ ] **Step 3: Smoke check — restart dev, verify navbar renders without errors and theme toggle flips palette**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && pnpm --filter @nexis/web dev &
SERVER_PID=$!; sleep 8
curl -fs http://localhost:3000 | grep -q "NEXIS" && echo "✓ navbar HTML present"
kill $SERVER_PID 2>/dev/null
```

---

### Task 7.2: Re-skin Hero section

**Files:**
- Modify: `apps/web/components/sections/Hero.tsx`
- Modify: `apps/web/lib/content.ts` (update copy per §4.1 hero spec)

- [ ] **Step 1: Update content**

In `apps/web/lib/content.ts`, replace the `hero` block:

```ts
hero: {
  preHeading: "AUTONOMOUS ENGINEERING, SUPERVISED BY YOU.",
  heading: "Nine AI agents. One engineering team that ships fixes while you sleep.",
  subheading: "Detect → diagnose → patch → validate → approve → deploy. Closed-loop fault recovery for CI/CD and production pipelines, with the engineer always in the loop on what matters.",
  ctaPrimary: "Get started",
  ctaSecondary: "See how it works →",
  socialProof: "For SRE and platform teams who are done with 3am pages.",
  terminalTitle: "nexis.sh",
  terminalLines: [
    "[00:02:14] sentinel › anomaly detected — pipeline: etl_orders",
    "[00:02:15] pathfinder › traversing dependency graph...",
    "[00:02:17] pathfinder › root cause: schema drift on orders.total_amount (float → string)",
    "[00:02:18] synthesiser › generating candidate patches...",
    "[00:02:21] synthesiser › patch_001 ready — cast coercion + downstream migration",
    "[00:02:22] validator › deploying to shadow pipeline...",
    "[00:02:29] validator › 2,847 property tests passed. 0 failures.",
    "[00:02:30] nexis › patch awaiting approval ✓",
  ],
},
```

- [ ] **Step 2: Read existing Hero.tsx**

```bash
cat /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/sections/Hero.tsx
```

- [ ] **Step 3: Re-skin** — replace hardcoded dark colors with CSS-variable-driven classes:
  - `bg-background` / `bg-surface` → `bg-[var(--color-background)]`
  - `text-text-primary` → `text-[var(--color-foreground)]`
  - `text-text-secondary` → `text-[var(--color-muted-foreground)]`
  - `bg-primary` → `bg-[var(--color-primary)]`

> Detail-level edits depend on what's in the file. Pattern: every Tailwind utility referencing `--color-text-*`, `--color-background`, `--color-surface`, `--color-border`, `--color-primary*` from the OLD palette gets remapped to the new variable names defined in T6.2's `globals.css`.

- [ ] **Step 4: Verify visual rendering manually**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && pnpm --filter @nexis/web dev &
SERVER_PID=$!; sleep 8
echo "Open https://localhost:3000 in browser, confirm Hero is light-mode + readable"
sleep 30; kill $SERVER_PID
```

---

### Task 7.3: Re-skin TrustedStrip + Problem

**Files:**
- Create: `apps/web/components/sections/TrustedStrip.tsx`
- Modify: `apps/web/components/sections/Problem.tsx`

- [ ] **Step 1: `TrustedStrip.tsx` is new (per §4.1 section 3) — 56px muted band with grayscale logos placeholder**

```tsx
import { content } from "@/lib/content";

const PLACEHOLDERS = [
  "Acme Co.", "Sandwell", "Northpoint", "Veritas", "Halcyon", "Brightline",
];

export default function TrustedStrip() {
  return (
    <section aria-label="Trusted by teams" className="bg-[var(--color-muted)] py-6">
      <div className="mx-auto max-w-[1200px] px-6">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)] mb-4 text-center">
          Trusted by engineering teams that hate alert fatigue
        </p>
        <ul className="flex flex-wrap items-center justify-center gap-8 grayscale opacity-70">
          {PLACEHOLDERS.map(name => (
            <li key={name} className="text-sm font-semibold text-[var(--color-muted-foreground)]">{name}</li>
          ))}
        </ul>
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Re-skin `Problem.tsx`** following same color-token swap pattern as Hero.

---

### Task 7.4: Re-skin HowItWorks (full-bleed pipeline section)

**Files:**
- Modify: `apps/web/components/sections/HowItWorks.tsx`

- [ ] **Step 1: Read + re-skin**

Same color-token swap pattern. Add `id="how-it-works"` to the section root. Keep the 6-step horizontal layout with the existing motion-aware glow token traversal — just change colors from blue-on-navy to navy-on-light with brand-blue accents.

---

### Task 7.5: Re-skin Agents grid (3×3, 9 agents)

**Files:**
- Modify: `apps/web/components/sections/Agents.tsx`
- Modify: `apps/web/lib/content.ts` (replace 2-layer model with 9-agent list per §4.1)

- [ ] **Step 1: Update `content.ts.agents`** — list all 9 agents with role + Owns/Does-not-own per §2.2 + §4.1

```ts
agents: {
  label: "THE FLEET",
  headline: "Nine specialised agents. One closed loop.",
  intro: "Each agent owns one job in the pipeline. Hand-offs are typed, retried, and audit-logged.",
  list: [
    { id: "sentinel",     role: "Detection",       owns: "Anomaly detection from Sentry/OTel",          notOwns: "Graph traversal" },
    { id: "pathfinder",   role: "Diagnosis",       owns: "Causal RCA via Neo4j + DoWhy",                notOwns: "Patch synthesis" },
    { id: "synthesiser",  role: "Patch generation", owns: "LLM patch synthesis + retrieval",            notOwns: "Validation" },
    { id: "architect",    role: "Plan",            owns: "Solution plan against contracts",            notOwns: "Code emission" },
    { id: "backend",      role: "Codegen",         owns: "Backend patch synthesis",                    notOwns: "DB migrations" },
    { id: "qa",           role: "Test gen",        owns: "Unit + property test generation",            notOwns: "Sandbox execution" },
    { id: "devops",       role: "Pipeline",        owns: "ArgoCD / GH Actions YAML changes",           notOwns: "App code" },
    { id: "data engineer", role: "Migrations",     owns: "Schema migrations + data backfill",          notOwns: "API layer" },
    { id: "approval gate", role: "Routing",        owns: "Severity routing + audit log",               notOwns: "Patch decisions" },
  ],
},
```

- [ ] **Step 2: Re-skin Agents.tsx to render a 3×3 grid (md:grid-cols-3) of cards** — each card shows id, role, owns, doesn't-own. Hover: `shadow-md` with `ring-1 ring-[var(--color-accent)]`. Add `id="agents"` on the section root.

---

### Task 7.6: LiveDemoEmbed (placeholder per §4.1 section 7)

**Files:**
- Create: `apps/web/components/sections/LiveDemoEmbed.tsx`

- [ ] **Step 1:** Build a static three-pane placeholder with scenario picker (left), animated canvas (center), terminal log (right). Real SSE wiring is Phase 6.

```tsx
"use client";

import { Button } from "@/components/ui/Button";

export default function LiveDemoEmbed() {
  return (
    <section id="live-demo" className="bg-[var(--color-background)] py-24">
      <div className="mx-auto max-w-[1200px] px-6">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)] mb-3 text-center">LIVE PIPELINE</p>
        <h2 className="text-center text-3xl md:text-4xl font-semibold tracking-tight text-[var(--color-foreground)] mb-10">
          Run a synthetic incident.
        </h2>
        <div className="grid grid-cols-1 lg:grid-cols-[240px_1fr_400px] gap-4 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4 shadow-sm">
          <aside className="rounded-md bg-[var(--color-muted)] p-4">
            <h3 className="text-sm font-semibold mb-3 text-[var(--color-foreground)]">Scenarios</h3>
            <ul className="space-y-1 text-sm text-[var(--color-muted-foreground)]">
              {["Schema drift", "Null deref", "OOM", "Migration failure"].map(s => (
                <li key={s} className="rounded px-2 py-1 hover:bg-[var(--color-card)] cursor-pointer">{s}</li>
              ))}
            </ul>
          </aside>
          <div className="min-h-[280px] rounded-md bg-[var(--color-muted)] grid place-items-center text-sm text-[var(--color-muted-foreground)]">
            Pipeline canvas (Phase 6 wiring)
          </div>
          <aside className="rounded-md bg-[var(--color-foreground)] text-[var(--color-background)] font-mono text-xs p-4 overflow-auto">
            <div>$ nexis demo --scenario=schema-drift</div>
            <div className="opacity-60">[idle — click Run]</div>
          </aside>
        </div>
        <div className="mt-6 text-center">
          <Button>Run a synthetic incident</Button>
        </div>
      </div>
    </section>
  );
}
```

---

### Task 7.7: Re-skin Metrics strip

**Files:**
- Modify: `apps/web/components/sections/Metrics.tsx`
- Modify: `apps/web/lib/content.ts.metrics`

- [ ] **Step 1: Update content per §4.1 — three numbers:**

```ts
metrics: {
  label: "OUTCOMES",
  headline: "Pipeline recovery you can measure.",
  stats: [
    { prefix: "MTTR ↓ ", number: 60, suffix: "%", label: "vs. manual response baseline" },
    { prefix: "≥ ",      number: 80, suffix: "%", label: "patch correctness rate" },
    { prefix: "< ",      number: 90, suffix: "s", label: "to fault detection" },
  ],
},
```

- [ ] **Step 2: Re-skin** with display-weight numbers in light navy, count-up animation on `whileInView`. Add `id="metrics"` on the section root.

---

### Task 7.8: Final CTA + Footer (sections 9 + footer)

**Files:**
- Modify: `apps/web/components/sections/CTA.tsx`
- Modify: `apps/web/components/layout/Footer.tsx`
- Modify: `apps/web/lib/content.ts.cta` and `.footer`

- [ ] **Step 1: Update CTA copy**

```ts
cta: {
  headline: "Give your team back its weekends.",
  sub: "NEXIS runs an autonomous incident-response loop with the engineer in the loop on what matters. Approve once. Sleep through the rest.",
  button: "Request access",
},
```

- [ ] **Step 2: Re-skin CTA centered** with `id="cta"` on the section.

- [ ] **Step 3: Re-skin Footer** — 5 columns + status badge + "SOC 2 in progress" pill. Light tokens.

---

### Task 7.9: Smoke-test the full landing

- [ ] **Step 1:** `pnpm --filter @nexis/web dev` then visually verify all 9 sections render in light mode, no console errors, theme toggle flips correctly.

- [ ] **Step 2:** `pnpm --filter @nexis/web build` succeeds.

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && \
pnpm --filter @nexis/web typecheck && \
pnpm --filter @nexis/web build
```

Expected: zero TS errors, build completes, `.next/standalone/apps/web/server.js` exists.

- [ ] **Step 3: Commit**

```bash
git -C /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app add -A && \
git -C /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app commit -m "feat(web): light-mode landing — 9 sections per §4.1, ThemeAwareLogo, shadcn primitives, next-themes

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Stage 8 — Database baseline (Drizzle for web; sqlc for control-plane)

### Task 8.1: packages/db Drizzle schema

**Files:**
- Create: `packages/db/package.json`, `packages/db/schema.ts`, `packages/db/index.ts`, `apps/web/drizzle.config.ts`, `apps/web/lib/db.ts`

- [ ] **Step 1: `packages/db/package.json`**

```json
{
  "name": "@nexis/db",
  "version": "0.0.0",
  "private": true,
  "main": "./index.ts",
  "types": "./index.ts",
  "dependencies": {
    "drizzle-orm": "^0.36.4",
    "postgres": "^3.4.5"
  }
}
```

- [ ] **Step 2: `packages/db/schema.ts`** — bare baseline tables (full org/user/RBAC schema from §3.2 lands in Phase 2)

```ts
import { pgTable, uuid, text, timestamp, jsonb, index } from "drizzle-orm/pg-core";

export const organizations = pgTable("organizations", {
  id:        uuid("id").primaryKey().defaultRandom(),
  name:      text("name").notNull(),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const users = pgTable("users", {
  id:        uuid("id").primaryKey().defaultRandom(),
  email:     text("email").notNull().unique(),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const auditLog = pgTable("audit_log", {
  id:        uuid("id").primaryKey().defaultRandom(),
  orgId:     uuid("org_id"),
  actor:     text("actor"),
  action:    text("action").notNull(),
  target:    text("target"),
  metadata:  jsonb("metadata"),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  orgCreatedIdx: index("audit_log_org_created_idx").on(t.orgId, t.createdAt),
}));

export const waitlist = pgTable("waitlist", {
  id:        uuid("id").primaryKey().defaultRandom(),
  email:     text("email").notNull().unique(),
  source:    text("source"),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});
```

- [ ] **Step 3: `packages/db/index.ts`**

```ts
export * from "./schema";
```

- [ ] **Step 4: `apps/web/drizzle.config.ts`**

```ts
import type { Config } from "drizzle-kit";
export default {
  schema: "../../packages/db/schema.ts",
  out: "../../packages/db/migrations",
  dialect: "postgresql",
  dbCredentials: { url: process.env.DATABASE_URL ?? "postgres://nexis:nexis_dev_password@localhost:5432/nexis" },
} satisfies Config;
```

- [ ] **Step 5: Add `@nexis/db` to `apps/web/package.json` dependencies**

Add to dependencies block:
```json
"@nexis/db": "workspace:*",
```

Re-run `pnpm install`.

- [ ] **Step 6: `apps/web/lib/db.ts`**

```ts
import { drizzle } from "drizzle-orm/postgres-js";
import postgres from "postgres";
import * as schema from "@nexis/db";

const connectionString = process.env.DATABASE_URL;
if (!connectionString) throw new Error("DATABASE_URL is not set");

const queryClient = postgres(connectionString, { prepare: false });
export const db = drizzle(queryClient, { schema });
export { schema };
```

- [ ] **Step 7: Generate the initial migration**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && \
pnpm exec drizzle-kit generate --name=init
```

Expected: `packages/db/migrations/0000_init.sql` is created. Inspect it.

- [ ] **Step 8: Apply migration to running postgres**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && docker compose up -d postgres && sleep 5 && \
DATABASE_URL=postgres://nexis:nexis_dev_password@localhost:5432/nexis \
  pnpm --filter @nexis/web exec drizzle-kit migrate
```

Expected: tables created. Verify:

```bash
docker compose exec -T postgres psql -U nexis -d nexis -c "\dt"
```

Expected: `organizations`, `users`, `audit_log`, `waitlist`.

---

### Task 8.2: Waitlist API route (TDD with Vitest)

**Files:**
- Create: `apps/web/app/api/waitlist/route.ts`, `apps/web/tests/waitlist.test.ts`, `apps/web/vitest.config.ts`

- [ ] **Step 1: `apps/web/vitest.config.ts`**

```ts
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    globals: true,
  },
  resolve: {
    alias: {
      "@": new URL("./", import.meta.url).pathname,
      "@nexis/db": new URL("../../packages/db/index.ts", import.meta.url).pathname,
    },
  },
});
```

- [ ] **Step 2: Write failing test (Vitest, mocks the db module)**

`apps/web/tests/waitlist.test.ts`:

```ts
import { describe, it, expect, vi } from "vitest";

vi.mock("@/lib/db", () => {
  const calls: any[] = [];
  return {
    db: {
      insert: () => ({
        values: (v: any) => ({
          onConflictDoNothing: () => ({ returning: async () => { calls.push(v); return [{ id: "uuid-x", email: v.email }]; } }),
        }),
      }),
    },
    schema: { waitlist: { name: "waitlist" } },
    __calls: calls,
  };
});

import { POST } from "@/app/api/waitlist/route";

describe("POST /api/waitlist", () => {
  it("rejects missing email", async () => {
    const res = await POST(new Request("http://x/api/waitlist", { method: "POST", body: "{}" }));
    expect(res.status).toBe(400);
  });

  it("accepts a valid email and persists", async () => {
    const res = await POST(new Request("http://x/api/waitlist", {
      method: "POST",
      body: JSON.stringify({ email: "user@example.com", source: "landing" }),
    }));
    expect(res.status).toBe(201);
    const body = await res.json();
    expect(body.email).toBe("user@example.com");
  });
});
```

- [ ] **Step 3: Run — fails (handler doesn't exist)**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && pnpm --filter @nexis/web test
```

- [ ] **Step 4: Implement `apps/web/app/api/waitlist/route.ts`**

```ts
import { NextResponse } from "next/server";
import { db, schema } from "@/lib/db";

export async function POST(req: Request) {
  let body: { email?: unknown; source?: unknown };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: "invalid JSON" }, { status: 400 });
  }

  const email = typeof body.email === "string" ? body.email.trim().toLowerCase() : "";
  if (!email || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    return NextResponse.json({ error: "valid email required" }, { status: 400 });
  }
  const source = typeof body.source === "string" ? body.source.slice(0, 64) : null;

  const rows = await db
    .insert(schema.waitlist)
    .values({ email, source })
    .onConflictDoNothing()
    .returning();

  const row = rows[0] ?? { email };
  return NextResponse.json({ email: row.email }, { status: 201 });
}
```

- [ ] **Step 5: Run — passes**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && pnpm --filter @nexis/web test
```

Expected: 2 tests pass.

- [ ] **Step 6: Verify against real Postgres (acceptance criterion)**

```bash
docker compose up -d postgres && sleep 4 && \
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && \
DATABASE_URL=postgres://nexis:nexis_dev_password@localhost:5432/nexis \
  pnpm --filter @nexis/web dev &
PID=$!; sleep 8
curl -s -X POST http://localhost:3000/api/waitlist \
  -H "content-type: application/json" \
  -d '{"email":"verify@example.com","source":"phase1-acceptance"}'; echo
docker compose exec -T postgres psql -U nexis -d nexis -c "SELECT email, source FROM waitlist WHERE email='verify@example.com';"
kill $PID 2>/dev/null
```

Expected: row visible with `email = verify@example.com`, `source = phase1-acceptance`.

---

### Task 8.3: sqlc baseline for control-plane

**Files:**
- Create: `services/control-plane/sqlc.yaml`, `services/control-plane/migrations/0001_init.up.sql`, `services/control-plane/migrations/0001_init.down.sql`

- [ ] **Step 1: `services/control-plane/sqlc.yaml`**

```yaml
version: "2"
sql:
  - schema: "migrations"
    queries: "internal/adapter/repo/queries"
    engine: "postgresql"
    gen:
      go:
        package: "repo"
        out: "internal/adapter/repo"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_pointers_for_null_types: true
```

- [ ] **Step 2: Migration mirroring the Drizzle baseline (so both layers agree)**

`services/control-plane/migrations/0001_init.up.sql`:

```sql
CREATE TABLE IF NOT EXISTS organizations (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name        text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email       text NOT NULL UNIQUE,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS audit_log (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      uuid,
  actor       text,
  action      text NOT NULL,
  target      text,
  metadata    jsonb,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_log_org_created_idx ON audit_log (org_id, created_at);
```

`services/control-plane/migrations/0001_init.down.sql`:

```sql
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS organizations;
```

- [ ] **Step 3: Stub query file so sqlc has something to generate (real queries land in Phase 2)**

```bash
mkdir -p /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/internal/adapter/repo/queries
```

`services/control-plane/internal/adapter/repo/queries/organizations.sql`:

```sql
-- name: GetOrganization :one
SELECT id, name, created_at FROM organizations WHERE id = $1;
```

- [ ] **Step 4: Generate**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && sqlc generate
```

Expected: files appear under `internal/adapter/repo/`. `go build ./...` still succeeds.

---

## Stage 9 — Full stack docker-compose (Phase 1 — plain HTTP, no Caddy)

> **Note:** Caddy + mkcert + `*.nexis.local` HTTPS routing is Phase 2 work (lands with WorkOS auth, where `Secure` cookies + OAuth callbacks need real HTTPS). Phase 1 ships everything on plain HTTP through host port mappings.

### Task 9.1: Add app services (web / control-plane / validator / gitops) to `docker-compose.yml`

**Files:**
- Modify: `docker-compose.yml` (append app services + their host port mappings)

- [ ] **Step 1: Append to the `services:` section of `docker-compose.yml`**

```yaml
  web:
    build:
      context: .
      dockerfile: apps/web/Dockerfile
    environment:
      DATABASE_URL: postgres://nexis:nexis_dev_password@postgres:5432/nexis?sslmode=disable
      NEXT_PUBLIC_APP_URL: http://localhost:3000
      NEXT_PUBLIC_API_URL: http://localhost:8080
      NODE_ENV: production
      PORT: "3000"
    ports:
      - "3000:3000"
    depends_on:
      postgres:
        condition: service_healthy
    networks: [nexis]

  control-plane:
    build:
      context: services/control-plane
      dockerfile: Dockerfile
    environment:
      PORT: "8080"
      APP_ENV: dev
      LLM_PROVIDER: ${LLM_PROVIDER:-openai}
      OPENAI_API_KEY: ${OPENAI_API_KEY:-}
      OPENAI_MODEL_SYNTHESIS: ${OPENAI_MODEL_SYNTHESIS:-gpt-4o}
      OPENAI_MODEL_CHEAP: ${OPENAI_MODEL_CHEAP:-gpt-4o-mini}
      OLLAMA_BASE_URL: ${OLLAMA_BASE_URL:-http://host.docker.internal:11434}
      OLLAMA_MODEL_GENERAL: ${OLLAMA_MODEL_GENERAL:-llama3.1:8b}
      DATABASE_URL: postgres://nexis:nexis_dev_password@postgres:5432/nexis?sslmode=disable
      OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4318
    ports:
      - "8080:8080"
    depends_on:
      postgres:
        condition: service_healthy
      otel-collector: { condition: service_started }
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/healthz"]
      interval: 5s
      timeout: 3s
      retries: 10
    extra_hosts:
      - "host.docker.internal:host-gateway"
    networks: [nexis]

  validator:
    build:
      context: services/validator
      dockerfile: Dockerfile
    environment:
      PORT: "8081"
    ports:
      - "8081:8081"
    networks: [nexis]

  gitops:
    build:
      context: services/gitops
      dockerfile: Dockerfile
    environment:
      PORT: "8082"
    ports:
      - "8082:8082"
    networks: [nexis]
```

> **Port map summary** (all `host:container`):
> - 3000 → web
> - 8080 → control-plane (API)
> - 8081 → validator
> - 8082 → gitops
> - 3001 → grafana (host 3001 because grafana's container port is 3000, conflicts with web)
> - 8025 → mailhog UI
> - 9001 → minio console
> - 8233 → temporal-ui (mapped from container 8080 to avoid host 8080 collision with control-plane)
> - 7474, 7687 → neo4j browser + bolt
> - 5432 → postgres
> - 6379 → redis
> - 7233 → temporal frontend (gRPC)
> - 4317, 4318 → otel-collector
> - 9090 → prometheus
> - 3100 → loki
> - 3200 → tempo

- [ ] **Step 2: Bring the full stack up**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && \
docker compose up --build -d
```

- [ ] **Step 3: Wait + check health**

```bash
sleep 60 && docker compose ps --format "table {{.Service}}\t{{.Status}}"
```

Expected: every service `running` or `healthy`. First build is slow (~5–10 min); subsequent runs hit layer cache.

---

### Task 9.2: Acceptance smoke tests (Phase 1 success criteria, plain HTTP)

**Files:** none (verification only)

- [ ] **Step 1: `http://localhost:3000` returns 200**

```bash
curl -fsI http://localhost:3000 | head -1
```

Expected: `HTTP/1.1 200 OK`.

- [ ] **Step 2: `http://localhost:8080/healthz` returns service ok**

```bash
curl -fs http://localhost:8080/healthz
```

Expected: `{"service":"control-plane","status":"ok"}`.

- [ ] **Step 3: LLM diag returns OpenAI default**

```bash
curl -fs http://localhost:8080/v1/_diag/llm
```

Expected: `{"provider":"openai","models":["gpt-4o-mini"],...}`.

- [ ] **Step 4: Switch to Ollama and re-check (the headline acceptance)**

```bash
LLM_PROVIDER=ollama OLLAMA_BASE_URL=http://host.docker.internal:11434 \
  docker compose up -d --no-deps control-plane && \
sleep 8 && \
curl -fs http://localhost:8080/v1/_diag/llm
```

Expected: `{"provider":"ollama","base_url":"http://host.docker.internal:11434","models":["llama3.1:8b"]}`.

- [ ] **Step 5: Waitlist persists end-to-end**

```bash
curl -s -X POST http://localhost:3000/api/waitlist \
  -H "content-type: application/json" \
  -d '{"email":"phase1-final@example.com","source":"acceptance"}'; echo
docker compose exec -T postgres psql -U nexis -d nexis \
  -c "SELECT email, source FROM waitlist WHERE email='phase1-final@example.com';"
```

Expected: API returns 201 + DB row present.

- [ ] **Step 6: Lighthouse run on landing**

```bash
npx --yes lighthouse http://localhost:3000 \
  --quiet \
  --chrome-flags="--headless" \
  --only-categories=performance,accessibility \
  --output=json --output-path=/tmp/lh.json && \
node -e "const r=require('/tmp/lh.json');console.log({perf:r.categories.performance.score*100, a11y:r.categories.accessibility.score*100})"
```

Expected: `perf ≥ 92`, `a11y ≥ 95`. If below, iterate (image sizing, font preload, color-contrast). Re-run.

- [ ] **Step 7: Stack came up in < 60s on warm cache**

```bash
docker compose down && \
START=$(date +%s) && docker compose up -d && \
until [ "$(docker compose ps --format json | python3 -c 'import sys,json; rows=[json.loads(l) for l in sys.stdin if l.strip()]; print(all(r["State"]=="running" for r in rows))')" = "True" ]; do sleep 2; done && \
END=$(date +%s) && echo "boot: $((END-START))s"
```

Expected: `boot: < 60s` on warm Docker layer cache.

- [ ] **Step 8: Commit acceptance**

```bash
git -C /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app add -A && \
git -C /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app commit -m "feat: Phase 1 acceptance — full stack via docker compose, LLM provider switchable, waitlist persists, Lighthouse passes

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Stage 10 — CI

### Task 10.1: GitHub Actions — lint + typecheck + test + docker build on PR

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: `.github/workflows/ci.yml`**

```yaml
name: ci

on:
  pull_request:
  push:
    branches: [main]

concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true

jobs:
  web:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: pnpm/action-setup@v4
        with: { version: 9.12.0 }
      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: pnpm
      - run: pnpm install --frozen-lockfile
      - run: pnpm --filter @nexis/web typecheck
      - run: pnpm --filter @nexis/web lint
      - run: pnpm --filter @nexis/web test
      - run: pnpm --filter @nexis/web build

  go-services:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        service: [control-plane, validator, gitops]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache-dependency-path: services/${{ matrix.service }}/go.sum
      - working-directory: services/${{ matrix.service }}
        run: |
          go mod download
          go vet ./...
          go test -race -timeout 120s ./...
          go build ./...

  docker:
    runs-on: ubuntu-latest
    needs: [web, go-services]
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-buildx-action@v3
      - name: Build all images
        run: |
          docker build -t nexis/web:ci         -f apps/web/Dockerfile .
          docker build -t nexis/control-plane:ci services/control-plane
          docker build -t nexis/validator:ci    services/validator
          docker build -t nexis/gitops:ci       services/gitops
```

- [ ] **Step 2:** Push to a feature branch in a remote and verify the workflow turns green. (Skip if no remote yet; CI lives in the repo and will activate when origin is configured.)

- [ ] **Step 3: Commit**

```bash
git -C /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app add .github/workflows/ci.yml && \
git -C /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app commit -m "ci: lint+typecheck+test+docker-build on PR

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Phase 1 Definition of Done

- [ ] `docker compose up -d` brings every service `healthy` in < 60s on warm cache
- [ ] `http://localhost:3000` renders the 9-section landing in light mode
- [ ] Theme toggle flips to dark (and back) without layout flash
- [ ] `http://localhost:8080/healthz` returns `{"service":"control-plane","status":"ok"}`
- [ ] `LLM_PROVIDER=openai`: `/v1/_diag/llm` returns `provider=openai`
- [ ] `LLM_PROVIDER=ollama`: `/v1/_diag/llm` returns `provider=ollama` + `models` includes `llama3.1:8b`
- [ ] `POST /api/waitlist` persists a row visible via `psql`
- [ ] Lighthouse perf ≥ 92, a11y ≥ 95
- [ ] All Go tests pass (`go test ./...` per service)
- [ ] All web tests pass (`pnpm --filter @nexis/web test`)
- [ ] `pnpm --filter @nexis/web build` succeeds
- [ ] `go-arch-lint check` passes inside `services/control-plane/`
- [ ] CI workflow file is present at `.github/workflows/ci.yml`
- [ ] Append `Completed YYYY-MM-DD` to `docs/PROJECT_PLAN.md` Phase 1 heading
