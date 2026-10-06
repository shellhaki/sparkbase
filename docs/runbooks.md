---
title: Runbooks
description: Step-by-step procedures for operators.
---

Commands assume the Docker install in `deploy/`.

## Add a data host

1. Prepare a Postgres server with pgvector (run `deploy/hosts/postgres/init.sh` on first start for PgBouncer), or a Redis server with an ACL file. It must be reachable from the Sparkbase server and from Traefik.
2. Admin → Hosts → Add host, with its internal address and admin credentials. Sparkbase locks it down (no database is reachable by `PUBLIC`) and checks pgvector.
3. Raise or lower its weight to steer new services.

## Drain a host

Set it to `draining` in Admin → Hosts. Existing services keep running there; new ones go elsewhere. Set `offline` only when nothing should be scheduled or checked on it.

## Restore a database

Service → Backups → Restore on the backup you want. The service's current data is replaced; type the service name to confirm. To try a restore first, add a new service and restore into it with `pg_restore` from a downloaded dump.

## Restore the whole install

1. Install fresh with the **same** `ENCRYPTION_KEY` and passwords from your backed-up `.env`.
2. Restore the control database, then the `pg-host`, `cache-host` and `garage-*` volumes.
3. `docker compose up -d`. Services come back on the same hostnames.

## Rotate the encryption key

```bash
docker compose stop api
docker compose run --rm -e NEW_ENCRYPTION_KEY="$(openssl rand -base64 32)" api rotate-encryption-key
# put the new key in .env as ENCRYPTION_KEY, then:
docker compose up -d api
```

It re-encrypts every stored secret in one transaction; on error nothing changes. Keep the old key until the server is running on the new one.

## Suspected cross-tenant access

1. Suspend the affected services (Admin → Services → Suspend). Connections and API calls stop at once; data is kept.
2. Rotate their credentials (Service → Settings → Rotate) and the project keys.
3. Export the audit log (Admin → Audit log) and the API logs (`docker compose logs api`).
4. Report it privately (see SECURITY.md) with the version and what you saw.

## Certificate problems

`docker compose logs traefik | grep -i acme` shows ACME errors. Usual causes: DNS records not pointing at the server, port 80 closed (HTTP-01), wrong DNS provider credentials (DNS-01), or Let's Encrypt rate limits (switch to `acme-dns` for wildcards).
