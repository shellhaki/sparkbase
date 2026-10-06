# Sparkbase: Agent Build Guide

Oct 6, 2026 · @Excel

A step by step guide for an AI coding agent to build Sparkbase, one stage at a time, with a check at the end of every step.

## Rules for the agent

Read this section first and follow it for the whole build. The companion document, **Sparkbase: Product & Technical Spec** (`spec.md`), is the source of truth for the database schema, the API endpoints, the driver contract, the networking and Traefik design, the limits table and the seeded plans. This guide says in what order to build things and how to know each step is done. When the two disagree, the spec wins; record the conflict in `DECISIONS.md` and carry on.

Sparkbase is open source and self-hosted only. There is no billing, no pricing, no hosted cloud edition and no feature gating. Never add code paths for any of them.

**How to work**

1. **Build in order.** Stages run 0 to 4. Do not start a stage until the previous gate has passed. Do not build anything from Stage 5 (MongoDB, vector, MySQL, auth, realtime, queues).
2. **One step at a time.** Finish a step, run its checks, commit, then move on. A step is done only when its **Done when** line is true and every test passes.
3. **Small commits.** One logical change per commit, with a clear message. Never leave the main branch broken.
4. **Tests are part of the step.** Write them with the code, not after. Integration tests run against real Postgres, Redis and Garage containers, never mocks of those engines.
5. **Record decisions.** When the spec leaves something open, pick the spec's stated default, write one line in `DECISIONS.md` (date, choice, reason), and continue. Stop and ask only for decisions that are hard to reverse, such as adding a new bundled service.
6. **Stop at every gate.** When a gate's checks pass, write a short report (what was built, what was tested, anything deferred) and wait for approval before starting the next stage.
7. **Ask before adding scope.** If something seems missing from the spec, note it in `DECISIONS.md` as a proposal. Do not build it.

**Hard rules**

- No ORM and no query builder. All SQL is hand-written, owned by small query functions, and parameterized. Never build SQL by joining user input into a string.
- Never log passwords, API keys, session tokens, connection strings, setup or invite tokens, or full emails. Never return a stored secret from any endpoint except the one that is meant to show it.
- Credentials are encrypted at rest with AES-256-GCM. API keys, session tokens and passwords are stored only as hashes.
- Every tenant resource has its own role, credentials and namespace. Tenants never share a credential.
- Every long operation is a job. Every job step is idempotent, so a retry can never create a duplicate.
- The control plane is never on the data path for native connections. Native traffic goes through Traefik to the data hosts.
- Only Traefik publishes ports. Every other service stays on internal Docker networks.
- Nothing is hardcoded to a vendor: no cloud provider, DNS provider or S3 provider is required.

**Definition of done for the whole project:** all five gates pass, the isolation test suite is green in CI, a new self-hoster can follow the docs on a clean server with a domain, run `docker compose up -d`, finish first-run setup and connect to a working Postgres resource over TLS without help, and every public endpoint in the spec exists, is documented and has a test.

## Setup and conventions

Decide these once, at the start, and keep to them in every stage.

**Stack**

| Concern | Choice |
| --- | --- |
| Language and runtime | TypeScript (strict) on current Node LTS |
| HTTP | Hono with `@hono/node-server` |
| Postgres driver | `postgres` (postgres.js), tagged-template SQL, no ORM |
| Redis driver | `ioredis` |
| Validation | Zod, via `@hono/zod-validator` |
| Object storage | Garage admin API over `fetch`; AWS SDK v3 S3 client for data access and presigning |
| Edge proxy | Traefik v3 |
| Logging and metrics | `pino`, `prom-client` |
| Passwords and keys | argon2id for passwords, SHA-256 for API keys and tokens |
| Tests | Vitest, with Postgres, Redis and Garage in containers |
| Package manager | pnpm workspaces |
| Dashboard | Next.js (App Router) that uses only the public API |

**Repository layout**

```text
sparkbase/
  apps/
    api/          Hono app: routes, auth middleware, rate limits, Traefik config endpoint
    worker/       job loop: provisioning, limits, backups, email
    dashboard/    web UI, built on the public API only
  packages/
    core/         domain logic; no HTTP, no SQL strings
    db/           migrations/*.sql, queries/*.ts, migration runner
    drivers/      postgres.ts, cache.ts, storage.ts
    sdk/          @sparkbase/sdk
    cli/          sparkbase command line, including admin commands
  deploy/
    docker-compose.yml       the self-hosting install
    docker-compose.dev.yml   local development
    traefik/                 static config, entrypoints, cert resolvers
    hosts/                   bootstrap for extra data hosts
  docs/           documentation site content
  waitlist/       landing page (separate app; not part of the build stages)
  DECISIONS.md
```

**Coding standards**

- Each query is a small exported function that owns its SQL and returns a typed row. Callers never see SQL strings.
- Routes are thin: validate input, call `core`, shape the response. No business logic in route handlers.
- Every endpoint validates its input and output with Zod, and returns the shared error shape from the spec: `{ "error": { "code", "message", "details" } }`.
- Migrations are numbered `.sql` files, forward-only, applied by a small runner that records them in a `schema_migrations` table. The API runs them at startup.
- Configuration comes only from environment variables, validated at startup. Fail fast with a clear message if one is missing.
- IDs are UUIDs. Timestamps are `timestamptz` in UTC.
- Resource kinds are `postgres`, `cache` and `storage` everywhere.

**Local environment.** `deploy/docker-compose.dev.yml` starts everything the build needs on a laptop: the control-plane Postgres, Redis, a Postgres data host with PgBouncer, a Redis data host, Garage and Traefik with a local certificate for `*.localhost` names. `pnpm dev` runs the API, worker and dashboard against them. `pnpm test` runs the full suite against the same containers.

**Environment variables.** As listed in the spec's configuration table: `SPARKBASE_DOMAIN`, `DATABASE_URL`, `REDIS_URL`, `ENCRYPTION_KEY`, `TRAEFIK_CONFIG_TOKEN`, `ACME_EMAIL`, `TLS_MODE`, `SMTP_URL` (optional; without it, emails are logged and links are shown to the admin). Provide complete `.env.example` files for development and for `deploy/`.

## Stage 0: Foundations

Goal: a running control plane with first-run setup, accounts, invites, API keys, projects, a job runner, a host registry and an isolation test harness. No tenant resources yet.

**0.1 Scaffold the monorepo**

- Create the pnpm workspace and the layout above, with TypeScript strict, ESLint, Prettier and Vitest. Leave `waitlist/` untouched and outside the workspace.
- Add `deploy/docker-compose.dev.yml` with the control-plane Postgres, Redis and Garage. Add CI that runs lint, type checks and tests.
- *Done when:* `pnpm install && pnpm lint && pnpm test` passes on a clean clone, and CI is green.

**0.2 Config and logging**

- Validate environment variables with Zod at startup. Add structured `pino` logging with request ids and a redaction list for secrets.
- *Done when:* starting the API with a missing variable fails with a clear message, and a test proves secrets are redacted from logs.

**0.3 Database package and first migration**

- Create the shared postgres.js client, the migration runner and `0001_init.sql` exactly as written in the spec's data model. The API runs pending migrations at startup.
- Seed the `plans` table with the Small, Medium and Large plans for each kind, using the limits in the spec, with Small as each kind's default. Seed `settings` with `signup_mode = 'invite'`, auto-pause off and a per-user resource cap of 10.
- *Done when:* `pnpm db:migrate` builds the schema on an empty database, running it twice changes nothing, and a test checks that every table exists.

**0.4 Crypto helpers**

- Implement AES-256-GCM encrypt and decrypt for stored secrets, argon2id hashing for passwords, token generation and SHA-256 hashing for sessions, invites and resets, and API key generation (`sb_` plus a random secret, with an 8 character prefix stored for display and a SHA-256 hash stored for lookup).
- *Done when:* unit tests cover round trips, tampered ciphertext failing to decrypt, wrong key failing, and a generated key verifying only against its own hash.

**0.5 First-run setup**

- On startup with no users, generate a one-time setup token, store its hash, and print it once in the API logs (this is the one log line allowed to contain a secret, and it says so). Build `GET /setup` and `POST /setup`, which creates the first admin and closes setup for good.
- *Done when:* tests show setup works once with the right token, refuses a wrong token, returns `setup_closed` after the first user exists, and is rate limited.

**0.6 Accounts, sessions and invites**

- Build `POST /auth/signup` (invite acceptance, or open signup when the setting allows it), `/auth/login`, `/auth/logout`, `/auth/verify-email`, `/auth/password/reset`, `GET /me` and `PATCH /me`. Sessions use httpOnly, secure, same-site cookies backed by the `sessions` table.
- Build the admin user endpoints: `GET /admin/users`, `POST /admin/invites` (returns the invite link, and emails it when SMTP is set), `DELETE /admin/invites/{id}`, `PATCH /admin/users/{id}`, and `GET/PATCH /admin/settings`.
- Email goes through an interface with an SMTP implementation and a logging implementation used when `SMTP_URL` is unset.
- Lock an account after repeated failed logins. Never reveal whether an email is registered.
- *Done when:* integration tests cover setup to admin to invite to member signup, signup refused without an invite in invite mode, open signup when enabled, login, logout, password reset, lockout, a disabled user being refused, a member refused on admin routes, and identical responses for known and unknown emails.

**0.7 API keys, auth middleware and rate limits**

- Build `GET/POST/DELETE /api-keys`. The secret is returned once, at creation. Add middleware that accepts a Bearer key or a session cookie and attaches the user, role and key scope to the request.
- Add a Redis sliding-window rate limiter keyed by API key or IP, with `X-RateLimit-*` headers.
- *Done when:* tests cover a valid key, a revoked key, an expired key, a key scoped to another project (403), and the 429 response with its headers.

**0.8 Projects and plans**

- Build the project endpoints from the spec, including slug generation, `GET /plans`, and the admin plan endpoints. Deleting a project queues a job and sets its status to `deleting`.
- *Done when:* a user can create, list, rename and delete projects, and cannot see or change another user's project; an admin can create and edit plans and a member cannot.

**0.9 Job queue and worker**

- Implement the `jobs` table flow: claim with `FOR UPDATE SKIP LOCKED`, retries with exponential backoff up to `max_attempts`, a stuck-job requeue after a timeout, and a worker loop with graceful shutdown.
- *Done when:* tests prove two workers never take the same job, a failing job retries then ends in `failed` with its error saved, and a crashed worker's job is picked up again.

**0.10 Host registry and scheduler**

- Build the admin host endpoints and `sparkbase admin host add|list|drain`, which write to the `hosts` table with an encrypted admin secret. Register the bundled data hosts automatically on first start from their configured addresses. Implement the scheduler that picks the least-loaded active host of a kind, weighted by `hosts.weight`.
- *Done when:* tests show new resources spread across hosts by weight, a draining or offline host is never chosen, and bundled hosts are registered exactly once across restarts.

**0.11 Isolation test harness**

- Build the test helper that provisions two tenants (A and B, owned by different users) of any resource kind and runs a list of checks as each one. This harness is how every later driver proves isolation.
- *Done when:* the harness runs in CI with a placeholder driver, and a deliberately broken driver makes it fail.

**Gate 0.** All steps above are done, CI is green, and the isolation harness exists and can fail. Write the gate report and wait for approval.

## Stage 1: Postgres resource

Goal: a user can create a Postgres resource, get a native connection string and connect over TLS through Traefik, with isolation and limits enforced. This stage is the hardest and the most important. Take your time with the isolation tests.

**1.1 Dev data host with a pooler**

- Add a Postgres data host and PgBouncer to the dev compose file. PgBouncer runs in transaction mode and authenticates tenants through `auth_query` against a function on the host, so new roles work without editing config files. Expose a pooled and a direct target on the internal network.
- *Done when:* a role created by hand on the host can connect through both targets, and a role that does not exist cannot.

**1.2 Postgres driver**

- Implement the `ResourceDriver` interface from the spec for Postgres: `provision`, `deprovision`, `pause`, `resume`, `rotateCredentials`, `applyLimits`, `measureUsage` and `setWriteLock`.
- Provisioning follows the seven steps in the spec. Each step is idempotent and its progress is recorded in `provision_state`, so a retry resumes instead of repeating. Remember that `CREATE DATABASE` cannot run inside a transaction.
- Generate role names and passwords randomly. Store the password encrypted in `resource_credentials`.
- *Done when:* a provision job takes a resource from `provisioning` to `active`, running it twice creates nothing extra, and deprovision removes the database and role completely.

**1.3 Traefik routing and TLS**

- Add Traefik to the dev compose file with TCP entrypoints for direct Postgres (5432) and pooled Postgres (6432), and a local certificate for `*.db.localhost`.
- Build `GET /internal/traefik/config`: it returns Traefik dynamic configuration with one TCP router per active Postgres resource (`HostSNI` on its endpoint, TLS on, routed to its host's direct or pooler target). It is reachable only on the internal network and requires `TRAEFIK_CONFIG_TOKEN`. Configure Traefik's HTTP provider to poll it.
- *Done when:* a provisioned resource is reachable at `<slug>.db.localhost` on both ports with `sslmode=require`, a connection without TLS is refused, a paused resource's router disappears within one poll interval, and the endpoint refuses requests without the token.

**1.4 Resource endpoints**

- Build the resource endpoints from the spec: create (returns `202` with status `provisioning`), list, get, update, delete, pause, resume, `GET /resources/{id}/connection`, and credential rotation with a grace period during which old and new credentials both work.
- Enforce the per-user resource cap from settings at creation, returning `resource_limit_reached`. Admins are exempt.
- *Done when:* tests cover the full create, poll, connect, rotate and delete flow, the `resource_limit_reached` error, and `resource_not_ready` while provisioning.

**1.5 Limits and usage**

- Apply per-role limits at provisioning: `CONNECTION LIMIT` (the plan's direct connections), `statement_timeout`, `idle_in_transaction_session_timeout` and `work_mem`, plus PgBouncer per-database pool limits for pooled connections.
- Build the `sample_usage` job (database size and connections, every 5 minutes) and `enforce_limits`: warn at 80% and 90%, flip `default_transaction_read_only` at 100%, and lift it when usage drops or the plan changes. Add `GET /resources/{id}/usage`.
- *Done when:* a test fills a database past its limit and sees writes blocked, then deletes data and sees writes allowed again, with an audit row and a notification at each step.

**1.6 Pause, resume, suspend and plan changes**

- Implement pause (`NOLOGIN`, existing connections terminated, Traefik router removed) and resume. Implement admin suspend and unsuspend with the same mechanics.
- Implement the optional `pause_inactive` job, which runs only when the admin enables auto-pause in settings.
- Implement plan changes: moving to a larger plan applies at once, moving to a smaller one is refused with `plan_change_blocked` when usage exceeds it. Respect the setting that decides whether members may change plans.
- *Done when:* a paused resource refuses logins and resumes in seconds, a suspended resource cannot be resumed by its owner, auto-pause works in a test with a faked clock and does nothing when disabled, and a blocked plan change returns the right error.

**1.7 Postgres isolation tests (the most important tests in the repository)**

Run each of these through the harness as tenant A against tenant B, through Traefik. Every one must be a failing attempt.

| Check | Expected result |
| --- | --- |
| Connect to B's database with A's credentials | Refused |
| Connect to B's hostname with A's credentials | Refused |
| Connect to a database that does not exist, and to `postgres` or the template databases | Refused |
| List other databases, roles and their settings | Nothing useful from other tenants |
| Create a role or a database | Permission denied |
| Read a server file, write a server file, run a server program | Permission denied |
| Create a forbidden extension (`dblink`, `file_fdw`, `postgres_fdw`, untrusted languages) | Refused |
| Open more direct connections than the limit | Extra connections refused |
| Run a query longer than the statement timeout | Cancelled |
| Write after the limit flip | Refused, reads still work |
| Connect while paused or suspended | Refused |
| Use rotated-out credentials after the grace period | Refused |
| Connect without TLS | Refused |

- *Done when:* all checks pass in CI, and temporarily weakening one setting (for example not revoking `PUBLIC` connect) makes the matching check fail.

**1.8 Minimal CLI**

- Build `sparkbase login --url`, `init`, `resources add`, `resources list` and `env pull`. `env pull` writes the connection strings into `.env` without touching other lines in the file.
- *Done when:* from an empty folder, a new user can run four commands and have a working `DATABASE_URL`.

**1.9 Dashboard basics**

- In `apps/dashboard`, build first-run setup, log in, invite acceptance, the project list, a project page with its resources and their status, a create-resource form, a connection string panel with copy buttons, and an admin page for users and invites. Use only the public API.
- *Done when:* a fresh install's admin can complete setup, invite a member, and the member can sign up, create a project, add a Postgres resource and copy a working connection string, all in the browser.

**Gate 1.** The isolation tests are green, a fresh install goes from `docker compose up` to a TLS connection through Traefik in under ten minutes, and limits, pause, suspend and rotation all work. Write the gate report and wait for approval.

## Stage 2: Cache, storage, data API and SDK

Goal: the other two resources, the HTTP data API over all three, request metering, a first SDK and a complete CLI. Build each new driver behind the same interface used in Stage 1, so the API and worker need no changes.

**2.1 Cache driver**

- Add a Redis data host to the dev compose file and a TLS TCP entrypoint (6380) with `HostSNI` routers for `<slug>.cache.<domain>` in the Traefik config endpoint.
- Implement the driver using the shared-instance default from the spec: one ACL user per resource, restricted to a key prefix, with the spec's dangerous commands denied.
- Measure memory by sampling the resource's keys with `MEMORY USAGE`. At the limit, switch the ACL user to read and delete only until usage drops. Count clients per user and kill the excess.
- Keep the driver swappable. If the open question about a Redis process per resource is decided that way, only this driver should change.
- *Done when:* provision, pause, resume, rotate and deprovision all work, TLS through Traefik is required, and a test proves a tenant can only touch keys under its own prefix.

**2.2 Object storage on Garage: one bucket and one key per resource**

- Add Garage to the dev compose file, initialize its layout, and register it as a `hosts` row of kind `storage` with its admin API token encrypted. Create the private backups bucket and allow no tenant key on it.
- Implement the storage driver against Garage's admin API using `fetch`: `provision` creates bucket `sb-<resource_id>` and an access key, allows the key on that bucket only, and sets the bucket quota from the plan. `rotateCredentials` creates a new key, allows it, and removes the old one after the grace period. `pause` removes the key's permission and `resume` grants it again. `measureUsage` reads bucket size and object count. `deprovision` empties and deletes the bucket and deletes the key.
- Return the endpoint, bucket, access key id and secret from `GET /resources/{id}/connection`.
- Route `s3.<domain>` through Traefik to Garage's S3 API (path-style), so native S3 clients and presigned URLs work from outside the server.
- *Done when:* a tenant can upload, list, download and delete in its own bucket with `aws s3`, `rclone`, the AWS SDK and presigned URLs; Garage refuses writes past the quota; and the tenant's key cannot reach any other bucket or the backups bucket.

**2.3 Isolation tests for cache and storage**

Add these to the harness and run them in CI.

| Resource | Check | Expected result |
| --- | --- | --- |
| Cache | Read or write a key outside the tenant's prefix | Denied |
| Cache | Run `KEYS *`, `FLUSHALL`, `FLUSHDB`, `CONFIG GET *`, any script or function | Denied |
| Cache | Exceed the client limit | Extra clients dropped |
| Cache | Write past the memory limit | Writes blocked, reads and deletes allowed |
| Cache | Connect while paused, or without TLS | Refused |
| Storage | List, read or write another resource's bucket with A's key | Denied |
| Storage | List all buckets, or reach the backups bucket | Nothing from other tenants; backups denied |
| Storage | Create or delete a bucket, or change permissions, with A's key | Denied |
| Storage | Data API object keys such as `../other` or a leading `/` | Rejected |
| Storage | Presigned URL used after it expires, or edited to point at another bucket | Refused |
| Storage | Write past the bucket quota, natively or through the data API | Refused |
| Storage | Use a rotated-out key after the grace period, or any key while paused | Refused |

- *Done when:* all checks pass, and each fails when its matching protection is removed.

**2.4 Data API**

- Build the data endpoints from the spec: Postgres `query`, `transaction` and `schema`; cache `get`, `put`, `delete` and `batch`; storage `list`, `presign-upload`, `presign-download` and `delete`.
- Run every data API call with the resource's own credentials, never with an admin role, so the same limits and isolation apply as for a native connection. Cap request size, response size and execution time.
- Return `resource_not_ready` for paused, suspended or provisioning resources and `quota_exceeded` at a limit.
- *Done when:* each endpoint has tests for success, bad input, a missing resource, a resource owned by someone else, and a paused resource. SQL over HTTP must be unable to escape its own database.

**2.5 Request metering**

- Count data API requests in Redis counters per resource, flush them into `usage_samples`, and enforce the monthly request limits with `quota_exceeded` and a clear message. Unlimited plans skip the check.
- Direct S3 traffic is not metered, as the spec says. Do not build an egress limit.
- *Done when:* a test exceeds a plan's data API request limit and is blocked, and the usage endpoint reports the same numbers.

**2.6 SDK v0.1**

- Build `@sparkbase/sdk` in TypeScript with zero required dependencies, using `fetch` so it runs in Node, Bun, Deno and edge runtimes. It takes the install's API `url`. Every method maps to one documented endpoint. Errors are typed and carry the API error `code`. Include `postgres()`, `cache()`, `storage()`, `resources` and `connectionString()` as shown in the spec.
- *Done when:* the SDK's tests run against a live local API, and the examples in the spec run unchanged.

**2.7 Complete the CLI**

- Add `usage`, `resources delete`, `resources pause` and `resume`, and the admin commands (`admin invite`, `admin users`, `admin host add|list|drain`, `admin plans`).
- *Done when:* every CLI command has a test, and `--help` explains each one.

**2.8 Documentation skeleton**

- Create the docs site structure with an install guide, a quickstart for each resource, the connection string formats, a page on pooled versus direct Postgres connections (transaction mode breaks session features, and that must be on the first page a Postgres user reads), the API reference generated from the Zod schemas, and an error code page.
- *Done when:* a developer who has never seen the project can follow the install guide and the quickstarts and connect to each resource.

**Gate 2.** Isolation tests pass for all three resources through Traefik, limits are enforced and measured on all three, and the SDK and CLI work end to end. Write the gate report and wait for approval.

## Stage 3: Operations

Goal: everything an operator needs to run an install with other people's data on it: tested backups, observability, admin tools, security review and runbooks.

**3.1 Backups and restore tests**

- Add the `backup_resource` job: a nightly logical dump per Postgres database, encrypted and stored in the private backups bucket, kept for the number of days the plan allows, tracked in the `backups` table. Back up the control-plane database the same way. Build the backup list, create and restore endpoints.
- Support a separate backups backend so operators can send backups off-site to any S3-compatible bucket.
- Add the weekly `verify_backups` job that restores a random backup into a scratch database, verifies it, and alerts if it fails.
- *Done when:* a restore test passes in CI, and a deliberately corrupted backup makes the weekly check alert.

**3.2 Observability**

- Expose Prometheus metrics on the internal network: per-host connections, disk and memory, per-resource usage against limit, API latency and error rate, and job queue depth and failures.
- Build `GET /status` and an admin health page in the dashboard showing host health, queue depth, failed jobs, recent limit breaches and certificate problems.
- Add an optional compose profile with Prometheus and Grafana and a ready dashboard.
- *Done when:* each alert condition (host disk above 80%, pooler saturation, provisioning failures, missing backups, stuck jobs) shows on the admin health page and has a test or a documented way to trigger it.

**3.3 Admin tools and audit log**

- Complete the admin endpoints and dashboard pages: users, invites, settings, plans, hosts, suspend and unsuspend, and the audit log viewer.
- Make sure every create, delete, rotate, plan change, pause, suspend, invite, role change and limit event writes an audit row, and that owners are notified of limit warnings, pauses and suspensions.
- *Done when:* a test walks through each lifecycle event and finds its audit row and its notification.

**3.4 Dashboard completion**

- Add usage graphs against plan limits, banners at 80% and 100%, plan change screens (when allowed), credential rotation, backup management and storage keys.
- *Done when:* a member can manage everything from the dashboard without the CLI, and an admin can run the install from the dashboard.

**3.5 Security review**

- Work through the spec's security baseline: TLS everywhere, secret handling, role privileges, network isolation of the data hosts, the setup token, dependency scanning in CI, a security contact and a disclosure policy. Then arrange an external review of the isolation design and tests.
- *Done when:* every item on the baseline is checked off in `SECURITY_CHECKLIST.md` with evidence, and external findings are fixed or consciously accepted.

**3.6 Runbooks**

- Write the runbooks the spec lists. Implement the `move_resource` job from the spec.
- *Done when:* each runbook has been followed once on a dev setup and corrected where it failed.

**Gate 3.** A restored backup works, the admin tools cover every operator task in the runbooks, and the security checklist is complete. Write the gate report and wait for approval.

## Stage 4: First release

Goal: a public release that anyone can self-host, contribute to and learn from.

**4.1 Self-hosting package**

- Build `deploy/docker-compose.yml` for a single host as the spec describes: Traefik, API, worker, dashboard, control-plane Postgres and Redis, a Postgres data host with PgBouncer, a Redis data host and Garage, with named volumes and internal networks. Only Traefik publishes ports.
- Ship Traefik static config with the `acme-dns`, `acme-http` and `custom` TLS modes, and `deploy/generate-secrets.sh` to fill the generated secrets in `.env`.
- Add the compose profile for operators who already run Traefik, with the labels, entrypoints and HTTP provider settings they need to add.
- Document adding more hosts with `sparkbase admin host add` and the `deploy/hosts/` bootstrap.
- *Done when:* in CI, a clean VM with a test domain runs `docker compose up -d`, completes setup through the API, and connects to a Postgres, a cache and a storage resource over TLS; and a second Postgres host can be added by following the guide.

**4.2 Open source release**

- Add the license files (AGPL-3.0 for the core, MIT for `packages/sdk` and `packages/cli`, replacing the current Apache-2.0 `LICENSE`), `CONTRIBUTING.md`, a `SECURITY.md` with a private reporting address, issue and pull request templates, a code of conduct, and the DCO contribution policy with a CI check for `Signed-off-by`.
- *Done when:* the repository passes a checklist review for a first-time contributor, and no secret or private file is in the history.

**4.3 Documentation content**

- Complete the docs: install guide for a fresh VPS, DNS and certificate setup for each TLS mode, the bring-your-own-Traefik guide, quickstarts, guides for Hono, Express, NestJS, Next.js, FastAPI and Go, a migration guide from a plain Postgres, the sizing guide, a limits and errors reference, upgrade instructions, and the runbooks. Every code sample is run in CI.
- *Done when:* every sample in the docs executes without error against a local stack.

**4.4 Starter templates**

- Publish starter repositories with the `.env` wiring already done, each using Postgres, cache and storage in a minimal working app.
- *Done when:* each template runs after `sparkbase env pull` and one start command.

**4.5 Release process**

- Use semantic versions, publish multi-arch Docker images (amd64 and arm64) and npm packages from CI, keep a changelog, and write upgrade notes for every release. Migrations stay forward-only and run at startup.
- *Done when:* tagging a version builds and publishes everything, and a test upgrades a previous version's install, data included, to the new one with `docker compose pull && docker compose up -d`.

**4.6 Release checks**

- Load test the data API and the provisioning path on a small VPS-sized machine, and use the results to write the sizing guide. Confirm backups, the admin health page and the runbooks work on the release build.
- *Done when:* the load test passes without breaking isolation or limit enforcement, and the results are written up.

**Gate 4.** Self-hosting works from a clean server by following the docs alone, the docs are verified, the release pipeline works, and the owner signs off the release checklist.

## What not to build

Do not build any of the following unless the owner asks for it in writing after Gate 4.

- Billing, payments, pricing, paid tiers, a hosted cloud edition, or anything behind a feature flag for a paid version.
- MongoDB, vector, MySQL or MariaDB resources.
- Auth, realtime or queues as Sparkbase services.
- Dedicated instances per resource, multi-region support or automatic failover.
- Organizations or teams with shared projects.
- SDKs other than TypeScript.
- Kubernetes manifests or Helm charts.
- Telemetry that is on by default.
- Anything in `waitlist/`. The landing page is a separate app and is not part of these stages.

## When you are unsure

Prefer the smaller, safer option. Prefer the spec over your own idea. Prefer a test that proves isolation over a feature that adds scope. If a decision is hard to reverse and the spec does not settle it, write the question in `DECISIONS.md`, stop at the end of the current step, and ask.
