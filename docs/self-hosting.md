# Self-hosting

Sparkbase runs as one Docker Compose stack: Traefik at the edge, the Sparkbase server (API and worker), the dashboard, a control database and Redis, a tenant Postgres host with PgBouncer and pgvector, a tenant Redis host, and Garage for object storage. Only Traefik publishes ports.

## Requirements

- A Linux server with Docker and Docker Compose v2. 2 vCPU and 4 GB RAM is a comfortable start (see [Sizing](sizing.md)).
- A domain you control.
- Open ports: 80, 443, 5432 (Postgres), 6432 (Postgres pooled), 6380 (Redis TLS).

## DNS

Point these records at the server (`example.com` is your `SPARKBASE_DOMAIN`):

| Record | Used for |
| --- | --- |
| `app.example.com` | Dashboard |
| `api.example.com` | API and every project URL |
| `s3.example.com` | S3 endpoint and presigned URLs |
| `*.db.example.com` | Native Postgres connections |
| `*.cache.example.com` | Native Redis connections |

## Install

```bash
git clone https://github.com/shellhaki/sparkbase && cd sparkbase/deploy
cp .env.example .env
./generate-secrets.sh
# edit .env: SPARKBASE_DOMAIN, TLS_MODE, ACME_EMAIL
docker compose up -d
```

Then open `https://app.example.com/setup` and create your account. **The first account becomes the admin and sign-up closes**, so do this right after starting. Everyone else joins through invite links from the Admin page.

To build the images from source instead of pulling them, run `docker compose build` before `up`.

## Certificates

Set `TLS_MODE` in `.env`:

| Mode | How | When |
| --- | --- | --- |
| `acme-http` (default) | Let's Encrypt HTTP-01, one certificate per hostname | Simple setups. Each new service waits for its certificate, and Let's Encrypt rate limits apply |
| `acme-dns` | Let's Encrypt DNS-01, wildcard certificates for `*.db` and `*.cache` | Recommended. Set `ACME_DNS_PROVIDER` and the provider's credentials (for example `ACME_DNS_PROVIDER=cloudflare` and `CF_DNS_API_TOKEN=...`), using [Traefik's provider names](https://doc.traefik.io/traefik/https/acme/#providers) |
| `custom` | Your certificate files | Put `fullchain.pem` and `privkey.pem` (valid for all hostnames above) in `deploy/traefik/certs` |
| `selfsigned` | Traefik's default certificate | Testing only |

Clients connect to Postgres with `sslmode=require` and to Redis with `rediss://`. Postgres clients must send SNI, which libpq does by default (`sslsni=1`, libpq 14+).

## Email

Email is optional. Without `SMTP_URL`, invite links are shown to the admin who creates them, and admins can create password reset links from the Admin page. With `SMTP_URL=smtp://user:pass@host:587`, those links are also emailed, and owners get limit warnings.

## Already running Traefik?

Remove `traefik` from `COMPOSE_PROFILES` in `.env`. Then give your Traefik:

1. Entrypoints `websecure` (443), `postgres` (5432), `postgres-pooled` (6432) and `redis` (6380). The names matter: the routes use them.
2. A file provider pointing at `deploy/traefik/dynamic` (HTTPS routes for app, api and s3), with `SPARKBASE_DOMAIN` and `TLS_MODE` in Traefik's environment.
3. An HTTP provider: `--providers.http.endpoint=http://api:8787/internal/traefik/config?token=<TRAEFIK_CONFIG_TOKEN>`.
4. A certificate resolver named `le` if you use an ACME mode.
5. Membership in the `sparkbase_web` and `sparkbase_data` networks.

## More hosts

The bundled hosts are registered on first start. To add capacity, prepare another Postgres (with the same `deploy/hosts/postgres/init.sh`) or Redis server reachable from the Sparkbase server, then register it in the dashboard (Admin → Hosts) or with the API. New services go to the least-loaded active host of their kind, weighted by each host's weight. Set a host to `draining` to stop new services landing on it.

## Upgrades

```bash
docker compose pull && docker compose up -d
```

Database migrations run automatically when the server starts and only move forward. Read the release notes in [CHANGELOG.md](../CHANGELOG.md) first; any manual step is listed there.

## Backing up the install

Back up these together:

- The control database (`control-db` volume, or `pg_dump`).
- `ENCRYPTION_KEY` from `.env`. Without it, stored credentials cannot be decrypted.
- The `pg-host`, `cache-host` and `garage-*` volumes, or use per-service backups (nightly for Postgres-family services) and Garage replication.

Keep at least one copy off the server: a backup on the same disk does not survive that disk.
