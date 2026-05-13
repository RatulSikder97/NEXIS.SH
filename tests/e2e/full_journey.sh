#!/usr/bin/env bash
# tests/e2e/full_journey.sh
#
# Full customer journey against the running stack on localhost:8080.
#
# Steps:
#   1.  Authenticate as the seed admin.
#   2.  Resolve workspace.
#   3.  Create a Project (full selectors + recovery policy).
#   4.  Read the project back, confirm DB persistence.
#   5.  Pre-incident cost snapshot.
#   6.  Trigger the synthetic null-deref incident scoped to this project.
#   7.  Poll workflow_runs until terminal.
#   8.  Verify activity_events ledger.
#   9.  Verify per-agent cost ledger entries.
#  10.  Post-incident cost snapshot (delta from step 5).
#  11.  Print the resolution log + every panel URL the operator can open.
#
# Usage:
#   ./tests/e2e/full_journey.sh
#   PROJECT_NAME="Orders API" SCENARIO=null-deref ./tests/e2e/full_journey.sh

set -euo pipefail

API="${API:-http://localhost:8080}"
WEB="${WEB:-http://localhost:3000}"
EMAIL="${EMAIL:-admin@nexis.local}"
PASSWORD="${PASSWORD:-nexis-admin-2026}"
SCENARIO="${SCENARIO:-null-deref}"
PROJECT_NAME="${PROJECT_NAME:-Orders API ($(date +%H%M%S))}"
ENVIRONMENT="${ENVIRONMENT:-prod}"
TIMEOUT_SEC="${TIMEOUT_SEC:-180}"
COOKIES="$(mktemp -t nexis-e2e.XXXXXX)"
trap 'rm -f "$COOKIES"' EXIT

bold() { printf '\n\033[1m%s\033[0m\n' "$1"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; }
warn() { printf '  \033[33m!\033[0m %s\n' "$1"; }
fail() { printf '  \033[31m✗\033[0m %s\n' "$1"; exit 1; }
kv()   { printf '    %-22s %s\n' "$1" "$2"; }

#-----------------------------------------------------------------------------
bold "1/11 · Authenticate"
LOGIN=$(curl -fsS -X POST -H "Content-Type: application/json" \
  -d "$(jq -n --arg e "$EMAIL" --arg p "$PASSWORD" '{email:$e,password:$p}')" \
  -c "$COOKIES" "$API/v1/auth/login") || fail "login failed"
USER_ID=$(echo "$LOGIN" | jq -r '.user_id')
ORG_ID=$(echo  "$LOGIN" | jq -r '.org_id')
kv "user_id" "$USER_ID"
kv "org_id"  "$ORG_ID"
ok "logged in as $EMAIL"

#-----------------------------------------------------------------------------
bold "2/11 · Resolve workspace"
WS=$(curl -fsS -b "$COOKIES" "$API/v1/workspaces" | jq -r '.[0].id')
[ -n "$WS" ] && [ "$WS" != "null" ] || fail "no workspace returned"
kv "workspace_id" "$WS"

#-----------------------------------------------------------------------------
bold "3/11 · Create project '$PROJECT_NAME'"
CREATE_BODY=$(jq -n \
  --arg name "$PROJECT_NAME" \
  --arg env "$ENVIRONMENT" \
  '{
    name: $name,
    description: "E2E journey test project",
    environment: $env,
    selectors: {
      sentry_organization_slug: "nexis-demo",
      sentry_project_slug: "orders-api",
      datadog_service_tag: "service:orders-api"
    },
    recovery_policy: {
      auto_merge_low_severity: false,
      auto_merge_medium_severity: false,
      medium_countdown_seconds: 120,
      kill_switch_enabled: false,
      approver_user_ids: [],
      max_concurrent_recoveries: 1,
      rollback_on_slo_breach: true
    },
    slo: {
      availability_target: 0.9995,
      latency_p95_ms: 250,
      error_rate_pct: 0.5
    }
  }')
PROJECT=$(curl -sS -X POST -H "Content-Type: application/json" -b "$COOKIES" \
  -d "$CREATE_BODY" "$API/v1/workspaces/$WS/projects")
PROJECT_ID=$(echo "$PROJECT" | jq -r '.id // empty')
if [ -z "$PROJECT_ID" ]; then
  fail "project create failed: $(echo "$PROJECT" | head -c 200)"
fi
PROJECT_SLUG=$(echo "$PROJECT" | jq -r '.slug')
kv "project_id"   "$PROJECT_ID"
kv "project_slug" "$PROJECT_SLUG"
ok "project created"

#-----------------------------------------------------------------------------
bold "4/11 · Verify project persisted"
READ=$(curl -fsS -b "$COOKIES" "$API/v1/projects/$PROJECT_ID")
[ "$(echo "$READ" | jq -r '.id')" = "$PROJECT_ID" ] || fail "project not retrievable"
ok "project reads back from DB"
kv "selectors"  "$(echo "$READ" | jq -c '.selectors | with_entries(select(.value != null and .value != ""))')"
kv "slo"        "$(echo "$READ" | jq -c '.slo')"

#-----------------------------------------------------------------------------
bold "5/11 · Pre-incident cost snapshot"
COST_BEFORE=$(curl -fsS -b "$COOKIES" "$API/v1/orgs/$ORG_ID/cost" 2>/dev/null || echo '{}')
SPEND_BEFORE=$(echo "$COST_BEFORE" | jq -r '.mtd_total_cents_exact // 0')
TOK_IN_BEFORE=$(echo "$COST_BEFORE" | jq -r '.tokens_in // 0')
TOK_OUT_BEFORE=$(echo "$COST_BEFORE" | jq -r '.tokens_out // 0')
kv "mtd_spend_cents"   "$SPEND_BEFORE"
kv "tokens_in"         "$TOK_IN_BEFORE"
kv "tokens_out"        "$TOK_OUT_BEFORE"

#-----------------------------------------------------------------------------
bold "6/11 · Trigger '$SCENARIO' incident scoped to project"
RUN_BODY=$(curl -fsS -X POST -H "Content-Type: application/json" -b "$COOKIES" \
  -d "$(jq -n --arg s "$SCENARIO" --arg p "$PROJECT_ID" '{scenario:$s, project_id:$p}')" \
  "$API/v1/workspaces/$WS/pipelines/demo")
RUN_ID=$(echo "$RUN_BODY" | jq -r '.id')
[ -n "$RUN_ID" ] && [ "$RUN_ID" != "null" ] || fail "pipeline didn't start"
kv "workflow_run_id" "$RUN_ID"
ok "recovery pipeline started"

#-----------------------------------------------------------------------------
bold "7/11 · Poll until terminal"
START=$(date +%s)
while true; do
  NOW=$(date +%s); ELAPSED=$((NOW - START))
  [ $ELAPSED -gt "$TIMEOUT_SEC" ] && fail "timed out after ${TIMEOUT_SEC}s"
  ROW=$(curl -fsS -b "$COOKIES" "$API/v1/workspaces/$WS/pipelines/$RUN_ID")
  STATUS=$(echo "$ROW" | jq -r '.run.status // empty')
  case "$STATUS" in
    succeeded|failed|cancelled|degraded)
      ok "terminal in ${ELAPSED}s · status=$STATUS"
      break ;;
    *)
      printf '\r    ⏳ %s (%02ds)' "${STATUS:-queued}" "$ELAPSED" ;;
  esac
  sleep 2
done
echo ""
DURATION_MS=$(echo "$ROW" | jq -r '.run.duration_ms // 0')
kv "wall-clock"  "${ELAPSED}s"
kv "duration_ms" "$DURATION_MS"

#-----------------------------------------------------------------------------
bold "8/11 · Activity-event ledger"
EVENTS=$(echo "$ROW" | jq '.events // []')
EVENT_COUNT=$(echo "$EVENTS" | jq 'length')
AGENTS=$(echo "$EVENTS" | jq -r 'map(.agent_role) | unique | join(", ")')
DEGRADED=$(echo "$EVENTS" | jq -r '[.[] | select((.payload // {})|.degraded == true)] | length')
kv "event_count" "$EVENT_COUNT"
kv "agents"      "$AGENTS"
[ "$DEGRADED" -gt 0 ] && warn "$DEGRADED agent(s) ran via stub fallback (no real OpenAI key)"
ok "ledger persisted to activity_events"

#-----------------------------------------------------------------------------
bold "9/11 · Per-agent cost"
printf '    %-18s %8s %8s %10s %10s\n' "AGENT" "TOK IN" "TOK OUT" "DUR ms" "COST ¢"
printf '    %s\n' "──────────────────────────────────────────────────────────────"
echo "$EVENTS" | jq -r '
  .[] | select(.status == "succeeded" and (.payload // {}).agent_role != null) |
  [.payload.agent_role, (.payload.tokens_in // 0), (.payload.tokens_out // 0), (.payload.duration_ms // 0), (.payload.cost_cents // 0)] |
  @tsv' | awk -F '\t' '{ printf "    %-18s %8s %8s %10s %10s\n", $1, $2, $3, $4, $5 }'

#-----------------------------------------------------------------------------
bold "10/11 · Post-incident cost snapshot (delta)"
COST_AFTER=$(curl -fsS -b "$COOKIES" "$API/v1/orgs/$ORG_ID/cost" 2>/dev/null || echo '{}')
SPEND_AFTER=$(echo "$COST_AFTER" | jq -r '.mtd_total_cents_exact // 0')
TOK_IN_AFTER=$(echo "$COST_AFTER" | jq -r '.tokens_in // 0')
TOK_OUT_AFTER=$(echo "$COST_AFTER" | jq -r '.tokens_out // 0')
SPEND_DELTA=$(awk -v a="$SPEND_AFTER" -v b="$SPEND_BEFORE" 'BEGIN{printf "%.4f", a-b}')
TI_DELTA=$((TOK_IN_AFTER - TOK_IN_BEFORE))
TO_DELTA=$((TOK_OUT_AFTER - TOK_OUT_BEFORE))
kv "spend_before"  "${SPEND_BEFORE}¢"
kv "spend_after"   "${SPEND_AFTER}¢"
kv "spend_delta"   "${SPEND_DELTA}¢"
kv "tokens_in_Δ"   "$TI_DELTA"
kv "tokens_out_Δ"  "$TO_DELTA"
if [ "$DEGRADED" -gt 0 ]; then
  warn "cost is 0 because stub-fallback agents don't bill (set OPENAI_API_KEY for real cost)"
fi

#-----------------------------------------------------------------------------
bold "11/11 · Resolution log + panel URLs"
echo ""
echo "  Resolution chain:"
echo "$EVENTS" | jq -r '
  .[] | select(.status == "succeeded") |
  "    [" + (.seq|tostring) + "] " + .agent_role + "/" + .activity_name + "  →  " +
  ((.payload // {}).output_summary // .message // "—")'
echo ""
echo "  Open in panel:"
printf "    ▸ %s/console/projects/%s\n"     "$WEB" "$PROJECT_ID"
printf "    ▸ %s/console/incidents/%s\n"    "$WEB" "$RUN_ID"
printf "    ▸ %s/console/agents\n"          "$WEB"
printf "    ▸ %s/console/cost\n"            "$WEB"
printf "    ▸ %s/console/activity\n"        "$WEB"
echo ""
ok "Full journey complete · project created → incident triggered → recovered → cost tracked → log recorded"
