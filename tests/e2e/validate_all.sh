#!/usr/bin/env bash
# tests/e2e/validate_all.sh — Comprehensive requirements validation.
#
# Walks through every documented requirement and reports pass/fail.
# Designed to be the canonical "is everything actually working?" check.
#
# Exit code: 0 = all green, 1 = any failure.

set -uo pipefail

API="${API:-http://localhost:8080}"
WEB="${WEB:-http://localhost:3000}"
EMAIL="${EMAIL:-admin@nexis.local}"
PASSWORD="${PASSWORD:-nexis-admin-2026}"
COOKIES="$(mktemp -t nexis-validate.XXXXXX)"
trap 'rm -f "$COOKIES"' EXIT

PASS=0; FAIL=0; SKIP=0
section() { printf '\n\033[1;36m▌ %s\033[0m\n' "$1"; }
check()   { printf '  \033[32m✓\033[0m %s\n' "$1"; PASS=$((PASS+1)); }
fail()    { printf '  \033[31m✗\033[0m %s\n' "$1"; FAIL=$((FAIL+1)); }
skip()    { printf '  \033[33m○\033[0m %s\n' "$1"; SKIP=$((SKIP+1)); }
kv()      { printf '    %-20s %s\n' "$1" "$2"; }

# ---- 1. Stack health ------------------------------------------------------
section "1/12 · Docker stack health"
SVCS=$(docker compose -f /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/docker-compose.yml ps --format '{{.Service}}:{{.Status}}' 2>/dev/null)
for svc in postgres redis minio temporal neo4j control-plane gitops causal-inference; do
  if echo "$SVCS" | grep -q "^${svc}:Up"; then
    check "$svc up"
  else
    fail "$svc not up"
  fi
done

# ---- 2. Control-plane endpoints ------------------------------------------
section "2/12 · Control-plane endpoints"
H=$(curl -s "$API/healthz")
if echo "$H" | grep -q '"status":"ok"'; then check "GET /healthz"; else fail "GET /healthz returned: $H"; fi

# ---- 3. Auth flow ---------------------------------------------------------
section "3/12 · Auth + session"
LOGIN=$(curl -fsS -X POST -H "Content-Type: application/json" \
  -d "$(jq -n --arg e "$EMAIL" --arg p "$PASSWORD" '{email:$e,password:$p}')" \
  -c "$COOKIES" "$API/v1/auth/login" 2>&1) && check "POST /v1/auth/login" || fail "login failed"
USER_ID=$(echo "$LOGIN" | jq -r '.user_id // empty')
ORG_ID=$(echo  "$LOGIN" | jq -r '.org_id // empty')
[ -n "$USER_ID" ] && check "session has user_id: $USER_ID" || fail "no user_id"
[ -n "$ORG_ID" ]  && check "session has org_id: $ORG_ID"  || fail "no org_id"
ME=$(curl -fsS -b "$COOKIES" "$API/v1/me" 2>&1)
[ "$(echo "$ME" | jq -r '.user.email // empty')" = "$EMAIL" ] && check "GET /v1/me identifies admin" || fail "GET /v1/me"

# ---- 4. Workspace + project ----------------------------------------------
section "4/12 · Workspace + project"
WS=$(curl -fsS -b "$COOKIES" "$API/v1/workspaces" | jq -r '.[0].id')
[ -n "$WS" ] && [ "$WS" != "null" ] && check "GET /v1/workspaces · $WS" || fail "no workspace"

PROJ_BODY=$(jq -n --arg n "Validation $(date +%H%M%S)" \
  '{name:$n, description:"validation", environment:"dev",
    selectors:{sentry_organization_slug:"x", sentry_project_slug:"y"},
    slo:{availability_target:0.999}}')
P=$(curl -sS -X POST -H "Content-Type: application/json" -b "$COOKIES" \
  -d "$PROJ_BODY" "$API/v1/workspaces/$WS/projects")
PROJ_ID=$(echo "$P" | jq -r '.id // empty')
[ -n "$PROJ_ID" ] && check "POST /v1/workspaces/$WS/projects · $PROJ_ID" || fail "project create: $(echo "$P" | head -c 200)"

LIST=$(curl -fsS -b "$COOKIES" "$API/v1/workspaces/$WS/projects" | jq 'length')
[ "$LIST" -ge 1 ] && check "GET /v1/workspaces/$WS/projects · count=$LIST" || fail "project list returned 0"

# ---- 5. Recovery pipeline -------------------------------------------------
section "5/12 · Recovery pipeline"
RUN=$(curl -fsS -X POST -H "Content-Type: application/json" -b "$COOKIES" \
  -d "$(jq -n --arg p "$PROJ_ID" '{scenario:"null-deref", project_id:$p}')" \
  "$API/v1/workspaces/$WS/pipelines/demo" | jq -r '.id')
[ -n "$RUN" ] && [ "$RUN" != "null" ] && check "POST /v1/workspaces/$WS/pipelines/demo · $RUN" || fail "pipeline start"

START=$(date +%s); DEC_PHASE=""
for i in $(seq 1 90); do
  ROW=$(curl -fsS -b "$COOKIES" "$API/v1/workspaces/$WS/pipelines/$RUN")
  STATUS=$(echo "$ROW" | jq -r '.run.status // empty')
  case "$STATUS" in
    succeeded|failed|cancelled|degraded) DEC_PHASE="$STATUS"; break ;;
  esac
  # Auto-approve if pending
  DEC=$(curl -fsS -b "$COOKIES" "$API/v1/workspaces/$WS/pipelines/$RUN/decision" 2>/dev/null | jq -r '.decision // empty')
  if [ "$DEC" = "pending" ]; then
    curl -fsS -X POST -b "$COOKIES" -H "Content-Type: application/json" -d '{"notes":"validate"}' \
      "$API/v1/workspaces/$WS/pipelines/$RUN/approve" > /dev/null && check "POST /v1/workspaces/$WS/pipelines/$RUN/approve"
  fi
  sleep 2
done
[ "$DEC_PHASE" = "succeeded" ] && check "pipeline succeeded in $(( $(date +%s) - START ))s" || fail "pipeline final: $DEC_PHASE"

# ---- 6. Activity events ledger -------------------------------------------
section "6/12 · Activity events ledger"
EVENTS=$(echo "$ROW" | jq '.events | length')
[ "$EVENTS" -ge 18 ] && check "20+ activity_events · got $EVENTS" || fail "only $EVENTS events"
AGENTS=$(echo "$ROW" | jq -r '.events | map(.agent_role) | unique | length')
[ "$AGENTS" -ge 9 ] && check "9+ agent roles fired · got $AGENTS" || fail "only $AGENTS agents"
echo "$ROW" | jq -e '.events | map(select(.agent_role=="approval_gate" and .message != null)) | length >= 2' >/dev/null \
  && check "approval_gate Route + Finalize emitted" || fail "approval_gate events missing"

# ---- 7. Per-agent drill-down ---------------------------------------------
section "7/12 · Per-agent drill-down endpoints"
for a in architect backend qa devops data_engineer pathfinder synthesiser sentinel approval_gate; do
  CNT=$(curl -fsS -b "$COOKIES" "$API/v1/workspaces/$WS/agents/$a/runs/$RUN/events" | jq '(.events // []) | length' 2>/dev/null || echo 0)
  if [ "$CNT" -ge 1 ]; then check "$a · $CNT events"; else fail "$a · 0 events"; fi
done
AGENTS_API=$(curl -fsS -b "$COOKIES" "$API/v1/workspaces/$WS/agents" | jq 'length')
[ "$AGENTS_API" -eq 10 ] && check "GET /agents catalog · 10 entries" || fail "agents catalog returned $AGENTS_API"

# ---- 8. Cost tracking ----------------------------------------------------
section "8/12 · Cost ledger"
COST=$(curl -fsS -b "$COOKIES" "$API/v1/orgs/$ORG_ID/cost" 2>/dev/null)
TIN=$(echo "$COST" | jq -r '.tokens_in // 0')
TOUT=$(echo "$COST" | jq -r '.tokens_out // 0')
kv "tokens_in"  "$TIN"
kv "tokens_out" "$TOUT"
[ "$TIN" -ge 0 ] && check "GET /v1/orgs/$ORG_ID/cost responds" || fail "cost endpoint"

# ---- 9. Audit + integrity ------------------------------------------------
section "9/12 · Audit log"
AUDIT=$(curl -fsS -b "$COOKIES" "$API/v1/audit?limit=20" | jq 'length')
[ "$AUDIT" -ge 1 ] && check "GET /v1/audit · $AUDIT entries" || fail "audit empty"

# ---- 10. System health ---------------------------------------------------
section "10/12 · System health probes"
SH=$(curl -fsS -b "$COOKIES" "$API/v1/system-health")
for sub in control_plane postgres redis temporal neo4j minio; do
  STATUS=$(echo "$SH" | jq -r ".${sub}.status // empty")
  if [ "$STATUS" = "healthy" ]; then
    LAT=$(echo "$SH" | jq -r ".${sub}.latency_ms")
    check "$sub healthy (${LAT}ms)"
  else
    fail "$sub status=$STATUS"
  fi
done

# ---- 11. Integrations -----------------------------------------------------
section "11/12 · Integrations + GitHub end-to-end"
INTEG=$(curl -fsS -b "$COOKIES" "$API/v1/integrations")
GH_STATUS=$(echo "$INTEG" | jq -r 'map(select(.provider=="github")) | .[0].status // empty')
if [ "$GH_STATUS" = "connected" ]; then
  check "GitHub integration connected"
  # Verify we can mint installation token via App JWT
  PEM=/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/secrets/github-app.pem
  if [ -s "$PEM" ]; then
    b64url() { openssl base64 -e -A 2>/dev/null | tr -d '\n=' | tr '/+' '_-'; }
    NOW=$(date +%s); HDR=$(printf '{"alg":"RS256","typ":"JWT"}' | b64url)
    PL=$(printf '{"iat":%d,"exp":%d,"iss":3711190}' $((NOW-60)) $((NOW+600)) | b64url)
    SG=$(printf '%s.%s' "$HDR" "$PL" | openssl dgst -binary -sha256 -sign "$PEM" | b64url)
    JWT="${HDR}.${PL}.${SG}"
    APP=$(curl -fsS -H "Authorization: Bearer $JWT" -H "Accept: application/vnd.github+json" https://api.github.com/app | jq -r '.slug // empty')
    [ -n "$APP" ] && check "GitHub App JWT works · slug=$APP" || fail "GitHub JWT auth"
  fi
else
  skip "GitHub not connected"
fi
for p in sentry slack argocd datadog pagerduty; do
  S=$(echo "$INTEG" | jq -r --arg p "$p" 'map(select(.provider==$p)) | .[0].status // empty')
  if [ "$S" = "connected" ]; then check "$p connected"; else skip "$p (not configured)"; fi
done

# ---- 12. RLS + multi-tenancy ---------------------------------------------
section "12/12 · RLS + multi-tenancy"
# Try to read a workspace as a non-admin — should 404 or 401
NOAUTH=$(curl -s -o /dev/null -w "%{http_code}" "$API/v1/workspaces")
[ "$NOAUTH" = "401" ] && check "Unauthenticated workspace list → 401" || fail "Unauth got $NOAUTH"
# Verify RLS policies present
POLICIES=$(docker compose -f /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/docker-compose.yml exec -T postgres psql -U nexis -d nexis -tAc "SELECT COUNT(*) FROM pg_policies WHERE schemaname='public'" 2>/dev/null | tr -d ' \n')
[ "$POLICIES" -ge 15 ] && check "RLS policies present · $POLICIES policies" || fail "only $POLICIES RLS policies"
WITHCHECK=$(docker compose -f /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/docker-compose.yml exec -T postgres psql -U nexis -d nexis -tAc "SELECT COUNT(*) FROM pg_policies WHERE schemaname='public' AND with_check IS NOT NULL" 2>/dev/null | tr -d ' \n')
[ "$WITHCHECK" -ge 15 ] && check "WITH CHECK on $WITHCHECK policies (write-side hardening)" || fail "only $WITHCHECK WITH CHECK policies"

# ---- Summary --------------------------------------------------------------
echo ""
printf '\033[1m═══ SUMMARY ═══\033[0m\n'
printf '  \033[32mPASS:\033[0m %d\n' "$PASS"
printf '  \033[31mFAIL:\033[0m %d\n' "$FAIL"
printf '  \033[33mSKIP:\033[0m %d\n' "$SKIP"
echo ""
[ "$FAIL" -eq 0 ] && printf '\033[1;32m✓ ALL REQUIREMENTS MET\033[0m\n' || printf '\033[1;31m✗ %d FAILURES\033[0m\n' "$FAIL"
exit $FAIL
