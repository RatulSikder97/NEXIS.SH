#!/usr/bin/env bash
# restore_db.sh — replay a backup_db.sh dump into the local Postgres.
#
# Why:
#   Backups are useless without a documented, tested restore path. This is
#   the companion to backup_db.sh — pipes a gzipped pg_dump into psql via
#   `docker compose exec` so it works on any host with docker installed
#   and the compose stack running.
#
# Safety:
#   The dump was created with --clean --if-exists, which means the restore
#   DROPs every object it knows about before re-CREATEing it. Connected
#   sessions other than psql itself will block the DROP — make sure no
#   control-plane / web service is hammering the DB. The script aborts on
#   the first error (`-v ON_ERROR_STOP=1`) so a partial restore never
#   silently passes.
#
# Usage:
#   ./scripts/restore_db.sh backups/nexis_20260514_021500.sql.gz
#   ./scripts/restore_db.sh latest    # convenience alias
set -euo pipefail

POSTGRES_SERVICE="${POSTGRES_SERVICE:-postgres}"
POSTGRES_USER="${POSTGRES_USER:-nexis}"
POSTGRES_DB="${POSTGRES_DB:-nexis}"
BACKUPS_DIR="${BACKUPS_DIR:-backups}"

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <backup.sql.gz | latest>" >&2
  exit 2
fi

target="$1"
if [ "$target" = "latest" ]; then
  target=$(ls -1t "$BACKUPS_DIR"/nexis_*.sql.gz 2>/dev/null | head -n1 || true)
  if [ -z "$target" ]; then
    echo "no backups found in $BACKUPS_DIR" >&2
    exit 1
  fi
  echo "restoring latest: $target"
fi

if [ ! -f "$target" ]; then
  echo "no such file: $target" >&2
  exit 1
fi

# Sanity-check the compose service is running before we try to exec into
# it. `compose ps -q` returns the container ID or empty.
if [ -z "$(docker compose ps -q "$POSTGRES_SERVICE" 2>/dev/null)" ]; then
  echo "$POSTGRES_SERVICE service is not running — run \`docker compose up -d $POSTGRES_SERVICE\` first" >&2
  exit 1
fi

gunzip -c "$target" | docker compose exec -T "$POSTGRES_SERVICE" \
  psql \
    --username="$POSTGRES_USER" \
    --dbname="$POSTGRES_DB" \
    --set ON_ERROR_STOP=1 \
    --quiet

echo "restored $target"
