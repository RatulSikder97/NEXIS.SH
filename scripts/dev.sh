#!/usr/bin/env bash
# NEXIS native dev stack — boots the whole ecosystem without Docker.
#
#   scripts/dev.sh up            boot infra + all services + web console
#   scripts/dev.sh up --no-web   everything except the Next.js console
#   scripts/dev.sh down          stop the services (leaves infra running)
#   scripts/dev.sh down --infra  stop the services AND the infra
#   scripts/dev.sh status        what is up, on which port
#   scripts/dev.sh logs <name>   tail a component's log
#
# Infra (Homebrew, native host processes): postgres 5432, redis 6379,
# neo4j 7687, minio 9000/9001, temporal 7233 (UI 8233), ollama 11434.
# Services: causal 8090, validator 8083, gitops 8082, deploy-engine 8091,
# control-plane 8080 (also hosts the Temporal worker + applies migrations
# at boot), web 3000.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_DIR="$ROOT/.run"
LOG_DIR="$RUN_DIR/logs"
BIN_DIR="$RUN_DIR/bin"
PID_DIR="$RUN_DIR/pids"
mkdir -p "$LOG_DIR" "$BIN_DIR" "$PID_DIR"

PGDATA_NEXIS="${NEXIS_PGDATA:-/opt/homebrew/var/postgresql-nexis}"
MINIO_DATA="${NEXIS_MINIO_DATA:-/opt/homebrew/var/minio}"

# The cluster is PostgreSQL 17, but `pg_ctl` on PATH is postgresql@16's on this
# machine and refuses a v17 data directory ("database files are incompatible").
# Pin the v17 bin dir explicitly, falling back to whatever is on PATH.
PGBIN="${NEXIS_PGBIN:-/opt/homebrew/opt/postgresql@17/bin}"
[ -x "$PGBIN/pg_ctl" ] || PGBIN="$(dirname "$(command -v pg_ctl)")"
PG_CTL="$PGBIN/pg_ctl"
PG_ISREADY="$PGBIN/pg_isready"

bold() { printf '\033[1m%s\033[0m\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; }
warn() { printf '  \033[33m!\033[0m %s\n' "$*"; }
die()  { printf '  \033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }

load_env() {
  set -a
  # shellcheck disable=SC1091
  . "$ROOT/.env.native"
  # Real credentials (Novita chat keys, per-agent models) — gitignored.
  # Sourced second so it wins over the tracked dev defaults.
  [ -f "$ROOT/.env.native.local" ] && . "$ROOT/.env.native.local"
  set +a
  # Each Go service reads the same PORT variable, so never inherit one.
  unset PORT
}

port_up() { lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1; }

wait_port() { # name port timeout
  local n=$1 p=$2 t=${3:-30} i=0
  while [ $i -lt "$t" ]; do
    port_up "$p" && { ok "$n listening on :$p"; return 0; }
    sleep 1; i=$((i + 1))
  done
  die "$n did not come up on :$p in ${t}s — see $LOG_DIR/$n.log"
}

wait_health() { # name port timeout
  local n=$1 p=$2 t=${3:-40} i=0
  while [ $i -lt "$t" ]; do
    if curl -fsS --max-time 2 "http://127.0.0.1:$p/healthz" >/dev/null 2>&1; then
      ok "$n healthy on :$p"; return 0
    fi
    sleep 1; i=$((i + 1))
  done
  die "$n unhealthy on :$p after ${t}s — see $LOG_DIR/$n.log"
}

running() { # name -> 0 if its pidfile points at a live process
  local f="$PID_DIR/$1.pid"
  [ -f "$f" ] && kill -0 "$(cat "$f")" 2>/dev/null
}

start_bg() { # name workdir cmd...
  local name=$1 wd=$2; shift 2
  if running "$name"; then ok "$name already running (pid $(cat "$PID_DIR/$name.pid"))"; return 0; fi
  ( cd "$wd" && nohup "$@" >>"$LOG_DIR/$name.log" 2>&1 & echo $! >"$PID_DIR/$name.pid" )
}

# ---------------------------------------------------------------- infra ----

start_infra() {
  bold "infra"

  if "$PG_ISREADY" -h 127.0.0.1 -p 5432 -q 2>/dev/null; then
    ok "postgres already up on :5432"
  else
    [ -d "$PGDATA_NEXIS" ] || die "no postgres cluster at $PGDATA_NEXIS (set NEXIS_PGDATA)"
    "$PG_CTL" -D "$PGDATA_NEXIS" -l "$LOG_DIR/postgres.log" -o "-p 5432" start >/dev/null
    for i in $(seq 1 20); do "$PG_ISREADY" -h 127.0.0.1 -p 5432 -q 2>/dev/null && break; sleep 1; done
    "$PG_ISREADY" -h 127.0.0.1 -p 5432 -q || die "postgres failed to start — see $LOG_DIR/postgres.log"
    ok "postgres started on :5432"
  fi

  if port_up 6379; then ok "redis already up on :6379"
  else redis-server --port 6379 --daemonize yes --dir "$RUN_DIR" && wait_port redis 6379 15; fi

  if port_up 7687; then ok "neo4j already up on :7687"
  else neo4j start >>"$LOG_DIR/neo4j.log" 2>&1 || true; wait_port neo4j 7687 60; fi

  if port_up 9000; then ok "minio already up on :9000"
  else
    MINIO_ROOT_USER="${MINIO_ACCESS_KEY:-nexis}" \
    MINIO_ROOT_PASSWORD="${MINIO_SECRET_KEY:-nexis_dev_password}" \
      start_bg minio "$ROOT" minio server "$MINIO_DATA" \
        --address 127.0.0.1:9000 --console-address 127.0.0.1:9001
    wait_port minio 9000 30
  fi

  if port_up 7233; then ok "temporal already up on :7233"
  else
    start_bg temporal "$ROOT" temporal server start-dev \
      --ip 127.0.0.1 --port 7233 --ui-port 8233 \
      --db-filename "$RUN_DIR/temporal.db" --log-level warn
    wait_port temporal 7233 45
  fi

  if port_up 11434; then ok "ollama already up on :11434 (embeddings)"
  else start_bg ollama "$ROOT" ollama serve; wait_port ollama 11434 30; fi
}

# ------------------------------------------------------------- services ----

build_go() { # name servicedir
  local name=$1 dir=$2
  ( cd "$ROOT/services/$dir" && go build -o "$BIN_DIR/$name" ./cmd/server ) || die "build failed: $dir"
  ok "built $name"
}

start_services() {
  bold "build"
  build_go nexis-control-plane control-plane
  build_go nexis-validator     validator
  build_go nexis-gitops        gitops
  build_go nexis-deploy-engine deploy-engine

  bold "services"

  # Causal-inference sidecar (Pathfinder's root-cause ranker). CAUSAL_GRPC_ENDPOINT
  # in .env.native must point at localhost:8090, not the compose hostname.
  start_bg causal "$ROOT/services/causal-inference" \
    env PYTHONPATH=src python3 -m uvicorn app:app --host 127.0.0.1 --port 8090
  wait_health causal 8090 40

  # Validator's own default is 8081, which another project holds on this
  # machine; VALIDATOR_URL pins it to 8083, so derive the port from that.
  local vport="${VALIDATOR_URL##*:}"; vport="${vport%%/*}"; vport="${vport:-8083}"
  start_bg validator "$ROOT" env PORT="$vport" "$BIN_DIR/nexis-validator"
  wait_health validator "$vport" 30

  local gport="${GITOPS_URL##*:}"; gport="${gport%%/*}"; gport="${gport:-8082}"
  start_bg gitops "$ROOT" env PORT="$gport" "$BIN_DIR/nexis-gitops"
  wait_health gitops "$gport" 30

  local dport="${DEPLOY_ENGINE_URL##*:}"; dport="${dport%%/*}"; dport="${dport:-8091}"
  start_bg deploy-engine "$ROOT" env PORT="$dport" "$BIN_DIR/nexis-deploy-engine"
  wait_health deploy-engine "$dport" 30

  # Control-plane last: it applies migrations, starts the Sentinel detector and
  # runs the Temporal RecoveryPipeline worker in-process, and calls the sidecars.
  start_bg control-plane "$ROOT" env PORT=8080 "$BIN_DIR/nexis-control-plane"
  wait_health control-plane 8080 60
}

start_web() {
  bold "web"
  if port_up 3000; then ok "web already up on :3000"; return 0; fi
  start_bg web "$ROOT/apps/web" pnpm dev
  wait_port web 3000 90
}

# ----------------------------------------------------------------- verbs ----

SERVICES=(web control-plane deploy-engine gitops validator causal)
INFRA=(temporal minio ollama)

up() {
  local with_web=1
  [ "${1:-}" = "--no-web" ] && with_web=0
  load_env
  start_infra
  start_services
  [ "$with_web" = 1 ] && start_web
  echo
  bold "ready"
  echo "  console        http://localhost:3000     (admin@nexis.local / nexis-admin-2026)"
  echo "  control-plane  http://localhost:8080/healthz"
  echo "  temporal UI    http://localhost:8233"
  echo "  minio console  http://localhost:9001     (${MINIO_ACCESS_KEY:-nexis} / ${MINIO_SECRET_KEY:-nexis_dev_password})"
  echo "  logs           $LOG_DIR"
}

down() {
  bold "stopping services"
  for n in "${SERVICES[@]}"; do
    if running "$n"; then
      pkill -P "$(cat "$PID_DIR/$n.pid")" 2>/dev/null || true
      kill "$(cat "$PID_DIR/$n.pid")" 2>/dev/null || true
      rm -f "$PID_DIR/$n.pid"; ok "stopped $n"
    fi
  done
  if [ "${1:-}" = "--infra" ]; then
    bold "stopping infra"
    for n in "${INFRA[@]}"; do
      if running "$n"; then
        kill "$(cat "$PID_DIR/$n.pid")" 2>/dev/null || true
        rm -f "$PID_DIR/$n.pid"; ok "stopped $n"
      fi
    done
    redis-cli -p 6379 shutdown nosave 2>/dev/null && ok "stopped redis" || true
    neo4j stop >/dev/null 2>&1 && ok "stopped neo4j" || true
    "$PG_CTL" -D "$PGDATA_NEXIS" stop -m fast >/dev/null 2>&1 && ok "stopped postgres" || true
  else
    echo "  (infra left running — 'down --infra' to stop it too)"
  fi
}

status() {
  printf '%-16s %-7s %s\n' COMPONENT PORT STATE
  while read -r name port; do
    if port_up "$port"; then printf '%-16s %-7s \033[32mup\033[0m\n' "$name" "$port"
    else printf '%-16s %-7s \033[31mdown\033[0m\n' "$name" "$port"; fi
  done <<EOF
postgres 5432
redis 6379
neo4j 7687
minio 9000
temporal 7233
ollama 11434
causal 8090
validator 8083
gitops 8082
deploy-engine 8091
control-plane 8080
web 3000
EOF
}

case "${1:-up}" in
  up)     shift || true; up "${1:-}" ;;
  down)   shift || true; down "${1:-}" ;;
  status) status ;;
  logs)   tail -f "$LOG_DIR/${2:?usage: dev.sh logs <name>}.log" ;;
  *)      sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//' ;;
esac
