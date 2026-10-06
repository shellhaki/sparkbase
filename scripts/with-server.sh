#!/usr/bin/env bash
# Starts a throwaway Sparkbase server on a fresh control database, runs a
# command with SB_API and SB_SETUP_TOKEN set, then stops and cleans up.
#
#   scripts/with-server.sh bun test        (from sdk/js)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
"$ROOT/scripts/dev-services.sh" start >/dev/null
PSQL=psql; [ -x /opt/homebrew/opt/postgresql@18/bin/psql ] && PSQL=/opt/homebrew/opt/postgresql@18/bin/psql
DB="sbrun_$(openssl rand -hex 5)"
PORT="${SB_PORT:-18787}"
WORK="$(mktemp -d)"
PGPASSWORD=sparkbase $PSQL -h 127.0.0.1 -p 54329 -U sparkbase -d postgres -qc "create database $DB"

(cd "$ROOT/server" && go build -o "$WORK/sparkbase-server" ./cmd/sparkbase-server)
env -i PATH="$PATH" HOME="$HOME" \
  SPARKBASE_DOMAIN=localhost PUBLIC_API_URL="http://127.0.0.1:$PORT" DASHBOARD_URL="${SB_DASHBOARD_URL:-http://localhost:3000}" PORT="$PORT" \
  DATABASE_URL="postgres://sparkbase:sparkbase@127.0.0.1:54329/$DB?sslmode=disable" \
  REDIS_URL="redis://:devredis@127.0.0.1:63799/0" REDIS_PREFIX="sbrun:$DB:" \
  ENCRYPTION_KEY="$(openssl rand -base64 32)" TRAEFIK_CONFIG_TOKEN=test-token \
  NATIVE_MODE=direct TLS_MODE=none LOG_LEVEL="${LOG_LEVEL:-warn}" \
  BOOTSTRAP_PG_HOST="postgres://sb_admin:devadmin@127.0.0.1:54330/postgres" \
  BOOTSTRAP_CACHE_HOST="redis://:devredis@127.0.0.1:63799" \
  BOOTSTRAP_STORAGE="local://$WORK/storage" BACKUP_DIR="$WORK/backups" \
  "$WORK/sparkbase-server" >"$WORK/server.log" 2>&1 &
PID=$!
cleanup() {
  kill "$PID" 2>/dev/null || true
  wait "$PID" 2>/dev/null || true
  PGPASSWORD=sparkbase $PSQL -h 127.0.0.1 -p 54329 -U sparkbase -d postgres -qc "drop database if exists $DB with (force)" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT
for _ in $(seq 1 100); do
  grep -q "setup token:" "$WORK/server.log" 2>/dev/null && break
  if ! kill -0 "$PID" 2>/dev/null; then cat "$WORK/server.log"; exit 1; fi
  sleep 0.1
done
TOKEN="$(grep "setup token:" "$WORK/server.log" | awk '{print $3}')"
export SB_API="http://127.0.0.1:$PORT" SB_SETUP_TOKEN="$TOKEN"
set +e
"$@"
STATUS=$?
set -e
if [ "$STATUS" -ne 0 ] && [ -n "${SHOW_LOG:-}" ]; then cat "$WORK/server.log"; fi
exit $STATUS
