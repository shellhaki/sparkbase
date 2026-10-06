#!/usr/bin/env bash
# Starts private Postgres and Redis instances for development and tests.
# No Docker: uses the postgres and redis-server binaries already installed.
#
#   scripts/dev-services.sh start | stop | status | reset
#   scripts/dev-services.sh garage-start | garage-stop   (optional, needs Docker)
#
# Control Postgres  : 127.0.0.1:54329  user sparkbase / sparkbase, db sparkbase
# Data host Postgres: 127.0.0.1:54330  user sb_admin / devadmin (superuser)
# Redis             : 127.0.0.1:63799  default user password devredis
# Garage (optional) : S3 127.0.0.1:39000, admin 127.0.0.1:39003, admin token devgarage
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEV="$ROOT/.dev"
CONTROL_PORT=54329
DATA_PORT=54330
REDIS_PORT=63799

find_bin() {
  local name="$1"
  # Prefer a full server install: libpq ships initdb without the postgres binary.
  for dir in ${PG_BIN:-} /opt/homebrew/opt/postgresql@18/bin /opt/homebrew/opt/postgresql@17/bin /usr/local/opt/postgresql@18/bin /usr/lib/postgresql/*/bin; do
    if [ -x "$dir/$name" ]; then echo "$dir/$name"; return; fi
  done
  if command -v "$name" >/dev/null 2>&1; then command -v "$name"; return; fi
  echo "missing binary: $name" >&2
  exit 1
}

INITDB="$(find_bin initdb)"
PG_CTL="$(find_bin pg_ctl)"
PSQL="$(find_bin psql)"
REDIS="$(find_bin redis-server)"
REDIS_CLI="$(find_bin redis-cli)"

init_cluster() {
  local dir="$1" user="$2" pass="$3" port="$4"
  if [ -f "$dir/PG_VERSION" ]; then return; fi
  mkdir -p "$dir"
  local pwfile
  pwfile="$(mktemp)"
  printf '%s' "$pass" >"$pwfile"
  "$INITDB" -D "$dir" -U "$user" --pwfile="$pwfile" --auth=scram-sha-256 -E UTF8 --locale=C >/dev/null
  rm -f "$pwfile"
  cat >>"$dir/postgresql.conf" <<EOF
port = $port
listen_addresses = '127.0.0.1'
unix_socket_directories = '$dir'
max_connections = 200
shared_buffers = 64MB
fsync = off
synchronous_commit = off
full_page_writes = off
EOF
}

start_pg() {
  local dir="$1" name="$2"
  if "$PG_CTL" -D "$dir" status >/dev/null 2>&1; then return; fi
  "$PG_CTL" -D "$dir" -l "$DEV/$name.log" -w start >/dev/null
}

start() {
  mkdir -p "$DEV"
  init_cluster "$DEV/pg-control" sparkbase sparkbase "$CONTROL_PORT"
  init_cluster "$DEV/pg-data" sb_admin devadmin "$DATA_PORT"
  start_pg "$DEV/pg-control" pg-control
  start_pg "$DEV/pg-data" pg-data

  PGPASSWORD=sparkbase "$PSQL" -h 127.0.0.1 -p "$CONTROL_PORT" -U sparkbase -d postgres -tAc \
    "select 1 from pg_database where datname='sparkbase'" | grep -q 1 ||
    PGPASSWORD=sparkbase "$PSQL" -h 127.0.0.1 -p "$CONTROL_PORT" -U sparkbase -d postgres -qc "create database sparkbase"
  PGPASSWORD=sparkbase "$PSQL" -h 127.0.0.1 -p "$CONTROL_PORT" -U sparkbase -d postgres -tAc \
    "select 1 from pg_database where datname='sparkbase_test'" | grep -q 1 ||
    PGPASSWORD=sparkbase "$PSQL" -h 127.0.0.1 -p "$CONTROL_PORT" -U sparkbase -d postgres -qc "create database sparkbase_test"

  if ! "$REDIS_CLI" -p "$REDIS_PORT" -a devredis --no-auth-warning ping >/dev/null 2>&1; then
    mkdir -p "$DEV/redis"
    touch "$DEV/redis/users.acl"
    if ! grep -q '^user default' "$DEV/redis/users.acl"; then
      echo 'user default on >devredis ~* &* +@all' >"$DEV/redis/users.acl"
    fi
    "$REDIS" --port "$REDIS_PORT" --bind 127.0.0.1 --dir "$DEV/redis" \
      --aclfile "$DEV/redis/users.acl" --daemonize yes --logfile "$DEV/redis.log" \
      --save '' --appendonly no >/dev/null
    for _ in $(seq 1 50); do
      "$REDIS_CLI" -p "$REDIS_PORT" -a devredis --no-auth-warning ping >/dev/null 2>&1 && break
      sleep 0.1
    done
  fi
  status
}

stop() {
  "$PG_CTL" -D "$DEV/pg-control" -m fast stop >/dev/null 2>&1 || true
  "$PG_CTL" -D "$DEV/pg-data" -m fast stop >/dev/null 2>&1 || true
  "$REDIS_CLI" -p "$REDIS_PORT" -a devredis --no-auth-warning shutdown nosave >/dev/null 2>&1 || true
  echo "stopped"
}

status() {
  local ok=0
  "$PG_CTL" -D "$DEV/pg-control" status >/dev/null 2>&1 && echo "control postgres  127.0.0.1:$CONTROL_PORT  up" || { echo "control postgres  down"; ok=1; }
  "$PG_CTL" -D "$DEV/pg-data" status >/dev/null 2>&1 && echo "data postgres     127.0.0.1:$DATA_PORT  up" || { echo "data postgres     down"; ok=1; }
  "$REDIS_CLI" -p "$REDIS_PORT" -a devredis --no-auth-warning ping >/dev/null 2>&1 && echo "redis             127.0.0.1:$REDIS_PORT  up" || { echo "redis             down"; ok=1; }
  return $ok
}

GARAGE_IMAGE="${GARAGE_IMAGE:-dxflrs/garage:v2.4.1}"

garage_start() {
  mkdir -p "$DEV/garage/meta" "$DEV/garage/data"
  cat >"$DEV/garage/garage.toml" <<TOML
metadata_dir = "/var/lib/garage/meta"
data_dir = "/var/lib/garage/data"
db_engine = "sqlite"
replication_factor = 1
rpc_bind_addr = "[::]:3901"
rpc_public_addr = "127.0.0.1:3901"
rpc_secret = "0000000000000000000000000000000000000000000000000000000000000001"

[s3_api]
s3_region = "garage"
api_bind_addr = "[::]:3900"

[admin]
api_bind_addr = "[::]:3903"
admin_token = "devgarage"
TOML
  if ! docker ps --format '{{.Names}}' | grep -q '^sparkbase-dev-garage$'; then
    docker rm -f sparkbase-dev-garage >/dev/null 2>&1 || true
    docker run -d --name sparkbase-dev-garage -p 127.0.0.1:39000:3900 -p 127.0.0.1:39003:3903 \
      -v "$DEV/garage/garage.toml:/etc/garage.toml:ro" -v "$DEV/garage/meta:/var/lib/garage/meta" \
      -v "$DEV/garage/data:/var/lib/garage/data" "$GARAGE_IMAGE" >/dev/null
  fi
  for _ in $(seq 1 50); do
    curl -sf -H "Authorization: Bearer devgarage" http://127.0.0.1:39003/v2/GetClusterStatus >/dev/null 2>&1 && { echo "garage            127.0.0.1:39000  up"; return; }
    sleep 0.2
  done
  echo "garage did not start" >&2
  docker logs sparkbase-dev-garage 2>&1 | tail -20 >&2
  exit 1
}

case "${1:-start}" in
  garage-start) garage_start ;;
  garage-stop) docker rm -f sparkbase-dev-garage >/dev/null 2>&1 || true; echo "garage stopped" ;;
  start) start ;;
  stop) stop ;;
  status) status ;;
  reset) stop; rm -rf "$DEV"; start ;;
  *) echo "usage: $0 start|stop|status|reset" >&2; exit 2 ;;
esac
