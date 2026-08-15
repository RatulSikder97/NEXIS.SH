#!/usr/bin/env bash
# Inject a REAL production fault into NEXIS and let the fleet heal it.
#
# This is the demo you run in front of a client: you break something, nobody
# touches the console, and the platform detects → diagnoses → patches →
# validates → asks you to approve.
#
# It is not a shortcut into the pipeline. The event goes through the same
# signed intake any of your services would use:
#
#   you → POST /v1/webhooks/webhook/{org}  (HMAC-signed)
#       → incidents_raw
#       → Sentinel picks up the fatal row on its next tick
#       → RecoveryPipeline starts on its own
#
# Setup (once): connect "Incident intake" on /console/integrations, copy the
# signing secret it shows you, then export:
#
#   export NEXIS_ORG_ID=...        # shown on the same card
#   export NEXIS_WEBHOOK_SECRET=...
#   export NEXIS_URL=https://nexis.sh   # optional, this is the default
#
# Usage:
#   scripts/break-it.sh                    # default: division-by-zero in checkout
#   scripts/break-it.sh null-deref         # pick a preset
#   scripts/break-it.sh --list             # show presets
#   scripts/break-it.sh --title "..." --service api --stack "..." --logs "..."
set -euo pipefail

NEXIS_URL="${NEXIS_URL:-https://nexis.sh}"
ORG="${NEXIS_ORG_ID:-}"
SECRET="${NEXIS_WEBHOOK_SECRET:-}"

die() { printf '\033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }
ok()  { printf '\033[32m✓\033[0m %s\n' "$*"; }

# --- presets ---------------------------------------------------------------
# Each one names a real symbol in the demo service so Pathfinder's graph
# traversal resolves it instead of guessing.

preset_division_by_zero() {
  TITLE="ZeroDivisionError: division by zero in unit_price"
  SERVICE="checkout-api"
  NODE="nexis_fixture.pricing.unit_price"
  STACK='Traceback (most recent call last):
  File "src/nexis_fixture/app.py", line 13, in predict
    body = handler(req.get("args", {}))
  File "src/nexis_fixture/pricing.py", line 14, in unit_price
    return line_total / quantity
ZeroDivisionError: division by zero'
  LOGS="ts=$(date -u +%FT%TZ) level=error service=checkout-api msg=unhandled_exception route=/checkout exc=ZeroDivisionError line_total=49.9 quantity=0
ts=$(date -u +%FT%TZ) level=warn service=checkout-api msg=cancelled_line_priced note=\"quantity 0 reaches unit_price without a guard\""
}

preset_null_deref() {
  TITLE="AttributeError: 'NoneType' object has no attribute 'quantize'"
  SERVICE="checkout-api"
  NODE="nexis_fixture.api.safe_div"
  STACK='Traceback (most recent call last):
  File "src/nexis_fixture/app.py", line 13, in predict
    body = handler(req.get("args", {}))
  File "src/nexis_fixture/api.py", line 22, in handler
    return safe_div(hits, total).quantize(Decimal("0.01"))
AttributeError: '"'"'NoneType'"'"' object has no attribute '"'"'quantize'"'"''
  LOGS="ts=$(date -u +%FT%TZ) level=error service=checkout-api msg=unhandled_exception route=/predict exc=AttributeError"
}

preset_pool_exhausted() {
  TITLE="PoolTimeout: connection pool exhausted after 30s"
  SERVICE="orders-api"
  NODE="nexis_fixture.db.pool_settings"
  STACK='Traceback (most recent call last):
  File "src/nexis_fixture/db.py", line 34, in run_query
    cur = conn.cursor()
psycopg_pool.PoolTimeout: couldn'"'"'t get a connection after 30.0 sec'
  LOGS="ts=$(date -u +%FT%TZ) level=error service=orders-api msg=pool_timeout waited_s=30 pool_max=5 workers=32"
}

preset_kafka_lag() {
  TITLE="Kafka consumer lag 1.2M on orders-projector"
  SERVICE="orders-projector"
  NODE="nexis_fixture.jobs.consumer_config"
  STACK='kafka.consumer: group orders-projector lag=1204882 partitions=6
  File "src/nexis_fixture/jobs.py", line 72, in consumer_config
    "max_poll_records": 10000,
RebalanceLoop: poll interval exceeded while handling a 10k batch'
  LOGS="ts=$(date -u +%FT%TZ) level=error service=orders-projector msg=consumer_lag lag=1204882"
}

list_presets() {
  cat <<'EOF'
Presets:
  division-by-zero   ZeroDivisionError in pricing.unit_price      (default)
  null-deref         AttributeError on a None from api.safe_div
  pool-exhausted     PoolTimeout from db.pool_settings
  kafka-lag          consumer lag from jobs.consumer_config
EOF
}

# --- args ------------------------------------------------------------------
PRESET="division-by-zero"
TITLE=""; SERVICE=""; STACK=""; LOGS=""; NODE=""; ENVIRONMENT="production"
while [ $# -gt 0 ]; do
  case "$1" in
    --list) list_presets; exit 0 ;;
    --title) TITLE="$2"; shift 2 ;;
    --service) SERVICE="$2"; shift 2 ;;
    --stack) STACK="$2"; shift 2 ;;
    --logs) LOGS="$2"; shift 2 ;;
    --node) NODE="$2"; shift 2 ;;
    --env) ENVIRONMENT="$2"; shift 2 ;;
    -h|--help) sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) PRESET="$1"; shift ;;
  esac
done

if [ -z "$TITLE" ]; then
  case "$PRESET" in
    division-by-zero|zero-div) preset_division_by_zero ;;
    null-deref)                preset_null_deref ;;
    pool-exhausted)            preset_pool_exhausted ;;
    kafka-lag)                 preset_kafka_lag ;;
    *) die "unknown preset '$PRESET' — try --list" ;;
  esac
fi

[ -n "$ORG" ]    || die "NEXIS_ORG_ID is not set (see the Incident intake card on /console/integrations)"
[ -n "$SECRET" ] || die "NEXIS_WEBHOOK_SECRET is not set (shown once when you connect the intake)"
command -v openssl >/dev/null || die "openssl is required to sign the request"
command -v python3 >/dev/null || die "python3 is required to build the JSON body"

# --- build + sign ----------------------------------------------------------
BODY=$(TITLE="$TITLE" SERVICE="$SERVICE" ENVIRONMENT="$ENVIRONMENT" \
       STACK="$STACK" LOGS="$LOGS" NODE="$NODE" python3 -c '
import json, os, time
print(json.dumps({
    "title":       os.environ["TITLE"],
    "service":     os.environ["SERVICE"] or "unknown",
    "environment": os.environ["ENVIRONMENT"],
    "level":       "fatal",
    "event_id":    f"break-it-{int(time.time()*1000)}",
    "stacktrace":  os.environ["STACK"],
    "logs":        os.environ["LOGS"],
    "metadata":    {"root_cause_node": os.environ["NODE"], "injected_by": "break-it.sh"},
}, separators=(",", ":")))')

SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | awk '{print $NF}')

printf '\n\033[1mBreaking:\033[0m %s\n' "$TITLE"
printf '  service   %s\n  target    %s\n  intake    %s/v1/webhooks/webhook/%s\n\n' \
  "${SERVICE:-unknown}" "${NODE:-—}" "$NEXIS_URL" "$ORG"

CODE=$(printf '%s' "$BODY" | curl -sS -o /tmp/nexis-break-it.out -w '%{http_code}' \
  -X POST "$NEXIS_URL/v1/webhooks/webhook/$ORG" \
  -H "Content-Type: application/json" \
  -H "X-Nexis-Signature: sha256=$SIG" \
  --data-binary @-)

case "$CODE" in
  200) ok "fault accepted — Sentinel will pick it up on its next tick" ;;
  401) die "signature rejected (401). NEXIS_WEBHOOK_SECRET does not match the connected intake." ;;
  400) die "rejected (400): $(cat /tmp/nexis-break-it.out)" ;;
  404) die "no intake for this org (404). Connect 'Incident intake' on /console/integrations first." ;;
  *)   die "unexpected HTTP $CODE: $(cat /tmp/nexis-break-it.out)" ;;
esac

printf '\nWatch it heal:\n  %s/console/incidents\n  %s/console/approvals   (approve when it asks)\n\n' \
  "$NEXIS_URL" "$NEXIS_URL"
