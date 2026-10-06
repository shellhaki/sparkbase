#!/usr/bin/env bash
# Fills empty secrets in .env. Existing values are never changed.
set -euo pipefail
cd "$(dirname "$0")"
[ -f .env ] || cp .env.example .env
set_secret() {
  local name="$1" value="$2"
  if grep -qE "^${name}=.+" .env; then return; fi
  if grep -qE "^${name}=" .env; then
    sed -i.bak "s|^${name}=.*|${name}=${value}|" .env && rm -f .env.bak
  else
    echo "${name}=${value}" >> .env
  fi
  echo "generated ${name}"
}
set_secret ENCRYPTION_KEY "$(openssl rand -base64 32)"
set_secret TRAEFIK_CONFIG_TOKEN "$(openssl rand -hex 24)"
set_secret CONTROL_DB_PASSWORD "$(openssl rand -hex 24)"
set_secret CONTROL_REDIS_PASSWORD "$(openssl rand -hex 24)"
set_secret PG_HOST_PASSWORD "$(openssl rand -hex 24)"
set_secret PGBOUNCER_PASSWORD "$(openssl rand -hex 24)"
set_secret CACHE_HOST_PASSWORD "$(openssl rand -hex 24)"
set_secret GARAGE_RPC_SECRET "$(openssl rand -hex 32)"
set_secret GARAGE_ADMIN_TOKEN "$(openssl rand -hex 32)"
echo "secrets ready in $(pwd)/.env"
