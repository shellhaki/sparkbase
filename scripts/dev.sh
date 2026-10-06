#!/usr/bin/env bash
# Runs the server against the dev services. Creates server/.env on first run.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV="$ROOT/server/.env"
"$ROOT/scripts/dev-services.sh" start >/dev/null
if [ ! -f "$ENV" ]; then
  sed -e "s|^ENCRYPTION_KEY=.*|ENCRYPTION_KEY=$(openssl rand -base64 32)|" \
      -e "s|^TRAEFIK_CONFIG_TOKEN=.*|TRAEFIK_CONFIG_TOKEN=$(openssl rand -hex 24)|" \
      -e "s|^BOOTSTRAP_STORAGE=.*|BOOTSTRAP_STORAGE=local://$ROOT/.dev/storage|" \
      -e "s|^BACKUP_DIR=.*|BACKUP_DIR=$ROOT/.dev/backups|" \
      "$ROOT/server/.env.example" >"$ENV"
  echo "created server/.env"
fi
mkdir -p "$ROOT/.dev/storage" "$ROOT/.dev/backups"
cd "$ROOT/server"
set -a; . "$ENV"; set +a
exec go run ./cmd/sparkbase-server
