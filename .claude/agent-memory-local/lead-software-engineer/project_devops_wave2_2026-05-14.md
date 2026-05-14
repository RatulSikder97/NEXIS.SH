---
name: DevOps Wave 2 shipped
description: 2026-05-14 production-readiness batch — graceful shutdown, CI cache+trivy, compose profiles, log rotation, DB backup scripts, distroless healthcheck via --healthcheck flag.
type: project
---

DevOps audit at `docs/audits/2026-05-14-devops-audit.md`. Wave 2 (this batch) landed:

- Graceful shutdown in `services/control-plane/cmd/server/main.go`: signal goroutine cancels a shared `appCtx` (drives Temporal worker + Sentinel + cron) then runs `httpServer.Shutdown` with a 30s deadline. Main goroutine blocks on `ListenAndServe`.
- `--healthcheck` flag on the control-plane binary (dials `http://127.0.0.1:$PORT/healthz` with a 3s timeout, exits 0/1). Re-enables in-container HEALTHCHECKs on the distroless image without shipping curl.
- `docker-compose.yml`: `x-logging` anchor (50m/5 file rotation) applied to every long-running service. Profiles added: default = slim stack, `observability` = prometheus/grafana/loki/tempo/otel-collector, `agents` = validator/gitops/causal-inference. Validator gets curl-based healthcheck (its image has python+curl); gitops still has none because its main.go was out-of-scope (would have needed the same `--healthcheck` flag added).
- `.github/workflows/ci.yml`: buildx gha cache per-image-scope, trivy scan failing on Critical/High (ignore-unfixed), ECR push staged behind `if: github.ref == 'refs/heads/main' && env.AWS_ROLE_ARN != ''`, pnpm + go module caching, new `migrations` job spinning up pg16+pgvector and replaying up→down→up sweeps.
- `scripts/backup_db.sh` + `scripts/restore_db.sh`: pg_dump/psql via `docker compose exec`, gzipped output in `backups/`, 30-day retention, `restore_db.sh latest` convenience.
- README expanded with profile usage matrix + backup/restore section.

**Why:** the audit graded 3 Critical + 11 High + 12 Medium + 8 Low. Wave 1 took the top 8; Wave 2 burned down 6 more (F-4, F-6, F-12, F-22, F-26, plus the new backup script + profiles + healthcheck binary that the audit's Phase-7 checklist explicitly listed).

**How to apply:**
- New long-running compose services need `<<: *default-logging` or they'll re-introduce the unbounded-log-disk-fill bug (F-6).
- Distroless Go services that want an in-container HEALTHCHECK should follow the `--healthcheck` flag pattern in `cmd/server/main.go::runHealthcheck` — same binary, no curl/wget needed.
- Hard `depends_on` edges between a default-profile service and a profile-gated service will break `docker compose up` without that profile active. Use soft references (env-var endpoint, runtime tolerance) instead.
- `docker compose --env-file .env config --quiet` is the parse-validation command of record; always run with each profile combo when editing the compose file.
