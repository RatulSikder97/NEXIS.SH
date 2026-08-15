# NEXIS — single-VM deployment

Target: `65.0.31.84` (AWS **Lightsail**, ap-south-1, Ubuntu 24.04, 2 vCPU / 3.7 GiB / 77 GB).
Instance metadata reports `t3.medium` / `i-06856d23f979632b0` because Lightsail
runs on EC2 underneath — but the firewall is Lightsail's, not an EC2 security group.
Domain: `nexis.sh`. App root: `/opt/nexis`.

## Topology

One Caddy container holds the only public listener. Everything else binds to
`127.0.0.1` and is reachable only over the Docker bridge network or an SSH tunnel.

```
internet ──▶ :443 caddy ──┬─ /v1/*    ──▶ control-plane:8080
                          ├─ /healthz ──▶ control-plane:8080
                          └─ /*       ──▶ web:3000
```

**Routing is same-origin by requirement, not preference.** Three things in the
codebase break under split origins (`app.` / `api.`):

1. `apps/web/app/(auth)/sign-in/page.tsx` builds the WorkOS redirect from
   `window.location.origin` — not a configurable API host.
2. The same page sets the `nexis_oauth_state` CSRF cookie via `document.cookie`
   on the web origin; `handler/auth_oauth.go` reads it on the API origin.
3. The session cookie is `SameSite=Lax` (`handler/auth.go`) with no
   `SameSite=None` code path, so it would stop riding cross-site fetches.

Same-origin also makes `middleware/cors.go` a no-op, which its own doc comment
describes as the intended production mode.

## Prerequisites (outside the box)

| # | Requirement | Status |
|---|---|---|
| 1 | `nexis.sh` A record → `65.0.31.84`, Cloudflare proxy **OFF** (grey cloud) | must be set |
| 2 | Lightsail instance firewall: inbound TCP **80** and **443** | must be opened |

Until both land, Caddy cannot complete an ACME HTTP-01 challenge, so there is no
certificate and `https://nexis.sh` will not resolve to this box. The `:80`
catch-all block in `deploy/Caddyfile` serves the stack over plain HTTP on the raw
IP in the meantime; delete that block once TLS is live.

`ufw` is inactive and `iptables` is empty on the host — the block is entirely the
Lightsail firewall, which lives in the AWS control plane. **It cannot be changed
from inside the instance over SSH.** Either:

*Console:* Lightsail → the instance → **Networking** → **IPv4 Firewall** →
*Add rule* → HTTP (80) and HTTPS (443).

*CLI* (needs `lightsail:GetInstances`, `lightsail:GetInstancePortStates`,
`lightsail:OpenInstancePublicPorts`):

```bash
export AWS_DEFAULT_REGION=ap-south-1
NAME=$(aws lightsail get-instances \
  --query "instances[?publicIpAddress=='65.0.31.84'].name | [0]" --output text)
for P in 80 443; do
  aws lightsail open-instance-public-ports --instance-name "$NAME" \
    --port-info "fromPort=$P,toPort=$P,protocol=TCP"
done
aws lightsail get-instance-port-states --instance-name "$NAME" --output table
```

Verify from off-box with `nc -z 65.0.31.84 80 && nc -z 65.0.31.84 443`.

## Layout

| Path | Purpose |
|---|---|
| `/opt/nexis` | source tree (rsynced, no `.git`) |
| `/opt/nexis/.env` | generated secrets, mode `0600` |
| `/opt/nexis/secrets/github-app.pem` | GitHub App private key, mode `0600` |
| `deploy/Caddyfile` | edge config |
| `docker-compose.prod.yml` | production overlay |

## Commands

Always layer both compose files; the overlay alone is incomplete.

```bash
cd /opt/nexis
F="-f docker-compose.yml -f docker-compose.prod.yml"
P="--profile agents --profile observability"

docker compose $F $P up -d          # start everything
docker compose $F $P ps             # status
docker compose $F $P logs -f control-plane
docker compose $F $P build web      # rebuild after a code change
docker compose $F $P down           # stop (volumes survive)
```

**Rebuild the `web` image after any change to `APP_BASE_URL`.** Next.js inlines
`NEXT_PUBLIC_*` into the client bundle at compile time — the values are passed as
build args in `docker-compose.prod.yml`, and changing the runtime env alone has
no effect on an already-compiled bundle.

Migrations need no separate step: the control-plane applies
`services/control-plane/migrations/*.up.sql` at boot via its embedded runner
(`internal/platform/migrate`). With `APP_ENV=prod` a migration failure is fatal.

## Configuration notes

**`APP_ENV=prod` + `ALLOW_SELF_HOSTED=1`.** The session cookie sets `Secure` when
`AppEnv != "dev"`, but `Config.FatalIfLocalInCloud` aborts boot when `AppEnv` is
`staging`/`prod` while any provider selector is still local — a self-hosted box
legitimately has all of them local. `ALLOW_SELF_HOSTED` was added to opt out
explicitly. Without it the only way to get `Secure` cookies without a boot abort
is to spell `APP_ENV` as `"production"`, which works only because the guard
matches the exact strings `staging`/`prod` while the cookie check tests `!= "dev"`.
That is an accident; do not rely on it.

**Rotated credentials:** the `nexis` Postgres superuser, Neo4j, MinIO, and every
app secret (`SESSION_SECRET`, `AUDIT_SECRET`, `MASTER_KEY`, `GITHUB_WEBHOOK_SECRET`,
`VALIDATOR_TOKEN`, `GITOPS_TOKEN`, `DEPLOY_ENGINE_TOKEN`, Grafana admin).

**Not rotated:** `nexis_app` and `nexis_gitops`. Their passwords are baked into
`infra/postgres/init/01-nexis-app-role.sql` and migration
`0018_phase6_gitops_role.up.sql` respectively, and both roles are reachable only
from inside the bridge network. Changing them means editing SQL that runs once on
a fresh volume.

**Grafana** ships with anonymous Admin enabled in the base compose file. The
overlay turns that off and sets a real admin password.

## Resource reality

The declared limits in the base compose file total ~10 GiB against 3.7 GiB of
RAM. The overlay tightens every limit (Neo4j JVM heap pinned to 384m, page cache
128m) and the host carries a 12 GiB swapfile with `vm.swappiness=10`.

Running core + agents + observability on this box works but leaves little
headroom; sustained load will hit swap. The observability profile
(Prometheus/Grafana/Loki/Tempo/OTel ≈ 1 GiB) is the cheapest thing to drop —
omit `--profile observability` — and resizing to a 8 GiB instance is the real fix.

## Operations

```bash
./scripts/backup_db.sh            # pg_dump → backups/, keeps last 30
./scripts/restore_db.sh latest
free -h && docker stats --no-stream
```

Docker daemon log rotation is set globally in `/etc/docker/daemon.json`
(50 MB × 3) in addition to the per-service caps in compose.

Reach an internal UI without exposing it:

```bash
ssh -i <key> -L 8233:127.0.0.1:8233 ubuntu@65.0.31.84   # Temporal UI
ssh -i <key> -L 3030:127.0.0.1:3030 ubuntu@65.0.31.84   # Grafana
ssh -i <key> -L 8025:127.0.0.1:8025 ubuntu@65.0.31.84   # MailHog
```

## Known gaps

- **Mail is MailHog.** Nothing leaves the box, so magic links, invites, and
  password resets are only visible in the MailHog UI. Point `SMTP_HOST`/`SMTP_PORT`/
  `SMTP_FROM` at a real relay before relying on email.
- **Preview deploys return unreachable URLs.** `services/deploy-engine/internal/deploy/deploy.go`
  builds them as `http://localhost:<port>`, which is the *server's* localhost.
- **No CSP header.** HSTS, `X-Frame-Options`, `X-Content-Type-Options`, and
  `Referrer-Policy` are set in the Caddyfile; a Content-Security-Policy needs the
  app's inline-script/style behaviour audited first.
- **`X-Forwarded-For` is overwritten at the proxy**, because
  `handler/webhooks.go` reads the last hop with no trusted-proxy allowlist.

## Security follow-ups (require action outside this deploy)

These are pre-existing and were **not** fixed here — rewriting git history is
destructive and needs an explicit decision.

- `.env.bak` is **tracked in git** and contains a real 164-char OpenAI project
  key. `.gitignore` covers `.env` but not `.env.bak`.
- `secrets/github-app.pem` is **tracked in git** — it is the private key for the
  older GitHub App `nexis-bot-dev` (ID 3711190). The `secrets/*.pem` ignore rule
  was added after the file was already tracked, so it has no effect.
- `.env.native` is tracked and carries session/audit/master secrets and DB
  passwords.

The deployment uses freshly generated secrets for everything it controls, and
the **current** GitHub App (`nexis-recovery-dev`, ID 4579076) whose key is *not*
in git. The leaked OpenAI key was carried forward so LLM features work — **rotate
it**, then update `OPENAI_API_KEY` in `/opt/nexis/.env` and restart the
control-plane.

## Third-party callback URLs to update

| Integration | URL |
|---|---|
| GitHub App install callback | `https://nexis.sh/v1/integrations/github/install/callback` |
| GitHub webhook | `https://nexis.sh/v1/webhooks/github/<org_id>` |
| Slack OAuth redirect | `https://nexis.sh/v1/integrations/slack/callback` |
| Slack interactivity | `https://nexis.sh/v1/integrations/slack/interactivity` |
| Datadog webhook | `https://nexis.sh/v1/integrations/datadog/webhook?org=<org_id>` |
| PagerDuty webhook | `https://nexis.sh/v1/integrations/pagerduty/webhook?org=<org_id>` |
