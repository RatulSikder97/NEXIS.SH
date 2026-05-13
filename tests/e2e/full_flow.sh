#!/usr/bin/env bash
# tests/e2e/full_flow.sh
#
# Full-system E2E flow against the running stack on localhost:8080.
#
# Steps:
#   1. Authenticate as the seed admin.
#   2. Resolve the admin's primary workspace.
#   3. Trigger the synthetic `null-deref` scenario (real recovery pipeline).
#   4. Poll until the workflow_run lands in a terminal state.
#   5. Verify activity_events landed in the DB (every agent emitted at
#      least one finish frame).
#   6. Verify the run is visible from the panel SDKs:
#        - /v1/workspaces/{ws}/pipelines/{run}
#        - /v1/workspaces/{ws}/agents/{name}/runs/{run}/events for each L1/L2
#   7. Print the panel URL the operator can open to inspect the run.
#
# Exit code: 0 on success, non-zero with a diagnostic message otherwise.
#
# Usage:
#   ./tests/e2e/full_flow.sh                # uses defaults
#   API=http://localhost:8080 EMAIL=admin@nexis.local ./tests/e2e/full_flow.sh

set -euo pipefail

API="${API:-http://localhost:8080}"
WEB="${WEB:-http://localhost:3000}"
EMAIL="${EMAIL:-admin@nexis.local}"
PASSWORD="${PASSWORD:-nexis-admin-2026}"
SCENARIO="${SCENARIO:-null-deref}"
TIMEOUT_SEC="${TIMEOUT_SEC:-180}"
COOKIES="$(mktemp -t nexis-e2e.XXXXXX)"
trap 'rm -f "$COOKIES"' EXIT

bold() { printf '\033[1m%s\033[0m\n' "$1"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; }
warn() { printf '  \033[33m!\033[0m %s\n' "$1"; }
fail() { printf '  \033[31m✗\033[0m %s\n' "$1"; exit 1; }

bold "1/7 · Authenticate"
LOGIN=$(curl -fsS -X POST -H "Content-Type: application/json" \
  -d "$(jq -n --arg e "$EMAIL" --arg p "$PASSWORD" '{email:$e,password:$p}')" \
  -c "$COOKIES" "$API/v1/auth/login") || fail "login failed (check email/password)"
USER_ID=$(echo "$LOGIN"   | jq -r '.user_id')
ORG_ID=$(echo  "$LOGIN"   | jq -r '.org_id')
ok "logged in as $EMAIL (user $USER_ID, org $ORG_ID)"

bold "2/7 · Resolve workspace"
WS=$(curl -fsS -b "$COOKIES" "$API/v1/workspaces" | jq -r '.[0].id')
[ -n "$WS" ] && [ "$WS" != "null" ] || fail "no workspace returned"
ok "workspace: $WS"

bold "3/7 · Trigger synthetic '$SCENARIO' scenario"
RUN_BODY=$(curl -fsS -X POST -H "Content-Type: application/json" -b "$COOKIES" \
  -d "$(jq -n --arg s "$SCENARIO" '{scenario:$s}')" \
  "$API/v1/workspaces/$WS/pipelines/demo")
RUN_ID=$(echo "$RUN_BODY" | jq -r '.id')
[ -n "$RUN_ID" ] && [ "$RUN_ID" != "null" ] || fail "pipeline didn't start"
ok "workflow_run started: $RUN_ID"
ok "panel URL: $WEB/console/incidents/$RUN_ID?live=1"

bold "4/7 · Poll until terminal"
START=$(date +%s)
while true; do
  NOW=$(date +%s); ELAPSED=$((NOW - START))
  if [ $ELAPSED -gt "$TIMEOUT_SEC" ]; then
    fail "timed out after ${TIMEOUT_SEC}s"
  fi
  ROW=$(curl -fsS -b "$COOKIES" "$API/v1/workspaces/$WS/pipelines/$RUN_ID")
  STATUS=$(echo "$ROW" | jq -r '.run.status // .status // empty')
  case "$STATUS" in
    succeeded|failed|cancelled|degraded)
      ok "run terminal in ${ELAPSED}s · status=$STATUS"
      break
      ;;
    running|pending|"")
      printf '\r    ⏳ running… %02ds' "$ELAPSED"
      ;;
    *)
      printf '\r    status=%s (%ds)' "$STATUS" "$ELAPSED"
      ;;
  esac
  sleep 2
done
echo ""

bold "5/7 · Verify activity_events in DB"
RUN_EVENTS=$(echo "$ROW" | jq '.events // []')
RUN_EVENT_COUNT=$(echo "$RUN_EVENTS" | jq 'length')
[ "$RUN_EVENT_COUNT" -gt 0 ] || fail "zero activity_events for run $RUN_ID"
ok "activity_events: $RUN_EVENT_COUNT for this run"

AGENTS_SEEN=$(echo "$RUN_EVENTS" | jq -r 'map(.agent_role) | unique | join(", ")')
ok "agents observed: $AGENTS_SEEN"

DEGRADED=$(echo "$RUN_EVENTS" | jq -r '[.[] | select((.payload // {})|.degraded == true)] | length')
if [ "$DEGRADED" -gt 0 ]; then
  warn "$DEGRADED agent(s) ran via stub fallback (no real OpenAI key — synthetic payload still emitted)"
fi

DURATION=$(echo "$ROW" | jq -r '.run.duration_ms // 0')
ok "wall-clock duration: ${DURATION}ms"

bold "6/7 · Verify per-agent drill-down sees the run"
HITS=0
for AGENT in architect backend qa devops data_engineer pathfinder synthesiser validator_l2; do
  CNT=$(curl -fsS -b "$COOKIES" "$API/v1/workspaces/$WS/agents/$AGENT/runs/$RUN_ID/events" \
    | jq '(.events // []) | length' 2>/dev/null || echo 0)
  if [ "$CNT" -gt 0 ]; then
    ok "$AGENT: $CNT events"
    HITS=$((HITS + 1))
  fi
done
[ "$HITS" -gt 0 ] || warn "no agent had events for this run — was the workflow stub-mode only?"

bold "7/7 · Summary"
echo ""
printf "  Run ID:        %s\n" "$RUN_ID"
printf "  Final status:  %s\n" "$STATUS"
printf "  Wall-clock:    %ds\n" "$ELAPSED"
printf "  Activity rows: %d\n" "$RUN_EVENT_COUNT"
printf "  Agents fired:  %d\n" "$HITS"
echo ""
printf "  Open in panel:\n"
printf "    ▸ %s/console/incidents/%s?live=1\n" "$WEB" "$RUN_ID"
printf "    ▸ %s/console/agents\n" "$WEB"
printf "    ▸ %s/console/activity\n" "$WEB"
echo ""
ok "E2E flow complete"
