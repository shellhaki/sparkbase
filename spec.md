# Sparkbase: Product & Technical Spec

Oct 6, 2026 · @Excel

## Overview

Sparkbase is an open-source, self-hosted backend platform. You run it on your own server, create a project, and add only the services your app needs. Data services hold your data: a Postgres database, a cache, object storage, a vector database, queues and an auth API. Platform services do work around it: realtime channels, cron jobs, scheduled backups and uptime monitoring. Each service is reachable two ways:

1. **Through the project API and SDK.** Every project has a **project URL** and **project API keys**, like Supabase. `createClient(projectUrl, apiKey)` gives your app every service in the project over HTTPS.
2. **Natively.** Services that have a standard protocol (Postgres, Redis, S3) also hand out a native connection string, so existing drivers, ORMs and tools keep working.

Nothing is sold: there is no paid tier, no hosted edition and no feature gating.

**Pitch:** add the backend services your app needs to a project, on your own server, and use them from one SDK.

**Who it is for**

- Solo developers and indie hackers who want Supabase-style convenience on a VPS they already pay for.
- Small teams, agencies, classrooms and clubs where one person runs the server and invites the others.
- Developers who want a backend platform without handing their data to a third party.

**Positioning.** Supabase and Firebase bundle a fixed set of services around one database. Running Postgres, Redis, an S3 server, a vector store, a queue and an auth server by hand means doing credentials, limits, backups and an API for each. Sparkbase makes each of them a service you add to a project in one click, with isolation, limits, keys and one SDK already done.

**Product principles**

1. **Open source, all of it.** One codebase, one edition. Self-hosting is the product.
2. **Pick your services.** A project starts empty. You add the services you need, and only those run for you.
3. **One URL, one key, one SDK.** Every service in a project is reachable from the project URL with a project API key.
4. **Native when it exists.** Services with a standard protocol also give a native connection string. The SDK is a convenience, never a wall.
5. **Limits protect the server.** The operator sets limits per service so one project cannot take down the others.
6. **Memory only while in use.** Databases and caches run in their own containers with a memory limit and stop when idle. The next connection or API call starts them again.
7. **See everything.** Every service has live logs and metrics; databases and caches have a terminal.
8. **Boring technology.** Go, Postgres, Redis, plain SQL with no ORM. Traefik only for the network edge.

## Product model

The product is a few nouns: an **operator** runs an install, **users** own **projects**, a project holds **services** (called resources in the API and database), each service sits on a **plan** that sets its limits, and each project has **project API keys**.

| Concept | What it is | Notes |
| --- | --- | --- |
| Install | One Sparkbase deployment on a server (or a few) | Run by one operator |
| User | A person with a login and account API keys | Role `admin` or `member`. Admins manage users, hosts and plans |
| Project | A named container with a project URL | Has a short `ref` (slug), e.g. `a1b2c3`, used in its URL and hostnames |
| Service (resource) | One backend service inside a project | Has a kind, a name, a plan, a host, a status and credentials |
| Project API key | Key an app uses to call the project's services | Publishable (`sbp_`, safe in browsers) or secret (`sbs_`, server only) |
| Plan | A named limits preset for one service kind | Defined by the admin. No prices. Seeded with Small, Medium and Large |
| Host | A server that holds many services of one engine | Invisible to members; chosen by the scheduler. Used by the shared-host engine |
| Runtime | Where a service runs: `container` (its own container), `shared` (a slice of a shared host) or `control` (inside the server) | Set at creation from the engine and the kind |

**How people sign in.** Email and password. Self-hosted installs often have no SMTP server, so nothing depends on email: invite links are shown to the admin who creates them, and an admin can create a password reset link for any user. When SMTP is configured, invites and reset links are also emailed.

**Who can sign up.** Only once: the first account created on a fresh install becomes the admin, and sign-up closes. Everyone else joins through an invite link from an admin and then signs in. Finish setup right after starting a new install, because until then the first visitor would become the admin.

**Service catalog**

| Service | Kind | Runtime | Engine | Project API | Native access |
| --- | --- | --- | --- | --- | --- |
| Database | `postgres` | container | PostgreSQL 18 | SQL over HTTP, schema, extensions | Postgres wire over TLS |
| Cache | `cache` | container | Redis (Valkey-compatible) | Key/value get, set, delete, batch, console | RESP over TLS |
| Object storage | `storage` | shared | Garage (S3) or local disk | Upload, download, list, delete, signed URLs | S3 API (Garage backend) |
| Vector database | `vector` | container | Postgres with pgvector | Collections, upsert, similarity query | Postgres wire |
| Queues | `queue` | container | Postgres (`SKIP LOCKED`) | Create queues, send, receive, ack, nack | none |
| Auth | `auth` | container | Postgres, JWT | Email and password sign-up and sign-in, refresh, recovery, optional magic link or code, admin users | none |
| Realtime | `realtime` | control | Redis pub/sub, Postgres `LISTEN` | Broadcast, subscribe (Server-Sent Events), table change feeds | none |
| Cron | `cron` | control | Server scheduler | Jobs that call a URL, run SQL or send a queue message | none |
| Backups | `backup` | control | `pg_dump` | Schedule, retention, destination (this server or S3), runs | none |
| Monitor | `monitor` | control | Server scheduler | HTTP checks, results, alert webhook | none |
| NoSQL | `mongo` | | MongoDB | later | Mongo wire |

With the shared-host engine (`ENGINE=shared`), Postgres-family and cache services are slices of shared hosts instead of containers. The project API is the same either way.

A project can have many services of each kind, except `auth`: a project has at most one auth service, because end users belong to the project.

**The core user flow**

1. The operator runs the install, creates the first account (the admin) and invites people.
2. A user signs in and creates a project. The project gets a URL and two API keys.
3. They add services: pick a kind, a name and a plan.
4. They wait a few seconds while each one provisions.
5. Their app calls `createClient(projectUrl, key)` and uses `sb.postgres('main')`, `sb.cache('sessions')`, `sb.storage('uploads')`, `sb.vector('docs')`, `sb.queue('jobs')`, `sb.auth`, `sb.realtime('live')`, `sb.cron('tasks')`, `sb.monitor('uptime')` and `sb.backup('nightly')`. Or it copies a native connection string.
6. They watch each service's live logs and metrics in the dashboard, and use the terminal to look inside a database or cache.

**Service lifecycle**

| Status | Meaning | Reached by |
| --- | --- | --- |
| provisioning | Worker is creating it on a host | Create call |
| active | Healthy and usable. A container service may be asleep (stopped) and wakes on the next connection or call | Provisioning succeeded, or resume |
| paused | Requests and connections refused, data kept | Owner or admin action |
| suspended | Blocked by an admin, data kept | Admin action |
| failed | Provisioning did not finish | Retries exhausted; owner can retry or delete |
| deleting | Teardown in progress | Delete call |

## Architecture

Sparkbase splits into a control plane that decides things and a data plane that serves them. The control plane is one Go binary that runs the API (management API and project API), the worker and the activator. The data plane is the engines: one container per database or cache service (or shared hosts), and object storage.

```text
          app (SDK)          dashboard / CLI           native clients
              │                    │                         │
              ▼                    ▼                         ▼
   ┌─────────────────────────────────────────┐     ┌─────────────────┐
   │ Traefik (edge only: HTTPS, TLS, SNI)     │────►│ native TCP: pg, │
   └───────────────┬─────────────────────────┘     │ redis, s3       │
                   ▼                               └───────┬─────────┘
   ┌──────────────────────────────┐                        │
   │ sparkbase-server             │  project API:          │
   │  ├─ management API  /v1      │  SQL, kv, objects,     ▼
   │  ├─ project API     /p/{ref} │──vectors, queues,──► activator ──► data plane
   │  ├─ worker (jobs, schedules) │  auth, realtime,     (wakes        one container per
   │  └─ activator (TCP proxies)  │  cron, monitor,       sleeping     Postgres-family or
   └──────────────┬───────────────┘  backups              services)    cache service;
                  ▼                                                     Garage / local disk
     control-plane Postgres + Redis (also live logs, metrics, realtime pub/sub)
```

**Components**

| Component | Role | Technology |
| --- | --- | --- |
| API | Management API (`/v1`), project API (`/p/{ref}`), Traefik dynamic config | Go, `net/http` |
| Worker | Provisioning, usage sampling, limit enforcement, scale to zero, backups, cron runs, uptime checks, email | Go, same binary |
| Container engine | Creates, starts, stops and removes one container per Postgres-family or cache service through the Docker Engine API | Go, Docker socket, no SDK |
| Activator | One TCP proxy per container service; starts a sleeping container on the first connection | Go, same binary |
| Control DB | Users, projects, keys, services, plans, hosts, jobs, audit log | Postgres |
| Control Redis | Rate limits, request counters, live log buffers, per-minute metrics, realtime pub/sub | Redis |
| Service containers | One per `postgres`, `vector`, `queue`, `auth` or `cache` service, with the plan's memory limit | `pgvector/pgvector:pg18`, `redis:8-alpine` |
| Postgres hosts (shared engine) | Tenant databases for Postgres-family services | Postgres (+ PgBouncer, pgvector) |
| Redis hosts (shared engine) | Tenant cache namespaces | Redis or Valkey with ACLs |
| Object storage | One bucket and one key per service (Garage), or one directory per service (local) | Garage or the local filesystem |
| Traefik | Edge proxy: HTTPS for the API and dashboard; TLS and SNI routing for native Postgres and Redis | Traefik v3 |
| Dashboard | Web UI over the management and project APIs | Next.js on Bun |

**Architecture rules**

- **The project API is the main way in.** Every service is usable over HTTPS from the project URL with a project key. Native access is an extra for services that have a protocol.
- **Traefik is networking only.** It terminates TLS and routes. No product logic lives in it. The API and worker run and are tested without it.
- **Hosts are interchangeable.** A host is a row with a weight and a status. Adding capacity is registering another host.
- **One stable name per service.** Users never see which host holds their data, which makes moves possible.
- **Secrets live in one place.** Credentials are stored encrypted in the control DB and decrypted only by the API and worker.
- **Every long operation is a job.** The API answers fast with a status, the worker does the slow part, and retries are safe because each step is idempotent.
- **No vendor assumptions.** Configuration is environment variables. Nothing is hardcoded to a cloud provider.
- **Memory follows use.** A service that nobody is using should not hold memory. Containers stop when idle and wake on demand.

## Container engine and scale to zero

The Docker install runs with `ENGINE=containers` and needs Docker on the server. Databases and caches are the services that hold memory, so each one runs in its own container. Development and tests can use `ENGINE=shared`, which provisions on shared hosts with no Docker.

**One container per service.** Postgres-family services (`postgres`, `vector`, `queue`, `auth`) run in `pgvector/pgvector:pg18`; cache services run in `redis:8-alpine`. Each container:

- gets the plan's `memory_mb` as a hard memory limit, and Postgres settings (`shared_buffers`, `max_connections`) sized to fit it;
- keeps its data in a named volume, so stopping it loses nothing;
- joins the internal data network (`DOCKER_NETWORK`) and publishes no ports;
- has its own admin secret, stored encrypted in `resources.admin_secret_enc`. Tenant isolation inside the container is the same as on a shared host: the app uses its own login role (`sbu_*`) or its own ACL user, never the admin.

**Scale to zero.**

1. Every container service gets a fixed proxy port (`resources.proxy_port`, from `PROXY_BASE_PORT`, default 20000). The activator listens there. Traefik routes native TCP traffic to `ACTIVATOR_HOST:proxy_port`.
2. Open connections are counted in Redis. The project API marks the service as used on every call.
3. The `scale_to_zero` job runs every minute. A container with no open connections, no API calls for `idle_minutes` (admin setting, default 10) and no live table subscription is stopped.
4. The next native connection reaches the activator, which starts the container, waits until it accepts connections (usually under half a second) and then pipes bytes both ways. A project API call wakes the container the same way before it runs.
5. Paused and suspended services stay stopped and the activator refuses their connections.

**Status.** The resource JSON carries `runtime` (`container`, `shared` or `control`) and, for containers, `awake`. The dashboard shows sleeping services as asleep.

**Deleting.** Deprovisioning removes the container and its volume.

## Platform services

Platform services have `runtime = control`: they run inside the server and keep their state in the control database. They use no host and no container, and have no native credentials.

| Service | How it works | Limits (Small) |
| --- | --- | --- |
| `realtime` | `POST /broadcast` publishes JSON to a channel through Redis pub/sub. `GET /subscribe?channel=` streams messages as Server-Sent Events. Channels starting with `public:` accept the publishable key (key in `?apikey=` because `EventSource` cannot send headers); others need the secret key. **Table watches** install a trigger on a table in a Postgres service in the same project; the trigger calls `pg_notify`, and the server `LISTEN`s and republishes on `db:SERVICE:TABLE`. A database with a live watch is never stopped by scale to zero | 200 subscribers, 1M messages a month |
| `cron` | Jobs with a 5-field cron expression (UTC) and one action: `http` (call a URL), `sql` (run a statement on a Postgres service in the project, as its role) or `queue` (send a message to a queue service). The worker runs due jobs, records each run with status, duration and output, and logs it | 10 jobs, 60 s minimum interval |
| `backup` | A policy: which Postgres-family services, a cron schedule, retention days and a destination: the owner's own bucket (`r2`: Cloudflare R2 from an account ID, endpoint derived, region `auto`; or `s3`: any S3-compatible endpoint), or `local` on this server as a fallback. Saving a bucket destination writes and deletes a test object first; the secret is stored encrypted. Each run is a `pg_dump` per database (extensions excluded so restore works as the tenant role); restore happens from the database's own Backups tab | 7 days, 5 databases |
| `monitor` | HTTP checks (URL, method, expected status, interval, timeout). Results are kept, failures and recoveries notify the owner and `POST` JSON to an optional alert webhook | 5 checks, 60 s minimum interval |

**Outbound requests.** Cron `http` actions, uptime checks, alert webhooks and S3 backup endpoints go through a guard that blocks loopback, private, link-local and metadata addresses, after DNS resolution. The admin setting `allow_private_targets` turns this off for installs that need to reach their own network.

## Logs, metrics and terminals

**Live logs.** Every service has a log stream with two sources:

- `api`: every project API request to the service (method, path, status, duration, error code), plus events of platform services (cron runs, check results, backup runs).
- `container`: the container's own output, streamed from Docker. Postgres logs connections and statements slower than one second.

Lines are kept in a capped Redis list per service and published on Redis pub/sub, so a tail can start with recent history and then follow. The server's own JSON logs go through the same hub (`/v1/admin/logs`) for admins. Redaction runs before anything is stored: passwords, keys, tokens and connection strings never reach a log.

**Metrics.** Per-minute counters in Redis hashes (requests, errors, total latency) and usage samples (storage, memory, CPU for containers). `GET /v1/resources/{id}/metrics?minutes=60` returns series for charts. Prometheus metrics for the whole install stay at `/metrics`.

**Terminals.** Kept small on purpose:

- Postgres-family: a psql-like console over `POST /postgres/{name}/query` with `\dt`, `\d TABLE`, `\dx` and `\?`.
- Cache: a Redis console over `POST /cache/{name}/command`, run as the service's ACL user, so denied commands stay denied.

Both run as the service's own credentials and can do nothing the app cannot.

## Project URL and API keys

Every project has one URL and two kinds of project key.

**Project URL.** `https://api.<domain>/p/<ref>`, for example `https://api.example.com/p/a1b2c3`. In development, `http://localhost:8787/p/a1b2c3`. Path-based URLs need no extra DNS records.

**Project keys.** Created with the project and rotatable at any time.

| Key | Prefix | Where it goes | Can do |
| --- | --- | --- | --- |
| Publishable | `sbp_` | Browsers and mobile apps | Auth public routes (sign up, sign in, refresh, recover, sign out, current user) and nothing else |
| Secret | `sbs_` | Servers only | Every project API route for every service in the project |

Keys are sent in the `apikey` header, or as `Authorization: Bearer <key>` when no end-user token is sent. An end user's access token from the auth service goes in `Authorization: Bearer <jwt>` together with the `apikey` header.

Keys are stored as a SHA-256 hash for lookup and AES-256-GCM encrypted so the owner can view them again in the dashboard. A user's own session or account API key (`sb_`) also works on the project API for projects they own, which is how the dashboard browses data.

Row-level access for end users (anonymous or signed-in users reading data under policies) is a later phase. Until then, data access from untrusted clients goes through the developer's own server with the secret key, or through signed storage URLs.

## Networking, TLS and Traefik

Traefik is the network edge and nothing more. It is the only container that publishes ports in the Docker install. Everything else lives on internal networks.

**Hostnames.** The operator sets one base domain, `SPARKBASE_DOMAIN`, for example `example.com`.

| Hostname | Routed to | Protocol |
| --- | --- | --- |
| `app.<domain>` | Dashboard | HTTPS |
| `api.<domain>` | API, including every project URL | HTTPS |
| `s3.<domain>` | Garage S3 API (Garage backend only) | HTTPS |
| `<name>-<ref>.db.<domain>` | The service's Postgres host (`postgres` and `vector`) | Postgres wire, TLS |
| `<name>-<ref>.cache.<domain>` | The service's Redis host | RESP, TLS |

DNS records: `app`, `api` and `s3`, plus wildcards `*.db` and `*.cache`.

**Ports.** `80`, `443`, `5432` (Postgres direct), `6432` (Postgres pooled) and `6380` (Redis TLS).

**TLS for Postgres and Redis.** Traefik terminates TLS and routes by SNI. Postgres clients negotiate TLS with STARTTLS, which Traefik v3 supports on TCP routers, and libpq sends SNI by default. Clients use `sslmode=require`. Redis clients use `rediss://`.

**Certificates.** `TLS_MODE` is `acme-dns` (wildcards, recommended), `acme-http` (one certificate per hostname), `custom` (mounted files) or `selfsigned` (testing only).

**Dynamic routing.** Traefik polls `GET /internal/traefik/config` (internal network, shared token). It returns one TCP router per active native service with `HostSNI` on its endpoint. Container services route to the activator (`ACTIVATOR_HOST:proxy_port`), so a sleeping service still has a route and wakes on connect. Paused, suspended and deleted services get no router.

**Without Traefik.** For development and for installs that only use the project API, Sparkbase runs without Traefik: the API listens on `PORT` and native connection strings point straight at the host address. `NATIVE_MODE=direct` selects this.

## Tenancy and isolation

An install runs shared hosts, and every service gets an isolated slice of one. Users on the same install must not reach each other's data. Treat that as the core promise of the project.

**Per-engine isolation**

| Service | Unit of isolation | Access control |
| --- | --- | --- |
| `postgres` | One database plus one role | `REVOKE CONNECT ... FROM PUBLIC` on every database, non-superuser roles, `scram-sha-256` |
| `vector` | One database plus one role (pgvector inside) | Same as `postgres` |
| `queue` | One database plus one role | Same as `postgres`; native access not offered |
| `auth` | One database plus one role; one JWT signing secret | Same as `postgres`; native access not offered |
| `cache` | One ACL user restricted to a key prefix | `~prefix:*`; `KEYS`, `FLUSHALL`, `FLUSHDB`, `CONFIG`, `DEBUG`, `SCRIPT`, `EVAL`, `EVALSHA`, `FUNCTION`, `MODULE`, `ACL`, `SHUTDOWN`, `MIGRATE` and admin commands denied |
| `storage` (Garage) | One bucket plus one access key | The key is allowed on its own bucket only |
| `storage` (local) | One directory | Paths are cleaned and must stay inside the directory; only the API touches the disk |

**Postgres host specifics**

- When a host is registered, the worker revokes `CONNECT` from `PUBLIC` on every existing database (`postgres`, `template1`), so tenant roles can reach only their own database.
- Per-role `CONNECTION LIMIT`, `statement_timeout`, `idle_in_transaction_session_timeout` and `work_mem`.
- Extensions allowlist: `pgcrypto`, `uuid-ossp`, `pg_trgm`, `vector`. Never `dblink`, `file_fdw`, `postgres_fdw` or untrusted languages.
- Never grant `pg_read_server_files`, `pg_write_server_files` or `pg_execute_server_program`.
- PgBouncer in transaction mode for the pooled port when configured on the host; `auth_query` so new roles work without config edits.
- The project API runs every statement as the service's own role, never as an admin role, so its limits and isolation are the same as a native connection.

**Vector specifics.** When the host has the `vector` extension, collections use a `vector(n)` column and an HNSW index. When it does not (for example a development Postgres), the driver falls back to a `real[]` column and exact search in SQL. The API is the same either way.

**Queue specifics.** Each queue is a row in the service's `queues` table, and messages live in its `messages` table. Receiving uses `FOR UPDATE SKIP LOCKED` and sets a visibility timeout. A message received more than `max_receives` times moves to the dead-letter state.

**Auth specifics.** Email and password first: `POST /auth/signup` and `POST /auth/token?grant_type=password` return a session. Passwords are argon2id. `POST /auth/recover` emails a one-time link and 6-digit code; verifying it returns a session and the app sets a new password with `PUT /auth/user`. The same link and code flow (`POST /auth/otp`, `POST /auth/verify`) is available for passwordless sign-in. Links only go to the site URL or an allowlisted redirect. Without SMTP, the app's server calls `POST /auth/admin/generate-link` with the secret key and sends the email itself. Access tokens are HS256 JWTs signed with the service's secret, valid for one hour by default; refresh tokens are random, stored hashed, and rotate on every use. The JWT secret is shown to the owner so their own server can verify tokens.

**Storage specifics.** Two backends, chosen per storage host:

1. **Garage** (Docker install default): one bucket `sb-<resource_id>` and one access key per service, managed through Garage's admin API. Native S3 at `s3.<domain>` with path-style addressing, and presigned URLs signed with SigV4. Quotas are Garage bucket quotas.
2. **Local** (development and single-disk installs): one directory per service under the host's root. The project API reads and writes objects, and signed URLs are HMAC-signed API URLs. The quota is checked by the API before each write.

**Limit enforcement**

| Limit | Mechanism | Checked | Action on breach |
| --- | --- | --- | --- |
| Postgres, vector, queue, auth storage | `pg_database_size()` sampled | Every 5 min | Warn at 80% and 90%; at 100% set `default_transaction_read_only = on` for the role |
| Direct connections | Role `CONNECTION LIMIT` | Hard | New connections refused |
| Query time | Role `statement_timeout` | Hard | Query cancelled |
| Cache memory | Sampled key scan with `MEMORY USAGE` | Every 1 min | ACL user switched to read and delete only |
| Cache clients | `CLIENT LIST` by user | Every 1 min | Excess clients killed |
| Storage bytes | Garage bucket quota, or API check (local) | Hard | Writes refused |
| Project API requests | Counters in Redis per service | Rolling month | `quota_exceeded` |
| Auth users | Count of users | On signup | `quota_exceeded` |

Every breach writes an `audit_log` row and a notification for the owner, and clears once usage drops or the plan changes.

**Pausing.** Postgres-family roles get `NOLOGIN` and connections are terminated, the cache ACL user is disabled, the storage key loses its bucket permission (or the API refuses the directory), and the Traefik router is removed. Project API calls return `resource_not_ready`.

## Control plane data model

The control plane keeps its metadata in a dedicated Postgres database, separate from every tenant host. Migrations are numbered `.sql` files embedded in the binary.

```sql
create extension if not exists pgcrypto;

create table users (
  id                uuid primary key default gen_random_uuid(),
  email             text not null unique,
  password_hash     text not null,     -- argon2id
  name              text,
  role              text not null default 'member' check (role in ('admin','member')),
  failed_logins     int not null default 0,
  locked_until      timestamptz,
  last_login_at     timestamptz,
  disabled_at       timestamptz,
  created_at        timestamptz not null default now()
);

create table sessions (
  id         uuid primary key default gen_random_uuid(),
  user_id    uuid not null references users(id) on delete cascade,
  token_hash text not null unique,
  expires_at timestamptz not null,
  created_at timestamptz not null default now()
);

create table auth_tokens (           -- setup, password resets, invites
  id         uuid primary key default gen_random_uuid(),
  kind       text not null check (kind in ('setup','password_reset','invite')),
  user_id    uuid references users(id) on delete cascade,
  email      text,
  token_hash text not null unique,
  role       text check (role in ('admin','member')),
  invited_by uuid references users(id) on delete set null,
  expires_at timestamptz not null,
  used_at    timestamptz,
  created_at timestamptz not null default now()
);

create table settings (
  key        text primary key,
  value      jsonb not null,
  updated_at timestamptz not null default now()
);

create table projects (
  id         uuid primary key default gen_random_uuid(),
  owner_id   uuid not null references users(id) on delete cascade,
  slug       text not null unique,      -- the project ref
  name       text not null,
  status     text not null default 'active' check (status in ('active','suspended','deleting')),
  created_at timestamptz not null default now()
);

create table project_keys (
  id          uuid primary key default gen_random_uuid(),
  project_id  uuid not null references projects(id) on delete cascade,
  kind        text not null check (kind in ('publishable','secret')),
  prefix      text not null,
  key_hash    text not null unique,
  key_enc     bytea not null,
  created_at  timestamptz not null default now(),
  revoked_at  timestamptz
);

create table hosts (
  id               uuid primary key default gen_random_uuid(),
  kind             text not null check (kind in ('postgres','cache','storage')),
  name             text not null unique,
  internal_addr    text not null,
  config           jsonb not null default '{}',
  admin_secret_enc bytea not null,
  weight           int not null default 100,
  status           text not null default 'active' check (status in ('active','draining','offline')),
  created_at       timestamptz not null default now()
);

create table plans (
  id         text primary key,          -- e.g. 'postgres-small'
  kind       text not null,
  name       text not null,
  limits     jsonb not null,
  is_default boolean not null default false,
  active     boolean not null default true
);

create table resources (
  id              uuid primary key default gen_random_uuid(),
  project_id      uuid not null references projects(id) on delete cascade,
  kind            text not null check (kind in ('postgres','cache','storage','vector','queue','auth')),
  name            text not null,
  plan_id         text not null references plans(id),
  host_id         uuid references hosts(id),
  status          text not null default 'provisioning',
  external_name   text,                 -- db name, key prefix, bucket or directory
  endpoint        text,
  provision_state jsonb not null default '{}',
  write_locked    boolean not null default false,
  last_error      text,
  last_active_at  timestamptz,
  created_at      timestamptz not null default now(),
  unique (project_id, name)
);

create table resource_credentials (
  id          uuid primary key default gen_random_uuid(),
  resource_id uuid not null references resources(id) on delete cascade,
  username    text not null,
  secret_enc  bytea not null,
  extra_enc   bytea,                     -- e.g. the auth JWT secret
  created_at  timestamptz not null default now(),
  revoke_at   timestamptz,
  revoked_at  timestamptz
);

-- api_keys, usage_samples, backups, jobs, audit_log and notifications
-- are in the migration file.

-- 0003: containers and platform services
alter table resources add column runtime text not null default 'shared'
  check (runtime in ('shared','container','control'));
alter table resources add column container_name   text;
alter table resources add column admin_secret_enc bytea;      -- container superuser
alter table resources add column proxy_port       int unique;  -- activator port
alter table resources add column settings         jsonb not null default '{}';
-- resource and plan kinds gain 'realtime','cron','backup','monitor';
-- plans gain memory_mb for container kinds.

create table cron_jobs (...);        -- schedule, action jsonb, enabled, next_run_at
create table cron_runs (...);        -- status, duration, output
create table monitors (...);         -- url, method, interval, timeout, expect_status, state
create table monitor_results (...);  -- ok, status, latency, error
-- backups gain destination ('local' or 's3') and via_resource_id (the backup service).
```

The full schema lives in `server/internal/db/migrations/` (`0001_init.sql`, `0002_seed.sql`, `0003_containers_and_services.sql`).

**Host kinds and service kinds.** With the shared engine, `postgres`, `vector`, `queue` and `auth` services live on `postgres` hosts and `cache` services on `cache` hosts. With the container engine they get their own container and no host. `storage` services always live on `storage` hosts. Platform services use no host.

## Backend plan

The backend is one Go module. One binary, `sparkbase-server`, runs the API, the worker, or both. The CLI, `sparkbase`, is a second binary in the same module. All SQL is hand-written. There is no ORM and no query builder.

**Stack**

| Concern | Choice | Why |
| --- | --- | --- |
| Language | Go (current release) | One static binary, strong standard library |
| HTTP | `net/http` with method and path patterns | No framework needed |
| Postgres driver | `jackc/pgx/v5` | Fast, typed, parameterized |
| Redis driver | `redis/go-redis/v9` | Mature, supports ACL commands |
| Validation | Small typed request structs with explicit checks | No reflection magic |
| Migrations | Embedded numbered `.sql` files plus a small runner | Forward-only |
| Passwords and keys | argon2id (`x/crypto`) for passwords; SHA-256 for keys and tokens | Keys are high-entropy |
| Logging and metrics | `log/slog` JSON; Prometheus text format | Standard library first |
| Tests | `go test` against real Postgres and Redis | Mocks hide the bugs that matter |
| Dashboard | Next.js (App Router) on Bun | Same stack as the landing page |
| SDK | TypeScript, built with Bun, zero dependencies | Runs in Node, Bun, Deno, browsers and edge |

**Repository layout**

```text
sparkbase/
  server/                       Go module (AGPL-3.0)
    cmd/sparkbase-server/       API, worker and activator binary
    cmd/sparkbase/              CLI (MIT)
    internal/
      config/ logx/ sbcrypto/ httpx/ mail/ core/
      db/                       embedded migrations
      store/                    hand-written SQL, one file per area
      api/                      management API, project API, Traefik config, logs and metrics
      app/                      lifecycle, provisioning, ops, platform service logic
      worker/                   job loop and schedules
      drivers/                  postgres family, cache, storage (local, Garage)
      engine/                   container engine and activator
      dockerx/                  Docker Engine API client over the unix socket
      tenant/                   per-service connection pools
      logs/                     live log hub and slog tee
      cronexpr/ netguard/ s3/ jwt/
      cli/                      CLI commands (MIT)
      testenv/                  integration test helpers
      e2e/                      Docker compose end-to-end test (build tag e2e)
  apps/dashboard/               Next.js on Bun; also serves /docs
  packages/sdk-js/              @sparkbase/sdk (MIT)
  docs/                         Markdown docs and nav.json, rendered at /docs
  deploy/                       docker-compose.yml, Traefik, Garage, host init, .env.example
  examples/node-quickstart/
  scripts/                      dev services, dev runners, e2e scripts
  .github/workflows/            ci, release, dco
  Makefile  README.md  LICENSE  CONTRIBUTING.md  SECURITY.md  CODE_OF_CONDUCT.md  CHANGELOG.md  DECISIONS.md
```

`waitlist/` (the landing page) is a separate app and not part of the product.

**The driver contract.** Every service kind implements one interface. The API and worker only talk to this interface.

```go
type Driver interface {
    Kind() string
    Provision(ctx context.Context, r *Resource, h *Host, limits Limits) (*Provisioned, error)
    Deprovision(ctx context.Context, r *Resource, h *Host) error
    Pause(ctx context.Context, r *Resource, h *Host) error
    Resume(ctx context.Context, r *Resource, h *Host) error
    RotateCredentials(ctx context.Context, r *Resource, h *Host) (*Credentials, error)
    ApplyLimits(ctx context.Context, r *Resource, h *Host, limits Limits) error
    MeasureUsage(ctx context.Context, r *Resource, h *Host) (Usage, error)
    SetWriteLock(ctx context.Context, r *Resource, h *Host, locked bool) error
}
```

`postgres`, `vector`, `queue` and `auth` share one Postgres-family implementation for provisioning, and each adds its own schema setup and project API handlers. With the container engine, provisioning first creates and starts the container, then runs the same driver steps against it as its host. Platform services skip the driver: they are created active, with their state in control-plane tables.

**Provisioning a Postgres-family service.** Each step is idempotent and recorded in `provision_state`:

1. Pick the least-loaded active Postgres host, or create the service's container (container engine).
2. Generate a role name and random password; `CREATE ROLE ... LOGIN CONNECTION LIMIT n`.
3. `CREATE DATABASE ... OWNER role`, then `REVOKE ALL ON DATABASE ... FROM PUBLIC`.
4. Apply per-role settings.
5. Kind setup: extensions (`postgres`), collections table (`vector`), queue tables (`queue`), auth tables and a JWT secret (`auth`), run as the service role.
6. Set `endpoint` and store the encrypted credentials.
7. Set `status = 'active'` and write an audit row.

**Background jobs**

| Job | Trigger | Does |
| --- | --- | --- |
| `provision_resource`, `deprovision_resource` | API call | Create or tear down on a host |
| `delete_project` | API call | Deprovision every service, then delete the project |
| `rotate_credentials` | API call | New credentials, old ones revoked after the grace period |
| `sample_usage` | Every minute | `MeasureUsage` on active services, inserts samples |
| `enforce_limits` | After sampling | Warns, locks or unlocks writes |
| `revoke_expired_credentials` | Every minute | Drops rotated-out credentials after the grace period |
| `pause_inactive` | Hourly, only when enabled | Pauses idle services |
| `scale_to_zero` | Every minute | Stops idle containers (container engine) |
| `backup_resource` | Nightly, and on demand | Per-database dump into backup storage |
| `backup_tick` | Every minute | Runs due backup policies of `backup` services |
| `cron_tick` | Every 15 s | Runs due cron jobs |
| `monitor_tick` | Every 10 s | Runs due uptime checks, alerts on change |
| `send_email` | Any | Invites, resets, limit warnings (only when SMTP is configured) |

**Configuration** (environment variables only, validated at startup)

| Variable | Required | Notes |
| --- | --- | --- |
| `SPARKBASE_DOMAIN` | yes | Base domain, e.g. `example.com`; `localhost` in development |
| `PUBLIC_API_URL` | no | Defaults to `https://api.<domain>`; project URLs are built from it |
| `DATABASE_URL` | yes | Control-plane Postgres |
| `REDIS_URL` | yes | Control-plane Redis |
| `ENCRYPTION_KEY` | yes | 32 bytes, base64 |
| `TRAEFIK_CONFIG_TOKEN` | yes | Shared secret for the Traefik config endpoint |
| `PORT` | no | API port, default `8787` |
| `NATIVE_MODE` | no | `traefik` (default) or `direct` |
| `ENGINE` | no | `shared` (default for development) or `containers` (Docker install default) |
| `DOCKER_NETWORK` | with containers | Network the service containers join |
| `ACTIVATOR_HOST` | no | Hostname Traefik uses to reach the activator, default `api` |
| `PROXY_BASE_PORT` | no | First activator port, default `20000` |
| `NATIVE_PG_PORT`, `NATIVE_PG_POOLED_PORT`, `NATIVE_REDIS_PORT` | no | Public native ports printed in connection strings |
| `TLS_MODE`, `ACME_EMAIL` | no | Certificates (see Networking) |
| `BACKUP_DIR` | no | Where local backups are written |
| `BOOTSTRAP_PG_HOST`, `BOOTSTRAP_CACHE_HOST`, `BOOTSTRAP_STORAGE` | no | Bundled data hosts registered on first start |
| `SMTP_URL` | no | Without it, emails are logged and links are shown to the admin |

**Testing.** Unit tests for pure logic. Integration tests run the API and worker against a real control Postgres, a real Postgres data host and a real Redis, started locally by `scripts/dev-services.sh` (no Docker needed). Storage tests also run against a real Garage. Container tests (scale to zero, wake on connect) run when Docker is available. `scripts/e2e-compose.sh` brings up the full Docker install and runs the end-to-end suite through Traefik. Every driver has a contract test covering provision, isolation (tenant A cannot reach tenant B), limits, pause and resume, and deprovision. The isolation tests are the most important tests in the repository.

## API reference

Two layers: the **management API** at `/v1` creates and configures projects and services; the **project API** at `/p/{ref}` is what apps call.

**Conventions**

- JSON in and out. IDs are UUIDs.
- Management auth: session cookie or `Authorization: Bearer sb_...` account key.
- Project auth: `apikey` header with a project key, plus an optional end-user `Authorization: Bearer <jwt>`.
- Errors: `{ "error": { "code": "quota_exceeded", "message": "...", "details": {} } }`.
- Rate limit headers: `X-RateLimit-Limit`, `-Remaining`, `-Reset`.

**Setup, account and keys**

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/v1/setup` | Whether first-run setup is still open |
| POST | `/v1/setup` | Create the first account (`email`, `password`, `name`), which becomes the admin. Refused with `setup_closed` once any account exists |
| POST | `/v1/auth/login` | Email and password; starts a session. Locks for 15 minutes after 10 wrong passwords |
| POST | `/v1/auth/password/reset` | Email a reset link. Same response whether or not the email has an account |
| POST | `/v1/auth/password/reset/confirm` | `token`, `password`; signs out other sessions |
| GET | `/v1/auth/invites/{token}` | Look up an invite (email and role) |
| POST | `/v1/auth/invites/accept` | Accept an invite (`token`, `password`, `name`); creates the account and starts a session |
| POST | `/v1/auth/logout` | End the session |
| GET/PATCH | `/v1/me` | Current user; update name, or password with the current one |
| GET/POST | `/v1/api-keys` | List or create account keys (secret returned once) |
| DELETE | `/v1/api-keys/{id}` | Revoke |

**Admin** (role `admin`)

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/v1/admin/users` | List users and pending invites |
| POST | `/v1/admin/invites` | Invite by email with a role; returns the link |
| DELETE | `/v1/admin/invites/{id}` | Cancel an invite |
| PATCH | `/v1/admin/users/{id}` | Change role, disable or enable |
| POST | `/v1/admin/users/{id}/reset-link` | Create a password reset link to hand to a user |
| GET/PATCH | `/v1/admin/settings` | Auto-pause, resource cap, member plan changes, rotation grace period, `idleMinutes` (scale to zero), `allowPrivateTargets` |
| GET | `/v1/admin/resources` | Every service on the install |
| GET | `/v1/admin/logs?follow=1` | Server logs, live (Server-Sent Events) |
| GET/POST/PATCH | `/v1/admin/plans`, `/v1/admin/plans/{id}` | Manage plans |
| GET/POST/PATCH | `/v1/admin/hosts`, `/v1/admin/hosts/{id}` | Register, drain or take hosts offline |
| POST | `/v1/admin/resources/{id}/suspend`, `/unsuspend` | Suspend or lift |
| GET | `/v1/admin/audit` | Audit log |
| GET | `/v1/admin/jobs` | Recent and failed jobs |

**Projects and services**

| Method | Path | Purpose |
| --- | --- | --- |
| GET/POST | `/v1/projects` | List or create (`name`, optional `slug`) |
| GET/PATCH/DELETE | `/v1/projects/{id}` | Detail (with URL), rename, delete (async) |
| GET | `/v1/projects/{id}/keys` | Project URL and keys (publishable and secret) |
| POST | `/v1/projects/{id}/keys/rotate` | Rotate one key kind |
| GET | `/v1/plans?kind=` | Plans |
| GET/POST | `/v1/projects/{id}/resources` | List or add a service (`kind`, `name`, optional `plan`); add returns `202` |
| GET/PATCH/DELETE | `/v1/resources/{id}` | Detail, rename or change plan, delete |
| POST | `/v1/resources/{id}/pause`, `/resume`, `/retry` | Lifecycle |
| GET | `/v1/resources/{id}/connection` | Native connection details and the service's project API path (platform services: API path only) |
| GET | `/v1/resources/{id}/logs?follow=1&source=api\|container` | Recent log lines, then live (Server-Sent Events) |
| GET | `/v1/resources/{id}/metrics?minutes=60` | Per-minute requests, errors, latency, memory, CPU, storage |
| POST | `/v1/resources/{id}/credentials/rotate` | New credentials with a grace period |
| GET | `/v1/resources/{id}/usage` | Latest usage against limits, and samples |
| GET/POST | `/v1/resources/{id}/backups` | List or take backups (`postgres`-family) |
| GET | `/v1/notifications` | Limit warnings and pauses for the current user |

**Project API** (base: the project URL)

| Method | Path | Key | Purpose |
| --- | --- | --- | --- |
| GET | `/` | any | Project info and list of services |
| POST | `/postgres/{name}/query` | secret | `{ "sql", "params" }`, one statement |
| POST | `/postgres/{name}/transaction` | secret | `{ "statements": [{sql, params}] }`, atomic |
| GET | `/postgres/{name}/schema` | secret | Tables, columns |
| GET | `/postgres/{name}/extensions` | secret | Installed and allowed extensions; install one with `create extension` |
| GET/PUT/DELETE | `/cache/{name}/keys/{key}` | secret | Read; write (`{ value, ttl }`); delete |
| POST | `/cache/{name}/batch` | secret | Several get, set, delete operations |
| POST | `/cache/{name}/command` | secret | `{ args }`: one Redis command as the service's ACL user (console) |
| GET | `/storage/{name}/objects?prefix=` | secret | List objects |
| PUT/GET/DELETE | `/storage/{name}/objects/{key...}` | secret | Upload (body), download, delete |
| POST | `/storage/{name}/sign` | secret | `{ key, method, expiresIn }` → signed URL |
| GET/PUT | `/storage/{name}/signed/{key...}?…` | none | Use a signed URL (local backend) |
| GET/POST | `/vector/{name}/collections` | secret | List, create (`name`, `dimension`, `metric`) |
| DELETE | `/vector/{name}/collections/{c}` | secret | Drop a collection |
| POST | `/vector/{name}/collections/{c}/upsert` | secret | `{ items: [{id, vector, metadata}] }` |
| POST | `/vector/{name}/collections/{c}/query` | secret | `{ vector, topK, filter }` → nearest items |
| POST | `/vector/{name}/collections/{c}/delete` | secret | `{ ids }` |
| GET/POST | `/queue/{name}/queues` | secret | List, create (`name`, `visibilityTimeout`, `maxReceives`) |
| DELETE | `/queue/{name}/queues/{q}` | secret | Drop a queue |
| POST | `/queue/{name}/queues/{q}/send` | secret | `{ messages: [{ body, delay }] }` |
| POST | `/queue/{name}/queues/{q}/receive` | secret | `{ max, visibilityTimeout }` |
| POST | `/queue/{name}/queues/{q}/ack`, `/nack` | secret | `{ ids }` (nack: optional `delay`) |
| GET | `/queue/{name}/queues/{q}/stats` | secret | Ready, in flight, dead counts |
| POST | `/auth/signup` | publishable | `{ email, password, data }` → session |
| POST | `/auth/token?grant_type=password` | publishable | `{ email, password }` → session |
| POST | `/auth/recover` | publishable | `{ email, redirectTo }`: email a recovery link and code |
| POST | `/auth/otp` | publishable | `{ email, redirectTo, data }`: email a sign-in link and a 6-digit code |
| POST | `/auth/verify` | publishable | `{ token }` or `{ email, code }` → session (creates the user on first sign-in) |
| POST | `/auth/token?grant_type=refresh_token` | publishable | Refresh → session (the refresh token rotates) |
| POST | `/auth/logout` | publishable + user | Revoke the refresh token family |
| GET/PUT | `/auth/user` | publishable + user | Current user; update `data` or `password` |
| POST | `/auth/admin/generate-link` | secret | `{ email, redirectTo }` → link and code, for apps that send their own email |
| GET/PATCH | `/auth/admin/settings` | secret | Allow sign-ups, site URL, redirect allowlist, token lifetimes |
| GET/POST | `/auth/admin/users` | secret | List, create users |
| DELETE | `/auth/admin/users/{id}` | secret | Delete a user |
| POST | `/realtime/{name}/broadcast` | secret | `{ channel, event, payload }` |
| GET | `/realtime/{name}/subscribe?channel=…` | publishable (`public:` channels) or secret | Server-Sent Events stream; key may be passed as `?apikey=` |
| GET/POST | `/realtime/{name}/watches` | secret | List or add a table watch (`{ postgres, table }`) |
| DELETE | `/realtime/{name}/watches/{postgres}/{table}` | secret | Remove a watch and its trigger |
| GET/POST | `/cron/{name}/jobs` | secret | List or create (`name`, `schedule`, `action`) |
| PATCH/DELETE | `/cron/{name}/jobs/{id}` | secret | Change schedule, action or enabled; delete |
| POST | `/cron/{name}/jobs/{id}/run` | secret | Run now |
| GET | `/cron/{name}/jobs/{id}/runs` | secret | Recent runs |
| GET/POST | `/monitor/{name}/checks` | secret | List or create (`name`, `url`, `method`, `intervalSeconds`, `timeoutSeconds`, `expectStatus`) |
| PATCH/DELETE | `/monitor/{name}/checks/{id}` | secret | Change or delete |
| POST | `/monitor/{name}/checks/{id}/run` | secret | Check now |
| GET | `/monitor/{name}/checks/{id}/results` | secret | Recent results |
| PATCH | `/monitor/{name}/settings` | secret | `alertWebhook` |
| GET/PATCH | `/backup/{name}/settings` | secret | Databases, schedule, retention, destination |
| POST | `/backup/{name}/run` | secret | Run the policy now |
| GET | `/backup/{name}/runs` | secret | Backups taken by this service |

A session is `{ access_token, token_type: "bearer", expires_in, refresh_token, user }`.

**Operational**

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/health` | Liveness |
| GET | `/v1/status` | Host health and queue depth (admins see detail) |
| GET | `/internal/traefik/config` | Traefik dynamic config; token protected |
| GET | `/metrics` | Prometheus metrics; token protected |

**Error codes**

| Code | HTTP | Meaning |
| --- | --- | --- |
| `unauthorized` | 401 | Missing or invalid key, session or token |
| `forbidden` | 403 | Key kind, scope or role does not cover this action |
| `not_found` | 404 | No such project, service or object |
| `resource_limit_reached` | 403 | The per-user service cap is reached |
| `quota_exceeded` | 429 | A plan limit is hit |
| `rate_limited` | 429 | Too many requests |
| `resource_not_ready` | 409 | Service is provisioning, paused, suspended or failed |
| `plan_change_blocked` | 409 | Usage exceeds the target plan |
| `conflict` | 409 | Name taken, or one auth service per project |
| `setup_closed` | 409 | First-run setup was already completed |
| `validation_failed` | 422 | Bad request body |
| `query_failed` | 400 | The database rejected the SQL |

## SDK, CLI and connection strings

```ts
import { createClient } from '@sparkbase/sdk'

const sb = createClient('https://api.example.com/p/a1b2c3', process.env.SPARKBASE_SECRET_KEY)

const { rows } = await sb.postgres('main').query('select * from todos where id = $1', [id])
await sb.cache('sessions').set('user:42', { name: 'Ada' }, { ttl: 60 })
await sb.storage('uploads').upload('avatars/42.png', file)
const url = await sb.storage('uploads').signedUrl('avatars/42.png', { expiresIn: 600 })
await sb.vector('docs').collection('pages').upsert([{ id: 'p1', vector: emb, metadata: { lang: 'en' } }])
const hits = await sb.vector('docs').collection('pages').query(emb, { topK: 5 })
await sb.queue('jobs').queue('emails').send({ to: 'ada@example.com' })
const msgs = await sb.queue('jobs').queue('emails').receive({ max: 10 })
await sb.realtime('live').broadcast('public:room1', { text: 'hi' })
const stop = sb.realtime('live').subscribe(['public:room1'], (m) => console.log(m.payload))
await sb.realtime('live').watchTable('main', 'todos')   // changes arrive on db:main:todos
await sb.cron('tasks').create({ name: 'cleanup', schedule: '0 3 * * *', action: { type: 'sql', postgres: 'main', sql: 'delete from todos where done' } })
await sb.monitor('uptime').create({ name: 'site', url: 'https://myapp.com/health' })
await sb.backup('nightly').run()

// In a browser, with the publishable key
const client = createClient(projectUrl, 'sbp_...')
await client.auth.signUp({ email, password })
const { session } = await client.auth.signInWithPassword({ email, password })
const { user } = await client.auth.getUser()
```

**SDK rules.** Zero dependencies, `fetch` only, one method per endpoint, typed errors carrying the API `code`, and the auth client keeps the session in memory (or a storage adapter in browsers) and refreshes it.

**CLI**

| Command | Does |
| --- | --- |
| `sparkbase login --url URL` | Store an account key for an install |
| `sparkbase projects list` / `create NAME` | Manage projects |
| `sparkbase link PROJECT_REF` | Link the current folder to a project |
| `sparkbase services add KIND NAME [--plan]` | Add a service |
| `sparkbase services list` | Show services and status |
| `sparkbase env pull` | Write the project URL, keys and native connection strings into `.env` without touching other lines |
| `sparkbase usage` | Usage against limits |
| `sparkbase admin invite EMAIL` / `admin users` / `admin hosts` | Admin commands |

**Connection strings**

| Service | Format |
| --- | --- |
| Postgres (pooled) | `postgres://ROLE:PASSWORD@main-a1b2c3.db.example.com:6432/DB?sslmode=require` |
| Postgres (direct) | `postgres://ROLE:PASSWORD@main-a1b2c3.db.example.com:5432/DB?sslmode=require` |
| Vector | Same as Postgres, with pgvector tables |
| Cache | `rediss://USER:PASSWORD@sessions-a1b2c3.cache.example.com:6380` (keys must start with the prefix shown) |
| Storage (Garage) | Endpoint `https://s3.example.com`, bucket, access key, secret, region `garage`, path-style |
| Queue, Auth | Project API only; auth also shows its JWT secret |
| Realtime, Cron, Backups, Monitor | Project API only |

Pooled connections run in transaction mode, so session features need the direct port.

## Plans and limits

Plans are named limits presets the admin manages. No prices. Three plans per kind are seeded, Small is the default. Admins set a per-user service cap (default 10, unlimited for admins).

| Kind | Limit | Small | Medium | Large |
| --- | --- | --- | --- | --- |
| postgres | Storage | 1 GB | 10 GB | 50 GB |
| postgres | Direct connections | 5 | 15 | 40 |
| postgres | Statement timeout | 30 s | 60 s | 300 s |
| cache | Memory | 32 MB | 256 MB | 1 GB |
| cache | Connections | 10 | 50 | 200 |
| storage | Storage | 2 GB | 25 GB | 100 GB |
| storage | Max upload through the API | 50 MB | 200 MB | 1 GB |
| vector | Storage | 1 GB | 10 GB | 50 GB |
| vector | Max dimension | 2048 | 4096 | 8192 |
| queue | Storage | 512 MB | 5 GB | 20 GB |
| queue | Max message size | 256 KB | 1 MB | 4 MB |
| auth | Users | 10k | 100k | Unlimited |
| every kind | Project API requests per month | 100k | 1M | Unlimited |
| postgres-family | Backups kept | 3 days | 7 days | 14 days |
| postgres-family | Container memory | 256 MB | 1 GB | 4 GB |
| realtime | Subscribers / messages a month | 200 / 1M | 2,000 / 10M | 20,000 / unlimited |
| cron | Jobs / minimum interval | 10 / 60 s | 100 / 60 s | 1,000 / 60 s |
| backup | Retention / databases | 7 days / 5 | 30 days / 25 | 90 days / 100 |
| monitor | Checks / minimum interval | 5 / 60 s | 50 / 30 s | 500 / 10 s |

A cache container runs Redis with `maxmemory` set to the plan's memory and a container limit 48 MB above it.

## Self-hosting

Self-hosting is the product. Development needs only Go, Bun, Postgres and Redis installed locally (`scripts/dev-services.sh` starts private instances). Servers use the Docker install below; see `docs/self-hosting.md`.

**Docker install.** `deploy/docker-compose.yml` with Traefik, the server, the dashboard, control Postgres and Redis, and Garage. Docker is required: the server mounts the Docker socket and runs each database and cache as its own container (`ENGINE=containers`). The shared Postgres host (with PgBouncer) and shared Redis host are the `shared-hosts` compose profile. Only Traefik publishes ports.

**Docs.** The `docs/` folder is the documentation. The dashboard renders it at `/docs` (sidebar, search, table of contents), and the dashboard image copies it in at build time.

**Bring your own Traefik.** A compose profile without the bundled Traefik, with labels and the HTTP provider endpoint documented.

**Multiple hosts.** `POST /v1/admin/hosts` or `sparkbase admin hosts add` registers another data host.

**Upgrades.** Semantic versions, forward-only migrations run at startup.

**Backing up the install.** Back up the control database and `ENCRYPTION_KEY` together.

## Security, backups and operations

**Security baseline**

- TLS everywhere in production; HTTPS only for the API and dashboard.
- Credentials encrypted at rest with AES-256-GCM. Passwords are argon2id. Account keys, project key hashes, session tokens and one-time tokens are SHA-256 hashed.
- Tenant roles are never superusers and cannot create roles or databases.
- The project API runs as the service's own credentials.
- Credential rotation per service with a grace period.
- Audit log for every create, delete, rotate, plan change, pause, suspend, invite, role change and limit event.
- Rate limits on login, signup, resets, setup, end-user auth and every key. Accounts lock for 15 minutes after 10 wrong passwords; one-time codes lock after 5 wrong tries.
- Setup is open only until the first account exists; the server prints a reminder to finish it right after the first start.

**Backups.** Nightly logical dump (`pg_dump`) per Postgres-family service into backup storage, kept by plan, with restore into the same or a new service. The control database is dumped the same way. Weekly automated restore test.

**Observability.** Live logs and metrics per service, server logs for admins, terminals for databases and caches (see Logs, metrics and terminals), Prometheus metrics, JSON logs on stdout, `GET /v1/status`, and an admin health page in the dashboard.

**Runbooks.** Add a host, drain a host, restore a database, restore the install, rotate the encryption key, respond to a suspected cross-tenant incident.

## Roadmap

| Stage | Goal | Check |
| --- | --- | --- |
| 0. Foundations | Control plane, setup, accounts, invites, keys, projects with URL and keys, jobs, hosts | Integration tests green |
| 1. Database | `postgres` service, project API SQL, native string, limits, isolation tests | Isolation tests green |
| 2. Services | `cache`, `storage`, `vector`, `queue`, `auth`, SDK, CLI | Isolation and SDK tests green for every kind |
| 3. Operations | Backups, usage and limits, audit, admin tools, status | Restore works |
| 4. Release | Docker install with Traefik, docs, release pipeline | Clean server to working service by following the docs |
| 5. Containers and platform | Container engine with scale to zero, live logs, metrics, terminals, realtime, cron, backups, monitor, docs site | Compose e2e covers idle stop and wake through Traefik |
| 6. Later | Row-level access for end users, MongoDB, more SDKs, WebSocket transport for realtime | Each passes the isolation suite |

The dashboard is built alongside every stage.

## Licensing

| Part | License |
| --- | --- |
| Server, drivers, dashboard | AGPL-3.0 |
| SDK and CLI | MIT |

Contribution policy: DCO.

## Risks and open questions

| Risk | Mitigation |
| --- | --- |
| Cross-tenant data access | Per-service roles and credentials, `REVOKE ... FROM PUBLIC` on every database, project API runs as the service role, isolation tests in CI |
| SQL over HTTP abuse | Runs as the tenant role with timeouts and row and size caps |
| Install too hard | Local dev without Docker; Docker install with an end-to-end test |
| Noisy neighbors | Connection and timeout limits, host monitoring |
| Shared Redis eviction | Solved by the container engine: one Redis per cache service |
| Cold start latency | Activator wakes containers in well under a second; the idle timeout is an admin setting |
| Docker socket access | Only the server talks to Docker; service containers get no socket and publish no ports |
| Single server fails | Off-site backups, restore runbook |

**Open questions**

- [x] Redis: a process per service (container engine); shared instance with ACL prefixes remains for `ENGINE=shared`.
- [ ] Row-level access for end users: Postgres RLS with JWT claims, Supabase style?
- [ ] External S3 backends (R2, AWS S3) with a prefix-per-service model?
- [ ] How does Sparkbase relate to SparkDB?
