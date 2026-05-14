#!/usr/bin/env bash
# backup_db.sh — point-in-time pg_dump of the local nexis database.
#
# Why:
#   The compose stack uses a named volume (postgres_data) for the
#   pgvector/pg16 instance. A `docker compose down -v` wipes that volume
#   and there is no other recovery path. This script captures a logical
#   dump so a dev or a CI runner can `restore_db.sh` after destructive
#   schema work or a botched migration.
#
# How:
#   We pg_dump from inside the running container (no need for pg_dump on
#   the host, and we sidestep version-mismatch issues between host and
#   image). Output is gzipped and dropped into backups/ with a timestamped
#   filename. Set $BACKUPS_DIR / $POSTGRES_SERVICE / $POSTGRES_DB / etc.
#   to override.
#
# Example cron — daily at 02:00 local:
#   0 2 * * * cd /path/to/web_app && ./scripts/backup_db.sh >>backups/cron.log 2>&1
#
# Retention is opinionated: we keep the most recent 30 files. Bump
# $KEEP_LAST or wire a S3 sync if you need longer history.
set -euo pipefail

POSTGRES_SERVICE="${POSTGRES_SERVICE:-postgres}"
POSTGRES_USER="${POSTGRES_USER:-nexis}"
POSTGRES_DB="${POSTGRES_DB:-nexis}"
BACKUPS_DIR="${BACKUPS_DIR:-backups}"
KEEP_LAST="${KEEP_LAST:-30}"

mkdir -p "$BACKUPS_DIR"

ts="$(date +%Y%m%d_%H%M%S)"
out="$BACKUPS_DIR/nexis_${ts}.sql.gz"

# `pg_dump --clean --if-exists` makes the dump self-restoring without a
# fresh database. `--no-owner --no-privileges` keeps the dump portable
# across role names (dev nexis vs staging nexis_app). Adjust if you
# specifically need ownership preserved.
docker compose exec -T "$POSTGRES_SERVICE" \
  pg_dump \
    --username="$POSTGRES_USER" \
    --dbname="$POSTGRES_DB" \
    --format=plain \
    --clean \
    --if-exists \
    --no-owner \
    --no-privileges \
  | gzip -9 >"$out"

# Trim history to the most recent $KEEP_LAST files. find -printf isn't
# portable to macOS BSD find, so we use stat-less ls -t and `tail -n +N`.
keep=$((KEEP_LAST + 1))
removed=$(ls -1t "$BACKUPS_DIR"/nexis_*.sql.gz 2>/dev/null | tail -n "+${keep}" || true)
if [ -n "$removed" ]; then
  echo "$removed" | xargs -r rm -f
fi

size=$(wc -c <"$out" | tr -d ' ')
printf 'wrote %s (%s bytes)\n' "$out" "$size"
