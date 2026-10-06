#!/bin/sh
# Builds Traefik's static configuration from the environment, so one compose
# file covers every certificate mode.
set -eu
args="
--entrypoints.web.address=:80
--entrypoints.websecure.address=:443
--entrypoints.postgres.address=:5432
--entrypoints.postgres-pooled.address=:6432
--entrypoints.redis.address=:6380
--entrypoints.web.http.redirections.entrypoint.to=websecure
--entrypoints.web.http.redirections.entrypoint.scheme=https
--providers.file.directory=/etc/traefik/dynamic
--providers.file.watch=true
--providers.http.endpoint=http://api:8787/internal/traefik/config?token=${TRAEFIK_CONFIG_TOKEN}
--providers.http.pollInterval=5s
--ping=true
--log.level=${TRAEFIK_LOG_LEVEL:-INFO}
"
case "${TLS_MODE:-acme-http}" in
  acme-http)
    args="$args --certificatesresolvers.le.acme.email=${ACME_EMAIL}
--certificatesresolvers.le.acme.storage=/letsencrypt/acme.json
--certificatesresolvers.le.acme.httpchallenge.entrypoint=web" ;;
  acme-dns)
    args="$args --certificatesresolvers.le.acme.email=${ACME_EMAIL}
--certificatesresolvers.le.acme.storage=/letsencrypt/acme.json
--certificatesresolvers.le.acme.dnschallenge.provider=${ACME_DNS_PROVIDER}" ;;
  custom|selfsigned|none) ;;
  *) echo "unknown TLS_MODE ${TLS_MODE}" >&2; exit 1 ;;
esac
if [ -n "${ACME_CA_SERVER:-}" ]; then
  args="$args --certificatesresolvers.le.acme.caserver=${ACME_CA_SERVER}"
fi
# shellcheck disable=SC2086
exec traefik $args
