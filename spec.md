# Sparkbase: Product & Technical Spec

Oct 6, 2026 · @Excel

## Overview

Sparkbase is an open-source, self-hosted data platform. You run it on your own server, create a project, attach only the data services your backend needs, and connect to each one through a native connection string, an SDK, or an HTTP API. Nothing is bundled, and nothing is sold: there is no paid tier, no hosted edition and no feature gating.

**Pitch:** the data layer for your backend, on your own server, with exactly the services you need.

**Who it is for**

- Solo developers and indie hackers who want managed-feeling Postgres, Redis and object storage on a VPS they already pay for.
- Small teams, agencies, classrooms and clubs where one person runs the server and invites the others.
- Developers who want the convenience of a data platform without handing their data to a third party.

**Positioning.** Bundled platforms such as Supabase and Firebase are built around their own auth and client libraries. Running Postgres, Redis and an S3 server by hand on a VPS means doing pooling, TLS, credentials, backups and limits yourself for every project. Sparkbase is one `docker compose up` that gives every project its own isolated databases, caches and storage folders, with TLS, credentials, limits and backups already done.

**Product principles**

1. **Open source, all of it.** One codebase, one edition. Self-hosting is the product, not a side door.
2. **Native first.** Every resource speaks its standard protocol, so existing drivers, ORMs and tools keep working. The SDK and API are conveniences, never a wall.
3. **One command to run.** A clean server with Docker and a domain gets to a working Postgres resource in minutes, with HTTPS.
4. **Shared hosts now, more hosts later.** Stable per-resource hostnames make moving a resource between hosts invisible to its users.
5. **Limits protect the server.** The operator sets limits per resource so one project cannot take down the others.
6. **Boring technology.** Node, Hono, Postgres, Redis, Traefik, and plain SQL with no ORM.

## Product model

The product is five nouns: an **operator** runs an install, **users** own **projects**, a project holds **resources**, and each resource sits on a **plan** that sets its limits.

| Concept | What it is | Notes |
| --- | --- | --- |
| Install | One Sparkbase deployment on a server (or a few) | Run by one operator |
| User | A person with a login and API keys | Role `admin` or `member`. Admins manage users, hosts and plans |
| Project | A named container | Has a short slug used in hostnames, e.g. `a1b2c3` |
| Resource | One data service inside a project | Has a kind, a plan, a host, a status and credentials |
| Plan | A named limits preset for one resource kind | Defined by the admin. No prices. Seeded with Small, Medium and Large |
| Host | A server or bucket that holds many resources of one kind | Invisible to members; chosen by the scheduler |

**Who can sign up.** Signup is invite-only by default. An admin invites people by email (or copies the invite link when no SMTP server is configured). An admin can switch the install to open signup in settings. The first admin is created during first-run setup.

**Resource catalog**

| Resource | Engine | Native protocol | Phase |
| --- | --- | --- | --- |
| Relational database | PostgreSQL | Postgres wire over TLS | First release |
| Cache | Redis (Valkey-compatible) | RESP over TLS | First release |
| Object storage | Garage (S3-compatible), bundled | S3 API | First release |
| Vector database | pgvector on the Postgres hosts | Postgres wire + SDK | Phase 2 |
| NoSQL database | MongoDB | Mongo wire | Phase 2 |
| Relational (other engines) | MySQL, MariaDB | MySQL wire | Phase 2 |
| Auth, realtime, queues | Built in | HTTP, WebSocket + SDK | Phase 3 |

**The core user flow**

1. The operator runs `docker compose up -d`, completes first-run setup and invites people.
2. A user signs in and creates a project.
3. They add a resource: pick a kind, a name and a plan.
4. They wait a few seconds while it provisions, then copy the connection string or run `sparkbase env pull`.
5. They connect with any native driver, or use the SDK and HTTP API.

**Resource lifecycle**

| Status | Meaning | Reached by |
| --- | --- | --- |
| provisioning | Worker is creating it on a host | Create call |
| active | Healthy and usable | Provisioning succeeded, or resume |
| paused | Connections refused, data kept | Owner or admin action |
| suspended | Blocked by an admin, data kept | Admin action |
| failed | Provisioning did not finish | Retries exhausted; owner can retry or delete |
| deleting | Teardown in progress | Delete call |

## Architecture

Sparkbase splits into a control plane that decides things and a data plane that serves them. A user's application talks to the data plane directly over native protocols, through Traefik, so the control plane can be down for a while without breaking a running app.

```text
                       Internet
                           │
                 ┌─────────▼─────────┐
                 │      Traefik      │  :443 HTTPS + TLS for Postgres and Redis (SNI)
                 │  (ACME certs)     │  :80  redirect + ACME HTTP-01
                 └──┬─────┬──────┬───┘
       app.example.com    │      │ *.db / *.cache / s3.example.com
                    │     │      │
        ┌───────────▼┐ ┌──▼────┐ │      ┌──────────────────────────────┐
        │ dashboard  │ │  api  │ └─────►│ data plane                   │
        └────────────┘ └──┬────┘        │ PgBouncer → Postgres host(s) │
                          │ jobs        │ Redis host(s)                │
                     ┌────▼───┐         │ Garage (S3)                  │
                     │ worker ├────────►│                              │
                     └────┬───┘ admin   └──────────────────────────────┘
                          │
            control-plane Postgres + Redis
```

The dashboard, CLI and SDK call the API. The API writes metadata and queues jobs, and the worker claims them and acts on the data hosts. Applications skip all of that and connect straight to their resource hostname, which Traefik routes to the right host.

**Components**

| Component | Role | Technology |
| --- | --- | --- |
| Traefik | Edge proxy: HTTPS for the API, dashboard and S3 endpoint; TLS termination and SNI routing for Postgres and Redis; ACME certificates | Traefik v3 |
| API | Auth, projects, resources, plans, admin, the data API, Traefik dynamic config | Node, Hono |
| Worker | Provisioning, usage sampling, limit enforcement, backups, host moves, email | Node, same codebase |
| Control DB | Users, projects, resources, plans, hosts, jobs, audit log | Postgres |
| Redis | Rate limits, short-lived counters, sessions cache | Redis |
| Postgres hosts | Tenant databases behind a pooler | Postgres, PgBouncer |
| Redis hosts | Tenant cache namespaces | Redis or Valkey with ACLs |
| Object storage | One bucket and one access key per resource | Garage |
| Dashboard | Web UI over the same public API | Next.js |

**Architecture rules**

- **The control plane is not on the data path.** Native connections go through Traefik to the data hosts and never through the API. Only the optional HTTP data API passes through the API.
- **Hosts are interchangeable.** A host is a row with a weight and a status. Adding capacity is registering another host; retiring one is draining it.
- **One stable hostname per resource.** Users never see which host holds their data, which is what makes moves possible.
- **Secrets live in one place.** Credentials are stored encrypted in the control DB and are decrypted only by the worker and the API when needed.
- **Every long operation is a job.** The API answers fast with a status, the worker does the slow part, and retries are safe because each step is idempotent.
- **No vendor assumptions.** Configuration is environment variables. TLS comes from Traefik's ACME resolver or certificates the operator provides. Nothing is hardcoded to a cloud provider.

## Networking, TLS and Traefik

Traefik is the only container that publishes ports. Everything else lives on internal Docker networks.

**Hostnames.** The operator sets one base domain, `SPARKBASE_DOMAIN`, for example `example.com`. Sparkbase uses:

| Hostname | Routed to | Protocol |
| --- | --- | --- |
| `app.<domain>` | Dashboard | HTTPS |
| `api.<domain>` | API | HTTPS |
| `s3.<domain>` | Garage S3 API | HTTPS |
| `<slug>.db.<domain>` | The resource's Postgres host (via PgBouncer) | Postgres wire, TLS |
| `<slug>.cache.<domain>` | The resource's Redis host | RESP, TLS |

The operator creates DNS records once: `app`, `api` and `s3` pointing at the server, plus wildcard records `*.db` and `*.cache`. Sparkbase never needs DNS API access for routing. Moving a resource to another host changes Traefik's routing, not DNS.

**Ports.** Traefik publishes `80` (redirect and ACME HTTP-01), `443` (HTTPS), `5432` (Postgres direct), `6432` (Postgres pooled) and `6380` (Redis TLS). Each data port is a Traefik TCP entrypoint.

**TLS for Postgres and Redis.** Traefik terminates TLS and routes by SNI. Postgres clients negotiate TLS with STARTTLS, which Traefik v3 supports on TCP routers, and libpq sends SNI by default (`sslsni=1`). Clients connect with `sslmode=require` (or `verify-full`, since the certificate is valid for the hostname). Redis clients connect with `rediss://`. Traffic from Traefik to the data hosts stays on the internal network.

**Certificates.** Two supported modes, chosen with `TLS_MODE`:

1. `acme-dns` (recommended): one wildcard certificate per data domain (`*.db.<domain>`, `*.cache.<domain>`) through the ACME DNS-01 challenge, using any DNS provider Traefik supports. New resources work instantly with no new certificate.
2. `acme-http`: one certificate per hostname through HTTP-01. Works without DNS API credentials, but each new resource waits for its certificate, and Let's Encrypt rate limits apply to installs with many resources.

A third option, `custom`, loads certificate files the operator mounts.

**Dynamic routing.** Traefik reads its routers from the API through the HTTP provider (`GET /internal/traefik/config`, served on the internal network only and protected by a shared token). The endpoint builds one TCP router per active resource (`HostSNI(\`a1b2c3.db.example.com\`)` to that resource's host) from the `resources` and `hosts` tables. Traefik polls it every few seconds. Paused, suspended and deleted resources get no router, which closes their port at the edge in addition to the credential-level block. With a single data host, a catch-all `HostSNIRegexp` router is a valid shortcut, but the per-resource routers are the default because they make host moves and edge blocking work.

## Tenancy and isolation

An install runs shared hosts: each engine lives on one server to start (the same server as everything else in the single-host setup), and every resource gets an isolated slice of it. Users on the same install must not be able to reach each other's data, even though they share hosts. Treat that as the core promise of the project.

**Per-engine isolation**

| Engine | Unit of isolation | Access control | Main risk |
| --- | --- | --- | --- |
| Postgres | One database plus one role per resource | `REVOKE CONNECT ON DATABASE ... FROM PUBLIC`, non-superuser roles, `scram-sha-256`, TLS required | Noisy neighbors on CPU and IO; no native CPU quota |
| Redis | One ACL user per resource, restricted to a key prefix | `~prefix:*` key pattern; `KEYS`, `FLUSHALL`, `FLUSHDB`, `CONFIG`, `DEBUG`, `SCRIPT`, `EVAL`, `EVALSHA`, `FUNCTION` and `MODULE` denied | Memory and CPU are global; eviction can hit other tenants |
| Object storage | One Garage bucket plus one access key per resource | The key is allowed on its own bucket only; no tenant key is ever allowed on another bucket or the backups bucket | Disk and IO are shared |

**Postgres specifics**

- Put PgBouncer in front in transaction mode for the pooled port. Offer a direct, low-limit port for migrations and anything needing session features.
- PgBouncer authenticates tenants through `auth_query` against a function on the host, so new roles work without editing config files.
- Set per-role `CONNECTION LIMIT`, `statement_timeout`, `idle_in_transaction_session_timeout` and `work_mem`.
- Allowlist extensions (`pgcrypto`, `uuid-ossp`, `pg_trgm`, `vector`). Do not allow `dblink`, `file_fdw`, `postgres_fdw` or untrusted procedural languages.
- Never grant `pg_read_server_files`, `pg_write_server_files` or `pg_execute_server_program`.

**Redis specifics.** Sharing one Redis process gives weak isolation for memory: one tenant filling memory triggers eviction for everyone, and per-prefix memory has to be estimated by sampling. The default is shared with ACL plus prefix. A small Redis or Valkey process per resource is the main alternative and is listed in the open questions. Keep the driver swappable so only it changes if that decision flips.

**Object storage specifics.** The bundled engine is Garage, a lightweight S3-compatible store. Garage buckets and keys are cheap, and Garage permissions are granted per key per bucket, so each resource gets its own bucket (`sb-<resource_id>`, built from the immutable resource id) and its own access key allowed on that bucket only. The worker manages both through Garage's admin API. Backups live in a separate private bucket that no tenant key is ever allowed on.

Tenants reach their bucket in two ways:

1. **Native S3** with their own long-lived access key, through `s3.<domain>` with path-style addressing, from any S3 client or tool (`aws s3`, `rclone`, the AWS SDKs). Rotation creates a new key, allows it on the bucket, and removes the old one after the grace period.
2. **The data API and presigned URLs**, signed by the API with the resource's own key, so serverless and browser uploads work without exposing the key.

Garage enforces the storage limit itself through bucket quotas (maximum size and object count), so writes past the limit are refused by the engine, not by Sparkbase. Pausing or suspending a resource removes its key's permission on the bucket; resuming grants it again.

Garage has no prefix-scoped keys or temporary credentials, which is why the isolation unit is a bucket. Support for external S3 services (R2, AWS S3, Backblaze B2), which would need a shared-bucket, prefix-per-resource model with temporary credentials, is deferred until after the first release.

**Limit enforcement.** Each limit has a mechanism, a check frequency and a defined action. Limits protect the host, not a revenue model.

| Limit | Mechanism | Checked | Action on breach |
| --- | --- | --- | --- |
| Postgres storage | `pg_database_size()` sampled by a worker job | Every 5 min | Warn at 80% and 90%; at 100% set `default_transaction_read_only = on` for the role |
| Postgres direct connections | Role `CONNECTION LIMIT` | Hard | New connections refused |
| Postgres pooled connections | PgBouncer per-database `pool_size` and `max_db_connections` | Hard | Clients wait, then time out |
| Query time | Role `statement_timeout` | Hard | Query cancelled |
| Redis memory | Sampled key scan with `MEMORY USAGE` | Every 1 min | Switch ACL user to read and delete only until under limit |
| Redis clients | `CLIENT LIST` by user | Every 1 min | Kill excess clients |
| Object storage bytes and objects | Garage bucket quota, plus bucket size sampled through the admin API for usage graphs | Hard (sampled every 5 min) | Writes refused by Garage |
| Data API requests | Counters in Redis per resource | Rolling month | Deny with `quota_exceeded` |

Every breach writes an `audit_log` row, notifies the owner (email when SMTP is configured, always a dashboard banner), and clears automatically once usage drops or the admin moves the resource to a larger plan.

**Pausing.** Owners and admins can pause a resource: the Postgres role is set to `NOLOGIN` and its connections terminated, the Redis ACL user is disabled, the storage key loses its permission on the bucket, and the Traefik router is removed. Resuming takes seconds. Automatic pausing of idle resources is an optional admin setting, off by default.

## Control plane data model

The control plane keeps its own metadata in a dedicated Postgres database, separate from every tenant host. The schema below is the first migration (`0001_init.sql`). Credentials are stored encrypted, usage samples are append-only, and background work runs from a `jobs` table using `FOR UPDATE SKIP LOCKED`, so self-hosters need no extra queue software.

```sql
create extension if not exists pgcrypto;

create table users (
  id                uuid primary key default gen_random_uuid(),
  email             text not null unique,
  password_hash     text not null,             -- argon2id
  name              text,
  role              text not null default 'member'
                    check (role in ('admin','member')),
  email_verified_at timestamptz,
  failed_logins     int not null default 0,
  locked_until      timestamptz,
  disabled_at       timestamptz,
  created_at        timestamptz not null default now()
);

create table sessions (
  id           uuid primary key default gen_random_uuid(),
  user_id      uuid not null references users(id) on delete cascade,
  token_hash   text not null unique,           -- sha256 of the cookie value
  expires_at   timestamptz not null,
  created_at   timestamptz not null default now()
);

create table auth_tokens (                     -- email verification, password reset, invites
  id          uuid primary key default gen_random_uuid(),
  kind        text not null check (kind in ('verify_email','password_reset','invite')),
  user_id     uuid references users(id) on delete cascade,  -- null for invites
  email       text not null,
  token_hash  text not null unique,
  role        text check (role in ('admin','member')),     -- invites only
  invited_by  uuid references users(id) on delete set null,
  expires_at  timestamptz not null,
  used_at     timestamptz,
  created_at  timestamptz not null default now()
);

create table settings (                        -- install-wide, edited by admins
  key        text primary key,                 -- 'signup_mode', 'auto_pause_days', ...
  value      jsonb not null,
  updated_at timestamptz not null default now()
);

create table projects (
  id         uuid primary key default gen_random_uuid(),
  owner_id   uuid not null references users(id) on delete cascade,
  slug       text not null unique,             -- short id used in hostnames
  name       text not null,
  status     text not null default 'active'
             check (status in ('active','suspended','deleting')),
  created_at timestamptz not null default now()
);

create table hosts (
  id              uuid primary key default gen_random_uuid(),
  kind            text not null check (kind in ('postgres','cache','storage')),
  name            text not null unique,
  internal_addr   text not null,               -- admin target; for storage, the S3 endpoint
  config          jsonb not null default '{}', -- per-kind settings, e.g. bucket names, pooler addr
  admin_secret_enc bytea not null,             -- AES-256-GCM
  weight          int  not null default 100,   -- relative capacity
  status          text not null default 'active'
                  check (status in ('active','draining','offline')),
  created_at      timestamptz not null default now()
);

create table plans (
  id          text primary key,                -- e.g. 'postgres-small'
  kind        text not null check (kind in ('postgres','cache','storage')),
  name        text not null,
  limits      jsonb not null,                  -- {"storage_mb":5120,"direct_connections":10,...}
  is_default  boolean not null default false,
  active      boolean not null default true
);
create unique index plans_one_default_per_kind on plans (kind) where is_default;

create table resources (
  id              uuid primary key default gen_random_uuid(),
  project_id      uuid not null references projects(id) on delete cascade,
  kind            text not null check (kind in ('postgres','cache','storage')),
  name            text not null,
  plan_id         text not null references plans(id),
  host_id         uuid references hosts(id),
  status          text not null default 'provisioning'
                  check (status in ('provisioning','active','paused',
                                    'suspended','failed','deleting')),
  external_name   text,                        -- db name, key prefix or bucket name
  endpoint        text,                        -- stable hostname, e.g. a1b2c3.db.example.com
  provision_state jsonb not null default '{}', -- completed steps, so retries resume
  write_locked    boolean not null default false,
  last_active_at  timestamptz,
  created_at      timestamptz not null default now(),
  unique (project_id, name)
);
create index resources_host_idx on resources (host_id);

create table resource_credentials (
  id          uuid primary key default gen_random_uuid(),
  resource_id uuid not null references resources(id) on delete cascade,
  username    text not null,
  secret_enc  bytea not null,                  -- AES-256-GCM, key from env
  created_at  timestamptz not null default now(),
  revoke_at   timestamptz,                     -- end of the rotation grace period
  revoked_at  timestamptz
);

create table api_keys (
  id           uuid primary key default gen_random_uuid(),
  user_id      uuid not null references users(id) on delete cascade,
  project_id   uuid references projects(id) on delete cascade, -- null = account-wide
  name         text not null,
  prefix       text not null,                  -- first 8 chars, shown in UI
  key_hash     text not null unique,           -- sha256 of the full key
  scopes       text[] not null default '{}',
  last_used_at timestamptz,
  expires_at   timestamptz,
  revoked_at   timestamptz,
  created_at   timestamptz not null default now()
);

create table idempotency_keys (
  user_id     uuid not null references users(id) on delete cascade,
  key         text not null,
  request_hash text not null,
  status_code int,
  response    jsonb,
  created_at  timestamptz not null default now(),
  primary key (user_id, key)
);

create table usage_samples (
  resource_id uuid not null references resources(id) on delete cascade,
  metric      text not null,                   -- 'storage_bytes','connections',...
  sampled_at  timestamptz not null,
  value       bigint not null,
  primary key (resource_id, metric, sampled_at)
);

create table backups (
  id          uuid primary key default gen_random_uuid(),
  resource_id uuid not null references resources(id) on delete cascade,
  status      text not null default 'running'
              check (status in ('running','done','failed')),
  object_key  text,                            -- in the private backups bucket
  size_bytes  bigint,
  checksum    text,
  expires_at  timestamptz,
  error       text,
  created_at  timestamptz not null default now(),
  finished_at timestamptz
);

create table jobs (
  id           bigserial primary key,
  type         text not null,                  -- 'provision_resource', ...
  payload      jsonb not null,
  status       text not null default 'queued'
               check (status in ('queued','running','done','failed')),
  run_at       timestamptz not null default now(),
  attempts     int not null default 0,
  max_attempts int not null default 5,
  locked_at    timestamptz,
  last_error   text,
  created_at   timestamptz not null default now()
);
create index jobs_ready_idx on jobs (run_at) where status = 'queued';

create table audit_log (
  id          bigserial primary key,
  actor_id    uuid,
  project_id  uuid,
  resource_id uuid,
  action      text not null,                   -- 'resource.create', 'limit.breach', ...
  detail      jsonb,
  created_at  timestamptz not null default now()
);
```

**Notes**

- A worker claims a job with `update jobs set status='running', locked_at=now(), attempts=attempts+1 where id = (select id from jobs where status='queued' and run_at <= now() order by run_at for update skip locked limit 1) returning *`. Jobs stuck in `running` past a timeout are re-queued.
- Host choice is a query over `resources` grouped by `host_id`, weighted by `hosts.weight`, filtered to `status = 'active'`.
- For storage, a `hosts` row represents a Garage cluster (S3 endpoint and the backups bucket name in `config`, the encrypted admin API token in `admin_secret_enc`). Each resource's `external_name` holds its bucket name, and its access key id and encrypted secret live in `resource_credentials`.
- Resource kinds are `postgres`, `cache` and `storage` everywhere: in the API, plans, hosts and drivers.

## Backend plan

The backend is a TypeScript monorepo on Node (current LTS) with two processes sharing one core library: an HTTP API built on Hono, and a worker that does everything slow or scheduled. All SQL is hand-written. There is no ORM and no query builder.

**Stack**

| Concern | Choice | Why |
| --- | --- | --- |
| Runtime and language | Node LTS, TypeScript (strict) | Types catch shape errors without an ORM |
| HTTP | Hono with `@hono/node-server` | Small, fast, typed routes |
| Postgres driver | `postgres` (postgres.js) | Tagged-template SQL, automatic parameterization |
| Redis driver | `ioredis` | Mature, supports ACL commands |
| Validation | Zod via `@hono/zod-validator` | Request and response shapes in one place |
| Migrations | Numbered `.sql` files plus a small runner | Forward-only, no framework to learn |
| Passwords and keys | argon2id; SHA-256 for API keys and tokens | API keys are high-entropy, so a fast hash is fine |
| Logging and metrics | pino, `prom-client` | Structured logs and Prometheus metrics |
| Tests | Vitest plus real Postgres, Redis and Garage in containers | Mocks hide the bugs that matter here |
| Object storage | Garage admin API over `fetch`; AWS SDK v3 S3 client for data access and presigning | No extra admin library |
| Edge proxy | Traefik v3 | TCP routing by SNI, ACME, HTTP provider for dynamic config |
| Dashboard | Next.js (App Router) on the public API only | One UI for members and admins |

**Repository layout**

```text
sparkbase/
  apps/
    api/          Hono app: routes, auth middleware, rate limits, Traefik config endpoint
    worker/       job loop: provisioning, limits, backups, email
    dashboard/    web UI on the public API
  packages/
    core/         domain logic; no HTTP, no SQL strings
    db/           migrations/*.sql, queries/*.ts, migration runner
    drivers/      postgres.ts, cache.ts, storage.ts
    sdk/          @sparkbase/sdk
    cli/          sparkbase command line, including admin commands
  deploy/
    docker-compose.yml       single-host install with Traefik
    traefik/                 static config, entrypoints, cert resolvers
    hosts/                   bootstrap for extra data hosts
  docs/
```

**Data access without an ORM.** Each query is a small exported function that owns its SQL and returns a typed row. Callers never see SQL strings.

```ts
// packages/db/queries/resources.ts
import { sql } from '../client'

export async function getResource(id: string) {
  const [row] = await sql<ResourceRow[]>`
    select r.*, p.limits
    from resources r join plans p on p.id = r.plan_id
    where r.id = ${id}`
  return row ?? null
}
```

**The driver contract.** Every resource kind implements one interface. The API and worker only ever talk to this interface.

```ts
interface ResourceDriver {
  kind: 'postgres' | 'cache' | 'storage'
  provision(input: ProvisionInput): Promise<ProvisionResult>
  deprovision(r: Resource): Promise<void>
  pause(r: Resource): Promise<void>
  resume(r: Resource): Promise<void>
  rotateCredentials(r: Resource): Promise<Credentials>
  applyLimits(r: Resource, limits: PlanLimits): Promise<void>
  measureUsage(r: Resource): Promise<Usage>
  setWriteLock(r: Resource, locked: boolean): Promise<void>
}
```

**Provisioning a Postgres resource.** The API inserts a `resources` row in `provisioning` and enqueues a job. The worker then runs these steps, each idempotent and recorded in `provision_state`, so a crash or retry never creates duplicates:

1. Pick the least-loaded active Postgres host.
2. Generate a role name and a random password; `CREATE ROLE ... LOGIN CONNECTION LIMIT n`.
3. `CREATE DATABASE ... OWNER role` (outside a transaction), then `REVOKE ALL ON DATABASE ... FROM PUBLIC`.
4. Apply per-role settings: `statement_timeout`, `idle_in_transaction_session_timeout`, `work_mem`.
5. Enable allowlisted extensions requested by the plan.
6. Set the PgBouncer per-database pool limits and set `endpoint` to `<slug>.db.<domain>`, so the Traefik config endpoint starts routing it.
7. Store the encrypted password, set `status = 'active'`, write an audit row.

**Background jobs**

| Job | Trigger | Does |
| --- | --- | --- |
| `provision_resource`, `deprovision_resource` | API call | Create or tear down on a host |
| `sample_usage` | Every minute (Postgres and storage every 5 min) | Calls `measureUsage` on active resources, inserts samples |
| `enforce_limits` | After each sample run | Warns, locks or unlocks writes by plan limits |
| `pause_inactive` | Hourly, only when the admin enables auto-pause | Pauses idle resources and notifies owners |
| `backup_resource` | Nightly | Per-database dump to the private backups bucket |
| `verify_backups` | Weekly | Restores a random backup into a scratch database and checks it |
| `move_resource` | Admin action | Copy to another host, switch routing, retire old copy |
| `send_email` | Any | Invites, limit warnings, pause notices (only when SMTP is configured) |

**API auth and limits.** Dashboard sessions use httpOnly cookies. Programmatic access uses personal or project-scoped API keys in the form `sb_<random>`, stored as a hash and shown once. Rate limits are sliding windows in Redis, keyed by API key or IP.

**Configuration** is environment variables only, validated at startup:

| Variable | Required | Notes |
| --- | --- | --- |
| `SPARKBASE_DOMAIN` | yes | Base domain, e.g. `example.com` |
| `DATABASE_URL` | yes | Control-plane Postgres |
| `REDIS_URL` | yes | Control-plane Redis |
| `ENCRYPTION_KEY` | yes | 32 bytes, base64. Encrypts stored credentials |
| `TRAEFIK_CONFIG_TOKEN` | yes | Shared secret for the Traefik HTTP provider endpoint |
| `ACME_EMAIL` | with ACME | Let's Encrypt account email |
| `TLS_MODE` | no | `acme-dns` (default), `acme-http` or `custom` |
| `SMTP_URL` | no | Without it, invite and reset links are shown to the admin and emails are logged |

Per-host settings live in the `hosts` table. The bundled data hosts are registered automatically on first start from their compose service addresses, so a single-host install needs no `host add`.

**Testing.** Unit tests for `core` with no I/O. Integration tests run the API against real Postgres, Redis and Garage containers. Each driver has a contract test suite run against the real engine, covering provision, isolation (tenant A cannot reach tenant B), limits enforced, pause and resume, and deprovision. An end-to-end suite runs the full compose stack with Traefik and connects through it with real clients. The isolation tests are the most important tests in the repository.

## API reference

The API has two layers. The **management API** creates and configures projects and resources. The **data API** gives HTTP access to the resources themselves, for serverless and edge runtimes where a raw TCP connection is awkward. Anything the data API can do, a native driver can do too.

**Conventions**

- Base URL `https://api.<domain>/v1`. JSON in and out. IDs are UUIDs.
- Auth: `Authorization: Bearer sb_...` (API key) or a dashboard session cookie.
- List endpoints use cursor pagination: `?limit=50&cursor=...`.
- `POST` creates accept an `Idempotency-Key` header.
- Errors share one shape: `{ "error": { "code": "quota_exceeded", "message": "...", "details": {} } }`.
- Rate limit headers on every response: `X-RateLimit-Limit`, `-Remaining`, `-Reset`.

**Setup, account and keys**

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/setup` | Whether first-run setup is still open |
| POST | `/setup` | Create the first admin; needs the setup token printed in the API logs; closes once any user exists |
| POST | `/auth/signup` | Accept an invite (token + password), or sign up when open signup is on |
| POST | `/auth/login` | Start a session |
| POST | `/auth/logout` | End the session |
| POST | `/auth/verify-email` | Confirm the emailed token |
| POST | `/auth/password/reset` | Request or complete a password reset |
| GET | `/me` | Current user |
| PATCH | `/me` | Update name, email, password |
| GET | `/api-keys` | List keys (prefix only, never the secret) |
| POST | `/api-keys` | Create a key, optionally scoped to one project; secret returned once |
| DELETE | `/api-keys/{id}` | Revoke a key |

**Admin** (role `admin` only)

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/admin/users` | List users |
| POST | `/admin/invites` | Invite by email with a role; returns the invite link |
| DELETE | `/admin/invites/{id}` | Cancel an invite |
| PATCH | `/admin/users/{id}` | Change role, disable or enable |
| GET | `/admin/settings` | Install settings |
| PATCH | `/admin/settings` | Signup mode, auto-pause, per-user resource cap |
| GET/POST/PATCH | `/admin/plans`, `/admin/plans/{id}` | Manage plans and their limits |
| GET/POST/PATCH | `/admin/hosts`, `/admin/hosts/{id}` | Register, drain or take hosts offline |
| POST | `/admin/resources/{id}/suspend` | Suspend any resource |
| POST | `/admin/resources/{id}/unsuspend` | Lift a suspension |
| POST | `/admin/resources/{id}/move` | Move a resource to another host |

**Projects**

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/projects` | List your projects |
| POST | `/projects` | Create a project (`name`, optional `slug`) |
| GET | `/projects/{projectId}` | Project detail with resource summary |
| PATCH | `/projects/{projectId}` | Rename |
| DELETE | `/projects/{projectId}` | Delete the project and every resource in it (async) |

**Resources**

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/plans?kind=postgres` | Plans and limits for a resource kind |
| GET | `/projects/{projectId}/resources` | List resources in a project |
| POST | `/projects/{projectId}/resources` | Create a resource (`kind`, `name`, optional `plan`, else the kind's default); returns `202` with status `provisioning` |
| GET | `/resources/{id}` | Detail and current status |
| PATCH | `/resources/{id}` | Rename or change plan (moving to a smaller plan is refused if usage exceeds its limits) |
| DELETE | `/resources/{id}` | Delete (async) |
| POST | `/resources/{id}/pause` | Pause |
| POST | `/resources/{id}/resume` | Resume |
| GET | `/resources/{id}/connection` | Connection string and parts (host, port, user, password, database) |
| POST | `/resources/{id}/credentials/rotate` | Issue new credentials; old ones revoked after a grace period |
| GET | `/resources/{id}/usage?from=&to=&metric=` | Usage samples against plan limits |
| GET | `/resources/{id}/backups` | List backups |
| POST | `/resources/{id}/backups` | Take a backup now |
| POST | `/resources/{id}/backups/{backupId}/restore` | Restore (to the same or a new resource) |

Whether members may change their own resources' plans is an admin setting. When it is off, only admins can change plans.

**Data API: Postgres**

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/resources/{id}/postgres/query` | Run one parameterized statement: `{ "sql": "...", "params": [] }` |
| POST | `/resources/{id}/postgres/transaction` | Run several statements atomically |
| GET | `/resources/{id}/postgres/schema` | Tables, columns and indexes |

**Data API: cache**

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/resources/{id}/cache/keys/{key}` | Read a value |
| PUT | `/resources/{id}/cache/keys/{key}` | Write a value, optional `ttl` seconds |
| DELETE | `/resources/{id}/cache/keys/{key}` | Delete |
| POST | `/resources/{id}/cache/batch` | Several get, set or delete operations in one call |

**Data API: object storage**

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/resources/{id}/storage/objects?prefix=` | List objects |
| POST | `/resources/{id}/storage/presign-upload` | Presigned PUT URL, so files go straight to storage |
| POST | `/resources/{id}/storage/presign-download` | Presigned GET URL |
| DELETE | `/resources/{id}/storage/objects/{key}` | Delete an object |

**Operational**

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/health` | Liveness for the API |
| GET | `/status` | Host health and queue depth (admins see detail, others a summary) |
| GET | `/internal/traefik/config` | Traefik dynamic config; internal network only, token protected |
| GET | `/metrics` | Prometheus metrics; internal network only |

**Example: create a resource**

```http
POST /v1/projects/8f1c.../resources
Authorization: Bearer sb_...
Content-Type: application/json

{ "kind": "postgres", "name": "main" }
```

```json
{
  "id": "2b7e...",
  "kind": "postgres",
  "name": "main",
  "plan": "postgres-small",
  "status": "provisioning",
  "endpoint": "a1b2c3.db.example.com"
}
```

Poll `GET /resources/{id}` until `status` is `active`.

**Error codes**

| Code | HTTP | Meaning |
| --- | --- | --- |
| `unauthorized` | 401 | Missing or invalid key or session |
| `forbidden` | 403 | Key scope or role does not cover this project or action |
| `not_found` | 404 | No such project or resource |
| `resource_limit_reached` | 403 | The per-user resource cap set by the admin is reached |
| `quota_exceeded` | 429 | A plan limit (storage, memory, requests) is hit |
| `resource_not_ready` | 409 | Resource is provisioning, paused or suspended |
| `plan_change_blocked` | 409 | Usage exceeds the target plan's limits |
| `setup_closed` | 409 | First-run setup was already completed |
| `validation_failed` | 422 | Bad request body |

## SDK, CLI and connection strings

The SDK is a convenience layer on top of native access. It manages resources, wraps the data API, and hands back connection strings for anyone who wants to use their own driver. Because every install has its own URL, the SDK and CLI always take the API base URL.

```ts
import { Sparkbase } from '@sparkbase/sdk'

const sb = new Sparkbase({
  url: 'https://api.example.com',
  apiKey: process.env.SPARKBASE_API_KEY,
  project: 'a1b2c3',
})

// Relational: HTTP query, or the native connection string
const db = sb.postgres('main')
const { rows } = await db.query('select * from users where id = $1', [id])
const url = await db.connectionString({ pooled: true })

// Cache
const cache = sb.cache('sessions')
await cache.set('user:42', { name: 'Ada' }, { ttl: 60 })

// Object storage
const files = sb.storage('uploads')
const { url: putUrl } = await files.presignUpload('avatars/42.png')

// Management
await sb.resources.create({ kind: 'cache', name: 'sessions' })
```

**SDK rules**

- Zero required dependencies in the core package; adapters (for example a `pg` or `ioredis` helper) are optional.
- Works in Node, Bun, Deno and edge runtimes, since it uses `fetch`.
- Every method maps one-to-one to a documented endpoint. No hidden behavior.
- Typed errors that carry the API error `code` (`QuotaExceededError`, `ResourceNotReadyError`).

**CLI**

| Command | Does |
| --- | --- |
| `sparkbase login --url https://api.example.com` | Authenticate against an install and store a key locally |
| `sparkbase init` | Create or link a project in the current folder |
| `sparkbase resources add postgres` | Add a resource (`--plan` optional) |
| `sparkbase resources list` | Show resources and status |
| `sparkbase env pull` | Write connection strings into `.env` without touching other lines |
| `sparkbase usage` | Show usage against limits |
| `sparkbase admin invite <email>` | Admins: invite a user |
| `sparkbase admin host add --kind postgres` | Admins: register another data host |

**Connection strings**

| Resource | Format |
| --- | --- |
| Postgres (pooled, default) | `postgres://sb_x1y2:PASSWORD@a1b2c3.db.example.com:6432/main?sslmode=require` |
| Postgres (direct, for migrations) | `postgres://sb_x1y2:PASSWORD@a1b2c3.db.example.com:5432/main?sslmode=require` |
| Cache | `rediss://sb_x1y2:PASSWORD@a1b2c3.cache.example.com:6380` |
| Object storage | Endpoint `https://s3.example.com`, bucket `sb-2b7e...`, access key id and secret, region `garage`, path-style addressing; or the data API and presigned URLs |

Pooled connections run in transaction mode, so session-level features (advisory locks, `LISTEN/NOTIFY`, some prepared statements) need the direct endpoint, which has a lower connection limit. The docs must say this on the first page a Postgres user reads.

## Plans and limits

Plans are named limits presets the admin manages. They carry no prices. A fresh install is seeded with three plans per kind, and one of them is the default for new resources. Admins can edit, add or deactivate plans, and set a per-user cap on the number of resources (default 10, unlimited for admins).

**Postgres (seeded)**

| Limit | Small (default) | Medium | Large |
| --- | --- | --- | --- |
| Storage | 1 GB | 10 GB | 50 GB |
| Pooled connections | 20 | 60 | 200 |
| Direct connections | 3 | 10 | 25 |
| Statement timeout | 30 s | 60 s | 300 s |
| Data API requests per month | 100k | 1M | Unlimited |
| Backups kept | 3 days | 7 days | 14 days |

**Cache (seeded)**

| Limit | Small (default) | Medium | Large |
| --- | --- | --- | --- |
| Memory | 32 MB | 256 MB | 1 GB |
| Connections | 10 | 50 | 200 |
| Data API requests per month | 100k | 1M | Unlimited |

**Object storage (seeded)**

| Limit | Small (default) | Medium | Large |
| --- | --- | --- | --- |
| Storage | 2 GB | 25 GB | 100 GB |
| Data API requests per month | 100k | 1M | Unlimited |

Traffic that goes straight to Garage through native S3 or presigned URLs does not pass through Sparkbase, so it is not metered. There is no egress limit for storage.

The seeded numbers are starting points for a small VPS. The docs include a sizing guide that relates host RAM and disk to how many resources of each plan a host can carry.

## Self-hosting

Self-hosting is the product. The default install is one server running everything; multi-host is the same code with more `hosts` rows.

**Single host**

```bash
git clone https://github.com/shellhaki/sparkbase && cd sparkbase/deploy
cp .env.example .env      # set SPARKBASE_DOMAIN, ACME_EMAIL, DNS provider token
./generate-secrets.sh     # fills ENCRYPTION_KEY, TRAEFIK_CONFIG_TOKEN, internal passwords
docker compose up -d
docker compose logs api | grep "setup token"
```

Then open `https://app.<domain>/setup`, paste the setup token and create the admin account.

`deploy/docker-compose.yml` starts:

| Service | Purpose | Published ports |
| --- | --- | --- |
| `traefik` | Edge proxy and certificates | 80, 443, 5432, 6432, 6380 |
| `api` | Control-plane API; runs migrations at startup | none |
| `worker` | Background jobs | none |
| `dashboard` | Web UI | none |
| `control-db` | Control-plane Postgres | none |
| `control-redis` | Control-plane Redis | none |
| `pg-host` + `pgbouncer` | Tenant Postgres and its pooler | none |
| `cache-host` | Tenant Redis with ACLs | none |
| `garage` | Object storage | none |

All data lives in named volumes. Internal services sit on a private network that Traefik bridges; data hosts accept connections only from Traefik and the worker.

**Already running Traefik?** Many self-hosters run Traefik already (Coolify, Dokploy, a home lab). The compose file has a profile without the bundled Traefik, plus documented labels and the HTTP provider endpoint to add to an existing Traefik, including the TCP entrypoints it needs.

**Multiple hosts.** `sparkbase admin host add --kind postgres --addr ...` registers another data host prepared with `deploy/hosts/` bootstrap scripts. The scheduler spreads new resources across hosts by weight, and Traefik routes each resource to its host.

**Upgrades.** Semantic versions, Docker images on a public registry, forward-only SQL migrations run at startup, and upgrade notes in every release. `docker compose pull && docker compose up -d` is the upgrade path.

**Backups of the install itself.** The docs cover backing up the control-plane database and `ENCRYPTION_KEY` together, since tenant credentials cannot be decrypted without the key.

## Security, backups and operations

A shared-host install is trusted only as far as its isolation holds. Treat a cross-tenant leak as the worst failure the project can have.

**Security baseline**

- TLS everywhere: certificates per hostname or wildcard through Traefik, `sslmode=require` on Postgres, `rediss://` for the cache, HTTPS only for the API, dashboard and S3 endpoint.
- Credentials encrypted at rest with AES-256-GCM; the key comes from the environment and never sits in the database. API keys, session tokens and passwords are stored only as hashes.
- Tenant roles are never superusers and cannot create roles or databases. Data hosts accept connections only from Traefik and the worker on the internal network.
- Credential rotation per resource, with a grace period during which both old and new credentials work.
- Audit log for every create, delete, rotate, plan change, pause, suspend, invite, role change and limit event.
- Rate limits on login, signup, setup and every API key. Account lockout after repeated failed logins.
- First-run setup requires a one-time token from the server logs, so a fresh install exposed to the internet cannot be claimed by someone else.
- Dependency scanning in CI, a private security contact, and a published disclosure policy.

**Backups**

| Resource | Method | Retention | Restore test |
| --- | --- | --- | --- |
| Postgres resources | Nightly per-database logical dump, encrypted, to the private backups bucket | By plan | Automated weekly restore of a random backup |
| Control-plane DB | Nightly dump with the same job | 14 days | Covered by the weekly restore test |
| Postgres hosts | Optional WAL archiving (WAL-G or pgBackRest) for point-in-time recovery | Operator choice | Documented drill |
| Cache | None by default; optional RDB snapshot | Latest | On request |
| Object storage | Garage replication when the operator runs more than one Garage node; otherwise disk-level backups documented | n/a | n/a |

Backups can go to an off-site S3-compatible bucket by setting a separate backups backend, which is the recommended setup: a backup on the same disk as the data does not survive that disk.

**Moving a resource between hosts.** The stable hostname makes this routine:

1. Restore a copy on the target host from a fresh dump (or use logical replication for large databases).
2. Briefly lock writes on the source and apply the final changes.
3. Verify row counts and a checksum sample.
4. Switch `host_id`, which changes the Traefik route within seconds, and update the pooler.
5. Keep the source read-only for a few days, then drop it.

**Observability**

- Metrics (Prometheus, internal endpoint): per-host connections, disk and memory; per-resource usage versus limit; API latency and error rates; job queue depth and failure counts.
- Logs: structured JSON from the API and worker on stdout, so any Docker log driver works.
- An optional compose profile adds Prometheus and Grafana with a ready dashboard.
- The admin dashboard shows host health, queue depth, failed jobs and recent limit breaches.

**Runbooks** in the docs: add a host, drain a host, move a resource, restore a database, restore the whole install, rotate the master encryption key, respond to a suspected cross-tenant incident, and recover from an expired or failed certificate.

## Roadmap

Five stages, each ending at a gate with a check you can run. The first gate, isolation, decides whether the rest is worth building.

| Stage | Goal | Gate |
| --- | --- | --- |
| 0. Foundations | Control plane, accounts, invites, keys, jobs, hosts, isolation harness | Harness exists and can fail |
| 1. Postgres resource | Native Postgres through Traefik with isolation and limits | Isolation tests green; fresh install to a connection in minutes |
| 2. Cache, storage, data API, SDK | The other two resources, the HTTP data API, SDK and CLI | Isolation green for all three; SDK and CLI work end to end |
| 3. Operations | Backups with restore tests, observability, admin tools, security review, runbooks | Restore works; security checklist done |
| 4. First release | Self-hosting package, docs, release pipeline | Clean server to working resource by following the docs alone |
| 5. Later | Vector, NoSQL, MySQL, auth, realtime, queues | Each new kind passes the same isolation suite |

**First release scope**

| In the first release | Not in the first release |
| --- | --- |
| Postgres, cache, object storage | MongoDB, vector, MySQL |
| Shared hosts, single-host compose install | Dedicated instances per resource, automatic failover |
| Admin, invites, plans as limits presets | Organizations and teams with shared projects |
| Native connection strings, data API, TypeScript SDK, CLI | Other SDK languages, webhooks |
| Backups with automated restore tests | Multi-region |
| Traefik with ACME, bring-your-own-Traefik profile | Kubernetes and Helm |

## Community and adoption

The project succeeds if people run it and trust it. The strategy is to make the first install effortless and the isolation story easy to verify.

- **The repo is the front door.** README with a 60-second demo, a clear quickstart, a public roadmap, a "good first issue" label and a changelog.
- **Landing page.** Explains the project, links to GitHub, and collects a release list (one email at first release) and a newsletter for build-in-public updates.
- **Build in public.** Post the isolation tests, the provisioning flow and the Traefik routing as they land. Tie it to the 100 Days of Backend series.
- **Docs as onboarding.** Quickstarts per resource, framework guides (Hono, Express, NestJS, Next.js, FastAPI, Go), a sizing guide and the runbooks.
- **Templates.** Starter repos with `.env` wiring done, each using Postgres, cache and storage.
- **Launch.** Show HN, Reddit's self-hosting communities, dev.to, and self-hosted app catalogs (for example the Coolify and Dokploy templates) once the first release passes its gate.

**What to watch:** time from `docker compose up` to a first successful connection, install failures reported in issues, GitHub stars, contributors, and opt-in install counts (no telemetry without consent).

## Licensing

| Part | License | Reason |
| --- | --- | --- |
| Control plane, worker, drivers, dashboard | AGPL-3.0 | Anyone who runs a modified copy as a public service shares their changes |
| SDK and CLI | MIT | Nothing discourages putting them in a closed-source app |

Contribution policy: DCO (a `Signed-off-by` line on each commit), decided before the first outside pull request.

## Risks and open questions

The biggest risks are a cross-tenant data leak and an install that is too hard to get running. Both are manageable if the first release stays small and the isolation and install tests come first.

**Risks**

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Cross-tenant data access | Fatal to trust | Per-resource roles and credentials, `REVOKE ... FROM PUBLIC`, isolation contract tests in CI, an external review before the first release |
| Install fails on real servers | People give up in the first ten minutes | End-to-end install test in CI on a clean VM, clear error messages for DNS and certificate problems |
| Certificate issuance problems | Resources unreachable over TLS | `acme-dns` wildcard as default, documented `custom` mode, a dashboard warning when a certificate fails |
| Postgres clients without SNI | Cannot be routed by Traefik | Document minimum client versions; offer a per-host port mapping fallback for old clients |
| Noisy neighbors on shared hosts | Slow or failing tenants | Connection and timeout limits, per-host monitoring, move heavy resources to another host |
| Shared Redis eviction and memory | One tenant degrades others | Reconsider a process per resource for the cache |
| Single server fails | All resources down | Off-site backups by default in the docs; optional WAL archiving; restore runbook |
| Garage feature gaps | Garage implements a subset of S3 (no bucket policies, no STS, limited versioning) | Document supported S3 features; test common clients (AWS SDK, `aws s3`, `rclone`) in CI |
| Scope too wide for one builder | Nothing ships | First release is Postgres, cache and storage only |
| Data loss | Fatal | Backups restored automatically every week; documented retention |

**Open questions to decide**

- [ ] Redis: shared instance with ACL prefixes (current default), or a small process per resource?
- [ ] When to add external S3 backends (R2, AWS S3) with the prefix-per-resource model?
- [ ] Should members be able to change their own resources' plans by default?
- [ ] How does Sparkbase relate to SparkDB: rebrand, successor, or separate?
- [ ] Does MySQL or MariaDB come in Phase 2, or wait until people ask?
- [ ] Which SDK languages after TypeScript: Python, Go?
- [ ] Organizations or shared projects: needed for the first release, or later?

**Sources.** This spec is a plan built from the decisions in our discussion and general engineering practice. It cites no external data. Default limits are starting points to be tuned on real hardware. Claims about Traefik and Garage features must be checked against their current documentation when each step is built.
