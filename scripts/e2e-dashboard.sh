#!/usr/bin/env bash
# Smoke test of the dashboard against a live server: pages render and the
# /api proxy carries the session cookie. Run: scripts/with-server.sh scripts/e2e-dashboard.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${DASH_PORT:-13000}"
cd "$ROOT/apps/dashboard"
[ -d .next ] || bun run build >/dev/null
SPARKBASE_API_URL="$SB_API" PORT="$PORT" bun run start >/tmp/sb-dash.log 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT
for _ in $(seq 1 100); do curl -sf "http://127.0.0.1:$PORT/login" >/dev/null && break; sleep 0.2; done
D="http://127.0.0.1:$PORT"
JAR="$(mktemp)"
fail() { echo "FAIL: $*"; exit 1; }
for page in /login /setup /reset-password /invite / /account /admin; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$D$page")
  [ "$code" = 200 ] || fail "page $page returned $code"
done
curl -sf "$D/api/v1/setup" | grep -q '"open":true' || fail "setup not open through proxy"
curl -sf -c "$JAR" -H 'Content-Type: application/json' -d "{\"email\":\"e2e@example.com\",\"password\":\"e2e password 1\"}" "$D/api/v1/setup" >/dev/null || fail "setup"
curl -sf -b "$JAR" "$D/api/v1/me" | grep -q e2e@example.com || fail "session cookie not carried"
PID_JSON=$(curl -sf -b "$JAR" -H 'Content-Type: application/json' -d '{"name":"e2e"}' "$D/api/v1/projects")
PROJECT=$(echo "$PID_JSON" | sed -E 's/.*"id":"([^"]+)".*/\1/')
REF=$(echo "$PID_JSON" | sed -E 's/.*"ref":"([^"]+)".*/\1/')
RES=$(curl -sf -b "$JAR" -H 'Content-Type: application/json' -d '{"kind":"postgres","name":"main"}' "$D/api/v1/projects/$PROJECT/resources")
echo "$RES" | grep -q '"status":"provisioning"' || fail "create service: $RES"
ok=""
for _ in $(seq 1 150); do
  if curl -sf -b "$JAR" -H 'Content-Type: application/json' -d '{"sql":"select 41 + 1 as answer"}' "$D/api/p/$REF/postgres/main/query" | grep -q '"answer":42'; then ok=1; break; fi
  sleep 0.2
done
[ -n "$ok" ] || fail "SQL through proxy"
code=$(curl -s -o /dev/null -w '%{http_code}' "$D/projects/$PROJECT/services/x")
[ "$code" = 200 ] || fail "service page $code"
curl -sf -b "$JAR" -X DELETE "$D/api/v1/projects/$PROJECT" >/dev/null || fail "delete project"
for _ in $(seq 1 100); do
  curl -sf -b "$JAR" "$D/api/v1/projects" | grep -q "$PROJECT" || break; sleep 0.2
done
echo "dashboard e2e: ok"
