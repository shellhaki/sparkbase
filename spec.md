# Sparkbase: Product & Technical Spec

Oct 5, 2026 · @Excel

## Overview

Sparkbase is a developer data platform: you create a project, attach only the data services your backend needs, and connect to each one through a native connection string, an SDK, or a public API. Nothing is bundled. A project with a Postgres database and a cache pays for those two things and nothing else.

**Pitch:** the data layer for your backend, with exactly the services you need and none you don't.

**Who it is for**

- Solo developers, students and indie hackers who want managed data services without adopting a full backend-as-a-service.
- Small teams and freelancers running many small projects, where per-resource billing beats per-project bundles.
- Developers building AI features who need a vector store next to their relational data.
- Teams that want to self-host the same stack on their own infrastructure.

**Positioning.** Bundled platforms such as Supabase and Firebase are built around their own auth and client libraries. Single-service providers (a database host, a Redis host, a bucket) each need their own account, bill and dashboard. Sparkbase is one project, one dashboard, one SDK, and a bill that lists only the resources you attached.

**Product principles**

1. **Pay for exactly what you attach.** Pricing and onboarding both reflect it.
2. **Native first.** Every resource speaks its standard protocol, so existing drivers, ORMs and tools keep working. The SDK and API are conveniences, never a wall.
3. **Shared tenancy now, dedicated later.** Stable per-project hostnames make moving a project between hosts invisible to customers.
4. **Quotas are a product feature.** Hard caps and clear upgrade prompts instead of surprise bills.
5. **Open source and self-hostable from day one.** The cloud runs the same code a self-hoster runs.
6. **Boring technology.** Node, Hono, Postgres, Redis, and plain SQL with no ORM.

## Product model

The whole product is four nouns: a **user** owns **projects**, a project holds **resources**, and each resource sits on a **plan** that sets its limits.

| Concept | What it is | Notes |
| --- | --- | --- |
| User | A person with a login and API keys | Organizations and teams come after MVP |
| Project | A named container, free to create | Has a short slug used in hostnames, e.g. `a1b2c3` |
| Resource | One data service inside a project | Has a kind, a plan, a host, a status and credentials |
| Plan | A tier for one resource kind (Free, Starter, Pro) | Defines storage, connections, memory, bandwidth |
| Host | A server that runs many tenants of one engine | Invisible to users; chosen by the scheduler |

**Resource catalog**

| Resource | Engine | Native protocol | Phase |
| --- | --- | --- | --- |
| Relational database | PostgreSQL | Postgres wire | MVP |
| Cache | Redis (Valkey-compatible) | RESP over TLS | MVP |
| Object storage | S3-compatible (Cloudflare R2 in production, MinIO for local development) | S3 API | MVP |
| NoSQL database | MongoDB | Mongo wire | Phase 2 |
| Vector database | pgvector first, dedicated engine later | Postgres wire + SDK | Phase 2 |
| Relational (other engines) | MySQL, MariaDB | MySQL wire | Phase 2 |
| Auth | Built in | HTTP + SDK | Phase 3 |
| Realtime | Built in | WebSocket + SDK | Phase 3 |
| Queues | Built in | HTTP + SDK | Phase 3 |

**The core user flow**

1. Sign up and create a project (no payment, no resource yet).
2. Add a resource: pick a kind, a name and a plan.
3. Wait a few seconds while it provisions, then copy the connection string or run `sparkbase env pull`.
4. Connect with any native driver, or use the SDK and public API.
5. Add or remove resources later. Billing follows what is attached.

**Resource lifecycle**

| Status | Meaning | Reached by |
| --- | --- | --- |
| provisioning | Worker is creating it on a host | Create call |
| active | Healthy and usable | Provisioning succeeded, or resume |
| paused | Connections refused, data kept | Free-tier inactivity or owner action |
| suspended | Blocked for quota or billing, data kept | Quota job or failed payment |
| failed | Provisioning did not finish | Retries exhausted; owner can retry or delete |
| deleting | Teardown in progress | Delete call |

## Architecture

Sparkbase splits into a control plane that decides things and a data plane that serves them. A customer's application talks to the data plane directly over native protocols, so the control plane can be down for a while without breaking a running app.

&#91;embedded content: architecture · control plane, data plane, two client paths\]

The dashboard, CLI and SDK call the API. The API writes metadata and queues jobs, and the worker claims them and acts on the data hosts. Applications skip all of that and connect straight to their project hostname.

**Components**

| Component | Role | Technology |
| --- | --- | --- |
| API | Auth, projects, resources, plans, the data API | Node, Hono |
| Worker | Provisioning, usage sampling, quota enforcement, pausing, backups, host moves | Node, same codebase |
| Control DB | Users, projects, resources, plans, hosts, jobs, audit log | Postgres |
| Redis | Rate limits, short-lived counters, cache for the API | Redis |
| Postgres hosts | Tenant databases behind a pooler | Postgres, PgBouncer |
| Redis hosts | Tenant cache namespaces | Redis or Valkey with ACLs |
| Object storage | Tenant folders (key prefixes) in shared buckets | Cloudflare R2 in production, MinIO for local development |
| DNS and TLS | Stable hostname per resource, certificates | Wildcard DNS and automated certificates |
| Dashboard | Web UI over the same public API | Built after the API settles |

**Architecture rules**

- **The control plane is not on the data path.** Native connections never pass through the API. Only the optional HTTP data API does.
- **Hosts are interchangeable.** A host is a row with a weight and a status. Adding capacity is registering another host; retiring one is draining it.
- **One stable hostname per resource.** Customers never see which host holds their data, which is what makes moves and dedicated instances possible.
- **Secrets live in one place.** Credentials are stored encrypted in the control DB and are decrypted only by the worker and the API when needed.
- **Every long operation is a job.** The API answers fast with a status, the worker does the slow part, and retries are safe because each step is idempotent.

## Tenancy and isolation

The MVP runs shared hosts: each engine lives on a small pool of large servers (start with one, grow to about five), and every project gets an isolated slice of them. A dedicated instance later is the same operation as moving a project to another host, so the design below does not change.

**Per-engine isolation**

| Engine | Unit of isolation | Access control | Main risk |
| --- | --- | --- | --- |
| Postgres | One database plus one role per resource | `REVOKE CONNECT ON DATABASE ... FROM PUBLIC`, non-superuser roles, `scram-sha-256`, TLS required | Noisy neighbors on CPU and IO; no native CPU quota |
| Redis | One ACL user per resource, restricted to a key prefix | `~prefix:*` key pattern, dangerous commands denied (`KEYS`, `FLUSHALL`, `CONFIG`, `DEBUG`, Lua at first) | Memory and CPU are global; eviction can hit other tenants |
| Object storage | One folder (key prefix) per resource inside a shared bucket | Prefix enforced by the data API and presigned URLs; optional temporary credentials scoped to bucket and prefix | R2 has no long-lived prefix keys or per-prefix usage figures; direct traffic is hard to meter |

**Postgres specifics**

- Put PgBouncer (or PgCat) in front in transaction mode. Connections run out long before CPU does. Offer a direct, low-limit endpoint for migrations and anything needing session features.
- Set per-role `CONNECTION LIMIT`, `statement_timeout`, `idle_in_transaction_session_timeout` and `work_mem`.
- Allowlist extensions (for example `pgcrypto`, `uuid-ossp`, `pg_trgm`, `pgvector`). Do not allow `dblink`, `file_fdw`, or untrusted procedural languages.
- Never grant `pg_read_server_files`, `pg_write_server_files` or `pg_execute_server_program`.

**Redis specifics.** Sharing one Redis process gives weak isolation for a cache. One tenant filling memory triggers eviction for everyone, and per-prefix memory has to be estimated by sampling. The default in this spec follows your decision (shared, ACL plus prefix), but a small Redis or Valkey process per project costs only a few MB when idle and gives a real `maxmemory` per tenant. That is the one exception to "no dedicated instances" worth reconsidering. It is listed in the open questions.

**Quota enforcement.** Plan limits only mean something if something enforces them. Each limit has a mechanism, a check frequency and a defined action.

| Limit | Mechanism | Checked | Action on breach |
| --- | --- | --- | --- |
| Postgres storage | `pg_database_size()` sampled by a worker job | Every 5 min | Warn at 80% and 90%; at 100% set `default_transaction_read_only = on` for the role |
| Postgres connections | Role `CONNECTION LIMIT` plus pooler pool size | Hard | New connections refused |
| Query time | Role `statement_timeout` | Hard | Query cancelled |
| Redis memory | Sampled key scan with `MEMORY USAGE` (shared) or `maxmemory` (per project) | Every 1 min | Switch ACL user to read-only or delete-only until under limit |
| Redis clients | `CLIENT LIST` by user | Every 1 min | Kill excess clients |
| Object storage bytes | Running size counter reconciled by a paginated prefix listing | Every 5 min | Data API writes refused and no new write credentials issued |
| Bandwidth and API requests | Metered at the data API, counters in Redis; direct R2 traffic is not metered | Rolling month | Throttle, then deny with `quota_exceeded` |

Every breach writes an `audit_log` row, emails the owner, and shows a banner with an upgrade button. Limits clear automatically as soon as usage drops or the plan changes.

**Free-tier auto-pause.** A free resource with no connection or API activity for 7 days is paused (Postgres role set to `NOLOGIN`, Redis ACL user disabled, bucket policy denies all). The owner gets an email on day 5. Resuming is one click and takes seconds. A resource paused for 60 days is deleted after emails at days 14, 30 and 53.

## Control plane data model

The control plane keeps its own metadata in a dedicated Postgres database, separate from every tenant host. The schema below is the starting migration (`0001_init.sql`). Credentials are stored encrypted, usage samples are append-only, and background work runs from a `jobs` table using `FOR UPDATE SKIP LOCKED`, so self-hosters need no extra queue software.

```sql
create extension if not exists pgcrypto;

create table users (
  id            uuid primary key default gen_random_uuid(),
  email         text not null unique,
  password_hash text not null,               -- argon2id
  name          text,
  email_verified_at timestamptz,
  created_at    timestamptz not null default now()
);

create table projects (
  id         uuid primary key default gen_random_uuid(),
  owner_id   uuid not null references users(id) on delete cascade,
  slug       text not null unique,           -- short id used in hostnames
  name       text not null,
  status     text not null default 'active'
             check (status in ('active','suspended','deleting')),
  created_at timestamptz not null default now()
);

create table hosts (
  id              uuid primary key default gen_random_uuid(),
  kind            text not null check (kind in ('postgres','redis','storage')),
  region          text not null,
  internal_addr   text not null,             -- admin connection target
  public_hostname text not null,             -- what customer CNAMEs point at
  admin_secret_enc bytea not null,
  weight          int  not null default 100, -- relative capacity
  status          text not null default 'active'
                  check (status in ('active','draining','offline')),
  created_at      timestamptz not null default now()
);

create table plans (
  id          text primary key,              -- e.g. 'postgres-starter'
  kind        text not null,
  name        text not null,
  price_cents int  not null default 0,
  limits      jsonb not null,                -- {"storage_mb":5120,"connections":30,...}
  active      boolean not null default true
);

create table resources (
  id             uuid primary key default gen_random_uuid(),
  project_id     uuid not null references projects(id) on delete cascade,
  kind           text not null,
  name           text not null,
  plan_id        text not null references plans(id),
  host_id        uuid references hosts(id),
  status         text not null default 'provisioning'
                 check (status in ('provisioning','active','paused',
                                   'suspended','failed','deleting')),
  external_name  text,                       -- db name, key prefix or bucket
  endpoint       text,                       -- stable per-project hostname
  last_active_at timestamptz,
  created_at     timestamptz not null default now(),
  unique (project_id, name)
);
create index resources_host_idx on resources (host_id);

create table resource_credentials (
  id          uuid primary key default gen_random_uuid(),
  resource_id uuid not null references resources(id) on delete cascade,
  username    text not null,
  secret_enc  bytea not null,                -- AES-256-GCM, key from env or KMS
  created_at  timestamptz not null default now(),
  revoked_at  timestamptz
);

create table api_keys (
  id           uuid primary key default gen_random_uuid(),
  user_id      uuid not null references users(id) on delete cascade,
  project_id   uuid references projects(id) on delete cascade, -- null = account-wide
  name         text not null,
  prefix       text not null,                -- first 8 chars, shown in UI
  key_hash     text not null,                -- sha256 of the full key
  scopes       text[] not null default '{}',
  last_used_at timestamptz,
  expires_at   timestamptz,
  revoked_at   timestamptz,
  created_at   timestamptz not null default now()
);

create table usage_samples (
  resource_id uuid not null references resources(id) on delete cascade,
  metric      text not null,                 -- 'storage_bytes','connections',...
  sampled_at  timestamptz not null,
  value       bigint not null,
  primary key (resource_id, metric, sampled_at)
);                                          -- partition by month once it grows

create table jobs (
  id           bigserial primary key,
  type         text not null,                -- 'provision_resource', ...
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
  action      text not null,                 -- 'resource.create', 'quota.breach', ...
  detail      jsonb,
  created_at  timestamptz not null default now()
);
```

**Notes**

- Billing tables (subscriptions, invoices, payment customer ids) live in the cloud-only billing package, not in this core schema, so self-hosted installs never carry them.
- A worker claims a job with `update jobs set status='running', locked_at=now() where id = (select id from jobs where status='queued' and run_at <= now() order by run_at for update skip locked limit 1) returning *`. Jobs stuck in `running` past a timeout are re-queued.
- Host choice is a query over `resources` grouped by `host_id`, weighted by `hosts.weight`, filtered to `status = 'active'`.

For storage, a `hosts` row represents a bucket (endpoint, bucket name, encrypted parent credentials), and each resource's `external_name` holds its folder prefix, built from the resource id, for example `r/<resource_id>/`. Production uses Cloudflare R2 and local development uses MinIO. Native S3 access uses short-lived credentials scoped to the bucket and prefix, issued on request, because R2 has no long-lived credentials scoped to a prefix.

## Backend plan

The backend is a TypeScript monorepo on Node (current LTS) with two processes sharing one core library: an HTTP API built on Hono, and a worker that does everything slow or scheduled. All SQL is hand-written. There is no ORM and no query builder.

**Stack**

| Concern | Choice | Why |
| --- | --- | --- |
| Runtime and language | Node LTS, TypeScript (strict) | Your stack; types catch shape errors without an ORM |
| HTTP | Hono with `@hono/node-server` | Small, fast, typed routes, runs anywhere |
| Postgres driver | `postgres` (postgres.js) | Tagged-template SQL, automatic parameterization, no ORM layer |
| Redis driver | `ioredis` | Mature, supports ACL commands and Lua |
| Validation | Zod via `@hono/zod-validator` | Request and response shapes in one place |
| Migrations | Numbered `.sql` files plus a \~50-line runner | Forward-only, no framework to learn |
| Passwords and keys | argon2id; SHA-256 for API keys | API keys are high-entropy, so a fast hash is fine |
| Logging and metrics | pino, `prom-client` | Structured logs and Prometheus metrics |
| Tests | Vitest plus real Postgres and Redis in containers | Mocks hide the bugs that matter here |
| Object storage admin | AWS SDK v3 S3 client, MinIO admin API | One client for any S3-compatible backend |

**Repository layout**

```text
sparkbase/
  apps/
    api/          Hono app: routes, auth middleware, rate limits
    worker/       job loop: provisioning, quotas, pausing, backups
    dashboard/    web UI (after the API is stable)
  packages/
    core/         domain logic; no HTTP, no SQL strings
    db/           migrations/*.sql, queries/*.ts, migration runner
    drivers/      postgres.ts, redis.ts, storage.ts provisioners
    sdk/          @sparkbase/sdk (published, permissive license)
    cli/          sparkbase command line (published)
    billing/      interface + no-op; cloud implementation is private
  deploy/         docker-compose.yml, host bootstrap scripts
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

**The driver contract.** Every resource kind implements one interface. The API and worker only ever talk to this interface, which is what keeps adding MongoDB or a vector engine cheap.

```ts
interface ResourceDriver {
  kind: 'postgres' | 'redis' | 'storage'
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

**Provisioning a Postgres resource.** The API inserts a `resources` row in `provisioning` and enqueues a job. The worker then runs these steps, each idempotent and recorded, so a crash or retry never creates duplicates:

1. Pick the least-loaded active Postgres host.
2. Generate a role name and a random password; `CREATE ROLE ... LOGIN CONNECTION LIMIT n`.
3. `CREATE DATABASE ... OWNER role` (outside a transaction), then `REVOKE ALL ON DATABASE ... FROM PUBLIC`.
4. Apply per-role settings: `statement_timeout`, `idle_in_transaction_session_timeout`, `work_mem`.
5. Enable allowlisted extensions requested by the plan.
6. Register the role with the pooler and point the project hostname at the host (DNS CNAME).
7. Store the encrypted password, set `status = 'active'`, write an audit row.

**Background jobs**

| Job | Trigger | Does |
| --- | --- | --- |
| `provision_resource`, `deprovision_resource` | API call | Create or tear down on a host |
| `sample_usage` | Every minute | Calls `measureUsage` on active resources, inserts samples |
| `enforce_quotas` | After each sample run | Warns, locks or unlocks writes by plan limits |
| `pause_inactive` | Hourly | Pauses idle free resources, sends warnings |
| `backup_resource` | Nightly | Per-database dump to object storage |
| `move_resource` | Admin or scheduler | Copy to another host, switch DNS, retire old copy |
| `send_email` | Any | Quota warnings, pause notices, receipts |

**API auth and limits.** Dashboard sessions use httpOnly cookies. Programmatic access uses personal or project-scoped API keys in the form `sb_live_<random>`, stored as a hash and shown once. Rate limits are sliding windows in Redis, keyed by API key or IP, with the limit set by plan.

**Configuration** is environment variables only: `DATABASE_URL`, `REDIS_URL`, `ENCRYPTION_KEY`, `PUBLIC_URL`, `MODE` (`cloud` or `selfhost`), `SMTP_URL`, and per-host settings stored in the `hosts` table.

**Testing.** Unit tests for `core` with no I/O. Integration tests run the API against real Postgres and Redis containers. Each driver has a contract test suite run against the real engine, covering provision, isolation (tenant A cannot read tenant B), limits enforced, pause and resume, and deprovision. The isolation tests are the most important tests in the repository.

## API reference

The API has two layers. The **management API** creates and configures projects and resources. The **data API** gives HTTP access to the resources themselves, for serverless and edge runtimes where a raw TCP connection is awkward. Anything the data API can do, a native driver can do too.

**Conventions**

- Base URL `https://api.sparkbase.io/v1`. JSON in and out. IDs are UUIDs.
- Auth: `Authorization: Bearer sb_live_...` (API key) or a dashboard session cookie.
- List endpoints use cursor pagination: `?limit=50&cursor=...`.
- `POST` creates accept an `Idempotency-Key` header.
- Errors share one shape: `{ "error": { "code": "quota_exceeded", "message": "...", "details": {} } }`.
- Rate limit headers on every response: `X-RateLimit-Limit`, `-Remaining`, `-Reset`.

**Account and keys**

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/auth/signup` | Create an account (email + password), send verification email |
| POST | `/auth/login` | Start a session |
| POST | `/auth/logout` | End the session |
| POST | `/auth/verify-email` | Confirm the emailed token |
| POST | `/auth/password/reset` | Request or complete a password reset |
| GET | `/me` | Current user |
| PATCH | `/me` | Update name, email, password |
| GET | `/api-keys` | List keys (prefix only, never the secret) |
| POST | `/api-keys` | Create a key, optionally scoped to one project; secret returned once |
| DELETE | `/api-keys/{id}` | Revoke a key |

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
| POST | `/projects/{projectId}/resources` | Create a resource (`kind`, `name`, `plan`); returns `202` with status `provisioning` |
| GET | `/resources/{id}` | Detail and current status |
| PATCH | `/resources/{id}` | Rename or change plan (downgrade refused if usage exceeds the new limits) |
| DELETE | `/resources/{id}` | Delete (async) |
| POST | `/resources/{id}/pause` | Pause |
| POST | `/resources/{id}/resume` | Resume |
| GET | `/resources/{id}/connection` | Connection string and parts (host, port, user, password, database) |
| POST | `/resources/{id}/credentials/rotate` | Issue new credentials; old ones revoked after a grace period |
| GET | `/resources/{id}/usage?from=&to=&metric=` | Usage samples against plan limits |
| GET | `/resources/{id}/backups` | List backups |
| POST | `/resources/{id}/backups` | Take a backup now |
| POST | `/resources/{id}/backups/{backupId}/restore` | Restore (to the same or a new resource) |

**Data API: Postgres**

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/resources/{id}/postgres/query` | Run one parameterized statement: `{ "sql": "...", "params": [] }` |
| POST | `/resources/{id}/postgres/transaction` | Run several statements atomically |
| GET | `/resources/{id}/postgres/schema` | Tables, columns and indexes for the dashboard and SDK |

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
| POST | `/resources/{id}/storage/presign-download` | Presigned GET URL, optionally expiring |
| DELETE | `/resources/{id}/storage/objects/{key}` | Delete an object |

**Phase 2 and later**

| Area | Endpoints (sketch) |
| --- | --- |
| Vector | `POST /resources/{id}/vector/collections`, `POST .../upsert`, `POST .../query` |
| NoSQL | `POST /resources/{id}/documents/{collection}/find`, `.../insert`, `.../update` |
| Webhooks | `GET/POST /projects/{projectId}/webhooks` for quota, status and usage events |
| Auth, realtime, queues | Designed with each service in Phase 3 |

**Operational**

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/health` | Liveness for the API |
| GET | `/status` | Public platform status (hosts healthy, queue depth) |

**Example: create a resource**

```http
POST /v1/projects/8f1c.../resources
Authorization: Bearer sb_live_...
Content-Type: application/json

{ "kind": "postgres", "name": "main", "plan": "postgres-free" }
```

```json
{
  "id": "2b7e...",
  "kind": "postgres",
  "name": "main",
  "plan": "postgres-free",
  "status": "provisioning",
  "endpoint": "a1b2c3.db.sparkbase.io"
}
```

Poll `GET /resources/{id}` until `status` is `active`, or subscribe to the `resource.active` webhook.

**Error codes**

| Code | HTTP | Meaning |
| --- | --- | --- |
| `unauthorized` | 401 | Missing or invalid key |
| `forbidden` | 403 | Key scope does not cover this project or action |
| `not_found` | 404 | No such project or resource |
| `plan_limit_reached` | 402 | Free-tier resource cap reached; upgrade or delete one |
| `quota_exceeded` | 429 | A plan limit (storage, memory, bandwidth, requests) is hit |
| `resource_not_ready` | 409 | Resource is provisioning, paused or suspended |
| `downgrade_blocked` | 409 | Usage exceeds the target plan's limits |
| `validation_failed` | 422 | Bad request body |

## SDK, CLI and connection strings

The SDK is a convenience layer on top of native access. It manages resources, wraps the data API, and hands back connection strings for anyone who wants to use their own driver. TypeScript ships first, then Python and Go.

```ts
import { Sparkbase } from '@sparkbase/sdk'

const sb = new Sparkbase({
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
await sb.resources.create({ kind: 'cache', name: 'sessions', plan: 'cache-free' })
```

**SDK rules**

- Zero required dependencies in the core package; adapters (for example a `pg` or `ioredis` helper) are optional.
- Works in Node, Bun, Deno and edge runtimes, since it uses `fetch`.
- Every method maps one-to-one to a documented endpoint. No hidden behavior.
- Typed errors that carry the API error `code` (`QuotaExceededError`, `ResourceNotReadyError`).
- Permissive license (MIT or Apache-2.0) so adoption is frictionless.

**CLI**

| Command | Does |
| --- | --- |
| `sparkbase login` | Authenticate and store a key locally |
| `sparkbase init` | Create or link a project in the current folder |
| `sparkbase resources add postgres --plan free` | Add a resource |
| `sparkbase resources list` | Show resources and status |
| `sparkbase env pull` | Write connection strings into `.env` |
| `sparkbase usage` | Show usage against limits |
| `sparkbase host add --kind postgres` | Self-hosters: register another data host |

`env pull` is the main onboarding hook: a new project goes from zero to a working `DATABASE_URL` in one command.

**Connection strings**

| Resource | Format |
| --- | --- |
| Postgres (pooled, default) | `postgres://sb_x1y2:PASSWORD@a1b2c3.db.sparkbase.io:6432/main?sslmode=require` |
| Postgres (direct, for migrations) | `postgres://sb_x1y2:PASSWORD@a1b2c3.db.sparkbase.io:5432/main?sslmode=require` |
| Cache | `rediss://sb_x1y2:PASSWORD@a1b2c3.cache.sparkbase.io:6380` |
| Object storage | Data API and presigned URLs, or temporary S3 credentials (access key, secret, session token) scoped to your folder; the API returns the endpoint and bucket |

Pooled connections run in transaction mode, so session-level features (advisory locks, `LISTEN/NOTIFY`, some prepared statements) need the direct endpoint, which has a lower connection limit. The docs must say this on the first page a Postgres user reads.

## Pricing and plans

Billing is per resource, not per project. A project is free to create, each resource carries its own Free, Starter or Pro plan, and the monthly bill is the sum of what is attached. The prices and limits below are **draft starting points**, not researched numbers. Set them after you know your real cost per host (see unit economics).

**Postgres**

| Limit | Free | Starter | Pro |
| --- | --- | --- | --- |
| Price per month (USD, draft) | 0 | 7 | 25 |
| Storage | 500 MB | 5 GB | 25 GB |
| Pooled connections | 20 | 60 | 200 |
| Direct connections | 3 | 10 | 25 |
| Data API requests per month | 50k | 1M | 10M |
| Backups kept | None | 7 days | 14 days |

**Cache**

| Limit | Free | Starter | Pro |
| --- | --- | --- | --- |
| Price per month (USD, draft) | 0 | 5 | 15 |
| Memory | 25 MB | 256 MB | 1 GB |
| Connections | 10 | 50 | 200 |
| Data API requests per month | 50k | 1M | 10M |

**Object storage**

| Limit | Free | Starter | Pro |
| --- | --- | --- | --- |
| Price per month (USD, draft) | 0 | 5 | 20 |
| Storage | 1 GB | 50 GB | 250 GB |
| Egress through the data API per month | 5 GB | 100 GB | 500 GB |
| Data API requests per month | 100k | 5M | 50M |

**Rules that make the model work**

- Hard caps, no overage billing. Hitting a limit blocks writes or requests with a clear error and an upgrade prompt, so a small developer is never surprised by a bill.
- Upgrades apply immediately and are prorated. Downgrades are refused until usage fits the lower plan.
- A project-level bundle discount can come later, if customers ask for it.
- Self-hosted installs have no price: plans are admin-defined or unlimited.

**Free tier policy**

- Up to 3 free resources per account, across any kinds.
- Email verification required before provisioning.
- Auto-pause after 7 days of inactivity; deletion after 60 days paused (see tenancy).
- Signup abuse controls: disposable-email blocking, per-IP signup limits, and a review flag on accounts that hit limits unusually fast.
- The free tier is both the acquisition channel and the largest cost. Keep it tight enough that real production use feels cramped.

**Unit economics.** Cost per tenant is the host cost divided by the tenants it carries, adjusted for how oversold the host is. Free databases are mostly idle, so one host can carry hundreds of them. An illustrative case: a server costing about $200 per month that carries 40 paid Starter databases ($280 revenue) and 300 mostly idle free ones covers its own cost. Treat that as a hypothesis to test: measure real CPU, IO, memory and disk per tenant during private beta, then set overselling ratios and prices from the data.

**Payments.** Pick a provider that can pay out to your country. Evaluate a merchant of record (Paddle, Lemon Squeezy, Polar), which handles international sales tax, against Stripe and regional gateways where Stripe is unavailable. Billing sits behind an interface in the `billing` package: plans, subscriptions and invoices are cloud-only, and the self-hosted build uses a no-op implementation.

## Open source and self-hosting

The project is open source and self-hostable, and the hosted cloud is paid for the operating, not the code: managed hosts, backups, upgrades, scaling, support and uptime. Self-hosters get the whole product with no feature gating.

**Licensing (recommended)**

| Part | License | Reason |
| --- | --- | --- |
| Control plane, worker, drivers, dashboard | AGPL-3.0 | Real open source, and a cloud provider that resells it must publish its changes |
| SDKs and CLI | MIT or Apache-2.0 | Nothing discourages putting them in a closed-source app |
| Cloud billing and hosted ops tooling | Private | Sits behind the billing interface, not needed to self-host |

BSL or a similar source-available license protects more strongly, but it is not OSI open source and some developers object to it. Decide the license and the contribution policy (DCO is lighter, a CLA keeps dual licensing possible) before the first outside pull request, because changing either later is painful.

**What self-hosting looks like**

- **Single host:** `docker compose up` starts the API, worker, dashboard, a control-plane Postgres, Redis, and bundled data services (a Postgres host with PgBouncer, a Redis host, MinIO). A new install is usable in minutes.
- **Multiple hosts:** `sparkbase host add --kind postgres --addr ...` registers extra data hosts, and the same scheduler spreads projects across them. Multi-host support is part of the open-source core, since it is how the cloud itself runs.
- **No cloud assumptions:** configuration is environment variables, TLS can use any certificate source, and nothing is hardcoded to a vendor.
- **Billing:** `MODE=selfhost` loads the no-op billing package. Plans are admin-defined or unlimited.
- **Upgrades:** semantic versions, Docker images on a public registry, forward-only SQL migrations run at startup, and upgrade notes in every release.

**Open core line.** If you ever hold something back, keep it to features that matter only to large organizations: SSO, audit-log export, multi-region. Gating core features behind an enterprise edition costs trust that is hard to rebuild.

**Repository hygiene for launch:** README with a 60-second demo, `CONTRIBUTING.md`, a security policy with a private reporting address, issue templates, a public roadmap, and a changelog.

## Security, backups and operations

A shared-tenancy platform is trusted only as far as its isolation holds. Treat a cross-tenant leak as the one failure that ends the company, and invest accordingly.

**Security baseline**

- TLS everywhere: certificates per project hostname, `sslmode=require` on Postgres, `rediss://` for the cache, HTTPS only for the API and storage.
- Credentials encrypted at rest with AES-256-GCM; the key comes from the environment or a KMS and never sits in the database. API keys and passwords are stored only as hashes.
- Tenant roles are never superusers and cannot create roles or databases. Data hosts accept connections only through the pooler and public hostnames, behind a firewall that allows nothing else.
- Credential rotation per resource, with a grace period during which both old and new credentials work.
- Audit log for every create, delete, rotate, plan change and quota event.
- Rate limits on signup, login and every API key. Account lockout after repeated failed logins.
- Dependency scanning in CI, a private security contact, and a published disclosure policy.
- Abuse handling: usage anomaly alerts (sudden egress spikes, connection floods, crypto-mining patterns), a suspend action in the admin tools, and an acceptable-use policy.

**Backups**

| Resource | Method | Retention | Restore test |
| --- | --- | --- | --- |
| Postgres | Nightly per-database logical dump to object storage, encrypted | By plan (none, 7 days, 14 days) | Automated weekly restore of a random database |
| Postgres hosts | Continuous WAL archiving (pgBackRest or WAL-G) for host-level disaster recovery | 7 days | Quarterly full host restore drill |
| Cache | None on Free and Starter; optional snapshot on Pro | Latest | On request |
| Object storage | Provider durability first; versioning or replication added in Phase 2 | n/a | n/a |

A backup that has never been restored is a guess. The weekly automated restore test is part of the MVP, not a later improvement.

**Single points of failure.** One large Postgres host is the riskiest part of the plan. Before public launch, add a streaming replica per host with a documented manual failover, and move to automatic failover (Patroni or similar) once paying customers depend on it.

**Moving a project between hosts.** The stable per-project hostname makes this routine, and it is the same operation as upgrading a project to a dedicated instance.

1. Restore a copy on the target host from a fresh dump (or use logical replication for large databases).
2. Briefly lock writes on the source and apply the final changes.
3. Verify row counts and a checksum sample.
4. Switch the DNS CNAME (kept at a 60-second TTL) and update the pooler.
5. Keep the source read-only for a few days, then drop it.

**Observability**

- Metrics (Prometheus): per-host CPU, IO wait, disk, connections, replication lag; per-resource usage versus limit; API latency and error rates; job queue depth and failure counts.
- Logs: structured JSON from the API and worker, shipped to one searchable store.
- Alerts: host disk above 80%, pooler saturation, replication lag, provisioning failures, backups missing, jobs stuck.
- A public status page, which also gives customers somewhere to look before they email you.

**Runbooks to write before launch:** add a host, drain a host, move a resource, restore a database, rotate the master encryption key, respond to a suspected cross-tenant incident, handle an abuse report.

**Compliance basics:** terms of service, privacy policy, a data deletion process (deleting a resource really deletes it, and the docs say when backups age out), and a clear statement of where each region's data is stored.

## Roadmap

The roadmap is six stages, and each ends at a gate with a check you can run. The first gate, isolation, is the one that decides whether the rest is worth building.

&#91;embedded content: roadmap · 6 stages, 5 gates\]

The stages carry no dates because pace depends on how many hours a week you can give this. Estimate each stage after Stage 0, when you know your real speed, and keep the gates fixed even if the dates move. "Phase 2" and "Phase 3" in the product and API tables mean waves of services after the MVP, and both are delivered in Stage 5.

**MVP scope**

| In the MVP | Not in the MVP |
| --- | --- |
| Postgres, cache, object storage | MongoDB, vector, MySQL |
| Shared tenancy on a small host pool | Dedicated instances per project |
| Free, Starter and Pro per resource, hard caps | Overage billing, project bundles, organizations |
| Native connection strings, data API, TypeScript SDK, CLI | Other SDK languages, webhooks |
| Backups with automated restore tests | Multi-region, automatic failover |
| Single-host self-hosting with `docker compose up` | Self-hosted multi-host guides and UI polish |

## Marketing and go-to-market

Sparkbase wins by being the fastest way to get exactly the data services a backend needs, and by proving it in public. The strategy is developer-led: open source for trust, a generous but tight free tier for adoption, and content that shows the product working in under two minutes.

**Core message.** Your backend needs a database and a cache, not a platform. Attach only what you need, pay only for that, and connect with the tools you already use.

| Pillar | Proof the visitor can check |
| --- | --- |
| Only what you need | Pricing page lists per-resource plans; a project with one Postgres shows one line item |
| Native, not locked in | Standard connection strings; works with any driver or ORM; export is a normal dump |
| Open and self-hostable | GitHub repo, `docker compose up`, same code as the cloud |
| No surprise bills | Hard caps, documented. No overage charges, ever |
| Fast to start | `sparkbase env pull` gives a working `DATABASE_URL` in under two minutes |

Tagline candidates to test: "The data layer for your backend." / "Only the services you need." / "Databases, caches and storage. Pay for what you attach." Run the options on the landing page with a simple split test before committing.

**Audience segments, in order of priority**

1. **Indie developers and students** build many small projects, are price-sensitive, and share tools widely. The free tier and the open-source repo are for them. They are the early word of mouth.
2. **Freelancers and agencies** run many small client projects. Per-resource pricing and one dashboard across projects is a direct fit, and they convert to paid first.
3. **Small startups** that outgrew a free tier but are not ready for a cloud console. They bring the revenue.
4. **Self-hosters** adopt the open-source build, file issues, contribute code and sometimes become cloud customers.
5. **AI builders** need a vector store beside relational data. This segment opens up when the vector resource ships.

**Competitive positioning (general positioning, not benchmarks)**

| Alternative | What they are | Sparkbase angle |
| --- | --- | --- |
| Supabase, Firebase | Bundled backend platforms | You get the data services only, with no platform to adopt, and a bill without bundled extras |
| Managed Postgres or Redis hosts | One service per vendor | One project and one dashboard across several services |
| Cloud consoles (AWS, GCP) | Everything, with high complexity | Provision in seconds; limits and prices a single developer can read |
| Self-managed on a VPS | Cheap but all the ops is yours | Same open-source stack with backups, pooling and quotas already done |

Before publishing any comparison page, check the competitor's current pricing and features directly. Never publish a claim you cannot source.

**Channels**

- **GitHub.** The repo is the storefront. A clear README, a short demo GIF, a "good first issue" label and a public roadmap. Stars and forks are social proof for everything else.
- **Build in public.** Post the work as it happens: the isolation tests, the provisioning flow, the first real tenant. Tie it to your existing 100 Days of Backend series, which already gives you an audience of backend learners, and publish this project as one of its tracks.
- **Campus and student communities.** Start where you already are. A university with many developers is a ready beta group: run a workshop ("build an API with a database, a cache and file uploads in an hour"), give student accounts Pro credits, and recruit student ambassadors.
- **Technical content.** Tutorials that solve one problem and end with Sparkbase in the stack: Hono, Express, NestJS, Next.js, FastAPI and Go starters; "add Redis caching to your API"; "file uploads without S3 complexity". Documentation is marketing, so write it as the best onboarding in the category.
- **Templates and starters.** One-click starter repos with `.env` wiring already done. Each one is also a search-engine entry point.
- **Launch platforms.** Hacker News (Show HN), Product Hunt, Reddit's developer communities, and dev.to or Hashnode. Launch only after private beta has fixed the first round of problems.
- **Hackathons and communities.** Sponsor local and online hackathons with free Pro credits and a prepared starter template. Offer a Discord for support.
- **SEO.** Pages for high-intent searches such as "free Postgres database", "free Redis hosting", "self-host Postgres with pooling", and migration guides from competing services.

**Activation funnel.** Optimize one thing: how fast a new user makes a successful first connection.

| Stage | Event to track | What to watch |
| --- | --- | --- |
| Visit | Landing or docs page view | Source and conversion to signup |
| Signup | Verified email | Drop-off at verification |
| Project | First project created | Time from signup |
| Resource | First resource created | Share who add one, and which kind |
| Connected | First successful connection or API call | Time from signup. Goal: under 2 minutes |
| Retained | Active again after day 7 | Free-tier resources still in use |
| Paid | First paid plan | Which limit triggered the upgrade |

Set numeric targets after the first 100 signups, once there is real data to anchor them.

**Launch plan, keyed to product gates rather than dates**

1. **While building (Stages 0 to 2).** Landing page with a waitlist and one clear demo. Weekly build-in-public posts. Open the repo early with a good README. Collect 100 to 200 waitlist signups.
2. **Private beta (Stage 3).** Invite 50 to 100 people from the waitlist, campus and the series audience. Give them Pro credits in exchange for weekly feedback. Track activation and every support question. Fix the top three friction points before moving on.
3. **Public launch (Stage 4).** Publish docs, templates, comparison pages and a launch post. Show HN and Product Hunt on the same week. Be online all day to answer questions.
4. **After launch.** Weekly changelog, monthly "what we shipped" post, a public roadmap with voting, and the first case studies from beta users.

**Metrics that matter**

- Activation: share of signups with a successful first connection within 10 minutes.
- Weekly active resources, split by free and paid.
- Free to paid conversion and the limit that triggered it.
- Support load per 100 resources, a proxy for how good the docs and errors are.
- GitHub stars, self-hosted installs (opt-in telemetry only), and contributors.
- Gross margin per host, which tells you if the pricing works.

**Budget.** The plan is deliberately cheap: time, content and community before paid acquisition. Spend money first on infrastructure reliability, then on credits for early users, and only later on paid ads or sponsorships, once activation and retention are proven.

**Brand and name checks.** Before spending on branding, confirm the `sparkbase` name is clear in trademark searches, the domain, GitHub organization, npm scope and social handles, and decide how it relates to the earlier SparkDB name so the two do not confuse people.

## Risks and open questions

The biggest risks are a cross-tenant data leak and a solo builder trying to ship too much. Both are manageable if the MVP stays small and the isolation tests come first.

**Risks**

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Cross-tenant data access | Fatal to trust | Per-resource roles and credentials, `REVOKE ... FROM PUBLIC`, isolation contract tests in CI, an external review before public launch |
| Noisy neighbors on shared hosts | Slow or failing tenants | Connection and timeout limits, per-host monitoring, move heavy tenants to another host, dedicated instances later |
| Shared Redis eviction and memory | One tenant degrades others | Reconsider a process per project for the cache |
| One large Postgres host fails | All tenants down | Streaming replica and tested failover before launch; WAL archiving; status page |
| Free-tier cost and abuse | Margin loss, reputation damage | Tight limits, verification, auto-pause and deletion, abuse alerts, admin suspend |
| Scope too wide for one builder | Nothing ships | MVP is Postgres, cache and storage only; everything else waits for the gates |
| Cloud providers resell the code | Lost revenue | AGPL core; sell the operating, not the code |
| Bundled platforms add "pick what you need" modes | Differentiation weakens | Lead on per-resource pricing, native access and self-hosting, not on a single feature |
| Payments and tax | Cannot collect or comply | Choose a merchant of record or a provider that can pay out to you before beta |
| Name collision | Rebrand cost | Trademark, domain and package-name checks now |
| Data loss | Fatal | Backups restored automatically every week; documented retention |

**Open questions to decide**

- [ ] How does Sparkbase relate to SparkDB: rebrand, successor, or separate? Is any code, brand or audience reused?
- [ ] Redis: shared instance with ACL prefixes (current default), or a small process per project?
- [ ] License: AGPL-3.0 for the core with permissive SDKs, or another choice? DCO or CLA?
- [ ] Where do the data hosts run (dedicated servers, cloud VMs), in which regions, and what is the monthly budget?
- [ ] Payment provider and legal entity for billing and tax.
- [ ] Does MySQL or MariaDB come in Phase 2, or wait until customers ask?
- [ ] Which SDK languages after TypeScript: Python, Go?
- [ ] Does the vector resource start as pgvector on the Postgres hosts, or as a separate engine?
- [ ] Dashboard: build custom, or start from an admin template to save time?
- [ ] Final prices, after measuring real cost per tenant in private beta.

* [ ] Object storage access on R2: keep short-lived prefix-scoped credentials plus the data API and presigned URLs, or build an S3 gateway so tenants get long-lived keys?
* [ ] Storage egress and request limits: traffic straight to R2 cannot be metered, so drop those limits from the plans or route traffic through the data API?

**Sources.** This spec is a plan built from the decisions in our discussion and general engineering practice. It cites no external data. Prices, limits and the unit-economics example are drafts to be validated, and competitor descriptions are general positioning that should be checked against current public information before they appear in marketing.
