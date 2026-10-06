#!/usr/bin/env bash
# Builds the images, starts the full Docker install with selfsigned TLS on
# shifted ports, runs the end-to-end test through Traefik, then removes
# everything (containers and volumes).
#
#   scripts/e2e-compose.sh            # KEEP=1 leaves the stack running
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/deploy"
ENV="$ROOT/deploy/.env.e2e"
cat >"$ENV" <<EOF
SPARKBASE_DOMAIN=sparkbase.localhost
TLS_MODE=selfsigned
COMPOSE_PROFILES=traefik
HTTP_PORT=8080
HTTPS_PORT=8443
PG_PORT=15432
PG_POOLED_PORT=16432
REDIS_TLS_PORT=16380
PUBLIC_API_URL=https://api.sparkbase.localhost:8443
DASHBOARD_URL=https://app.sparkbase.localhost:8443
S3_PUBLIC_URL=https://s3.sparkbase.localhost:8443
SPARKBASE_VERSION=e2e
ENCRYPTION_KEY=$(openssl rand -base64 32)
TRAEFIK_CONFIG_TOKEN=$(openssl rand -hex 24)
CONTROL_DB_PASSWORD=$(openssl rand -hex 24)
CONTROL_REDIS_PASSWORD=$(openssl rand -hex 24)
PG_HOST_PASSWORD=$(openssl rand -hex 24)
PGBOUNCER_PASSWORD=$(openssl rand -hex 24)
CACHE_HOST_PASSWORD=$(openssl rand -hex 24)
GARAGE_RPC_SECRET=$(openssl rand -hex 32)
GARAGE_ADMIN_TOKEN=$(openssl rand -hex 32)
EOF
DC=(docker compose -p sparkbase-e2e --env-file "$ENV")
cleanup() {
  if [ -z "${KEEP:-}" ]; then
    "${DC[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
    rm -f "$ENV"
  fi
}
trap cleanup EXIT
"${DC[@]}" build
"${DC[@]}" up -d
set +e
(cd "$ROOT/server" && E2E_HTTPS_PORT=8443 go test -count=1 -tags e2e -v ./internal/e2e/)
STATUS=$?
set -e
if [ $STATUS -ne 0 ]; then "${DC[@]}" logs --tail 60 api traefik garage pgbouncer; fi
exit $STATUS
