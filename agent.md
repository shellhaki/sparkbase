# Sparkbase: Agent Build Guide

Oct 5, 2026 · @Excel

A step by step guide for an AI coding agent to build Sparkbase, one stage at a time, with a check at the end of every step.

## Rules for the agent

Read this section first and follow it for the whole build. The companion document, **Sparkbase: Product & Technical Spec**, is the source of truth for the database schema, the API endpoints, the driver contract, the quota table and the plan limits. This guide says in what order to build things and how to know each step is done. When the two disagree, the spec wins; record the conflict in `DECISIONS.md` and carry on.

**How to work**

1. **Build in order.** Stages run 0 to 4. Do not start a stage until the previous gate has passed. Do not build anything from Stage 5 (MongoDB, vector, MySQL, auth, realtime, queues).
2. **One step at a time.** Finish a step, run its checks, commit, then move on. A step is done only when its **Done when** line is true and every test passes.
3. **Small commits.** One logical change per commit, with a clear message. Never leave the main branch broken.
4. **Tests are part of the step.** Write them with the code, not after. Integration tests run against real Postgres, Redis and MinIO containers, never mocks of those engines.
5. **Record decisions.** When the spec leaves something open, pick the spec's stated default, write one line in `DECISIONS.md` (date, choice, reason), and continue. Stop and ask only for decisions that are hard to reverse, such as the license, the payment provider or the hosting provider.
6. **Stop at every gate.** When a gate's checks pass, write a short report (what was built, what was tested, anything deferred) and wait for approval before starting the next stage.
7. **Ask before adding scope.** If something seems missing from the spec, note it in `DECISIONS.md` as a proposal. Do not build it.

**Hard rules**

- No ORM and no query builder. All SQL is hand-written, owned by small query functions, and parameterized. Never build SQL by joining user input into a string.
- Never log passwords, API keys, connection strings, tokens or full emails. Never return a stored secret from any endpoint except the one that is meant to show it once.
- Credentials are encrypted at rest with AES-256-GCM. API keys and passwords are stored only as hashes.
- Every tenant resource has its own role, credentials and namespace. Tenants never share a credential.
- Every long operation is a job. Every job step is idempotent, so a retry can never create a duplicate.
- The control plane is never on the data path for native connections.
- Billing code stays behind the `billing` interface. The core must run with the no-op billing implementation.

**Definition of done for the whole project:** all five gates pass, the isolation test suite is green in CI, a new self-hoster can run `docker compose up` and create a working Postgres resource without help, and every public endpoint in the spec exists, is documented and has a test.

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
| Object storage admin | AWS SDK v3 S3 client. Cloudflare R2 in production, MinIO for local development only |
| Logging and metrics | `pino`, `prom-client` |
| Passwords and keys | argon2id for passwords, SHA-256 for API keys |
| Tests | Vitest, with Postgres, Redis and MinIO in containers |
| Package manager | pnpm workspaces |
| Dashboard | Next.js (App Router) that uses only the public API |

**Repository layout**

```text
sparkbase/
  apps/
    api/          Hono app: routes, auth middleware, rate limits
    worker/       job loop: provisioning, quotas, pausing, backups
    dashboard/    web UI, built on the public API only
  packages/
    core/         domain logic; no HTTP, no SQL strings
    db/           migrations/*.sql, queries/*.ts, migration runner
    drivers/      postgres.ts, redis.ts, storage.ts
    sdk/          @sparkbase/sdk
    cli/          sparkbase command line
    billing/      interface, no-op, and (private) cloud implementation
  deploy/         docker-compose files, host bootstrap scripts
  docs/           documentation site content
  DECISIONS.md
```

**Coding standards**

- Each query is a small exported function that owns its SQL and returns a typed row. Callers never see SQL strings.
- Routes are thin: validate input, call `core`, shape the response. No business logic in route handlers.
- Every endpoint validates its input and output with Zod, and returns the shared error shape from the spec: `{ "error": { "code", "message", "details" } }`.
- Migrations are numbered `.sql` files, forward-only, applied by a small runner that records them in a `schema_migrations` table.
- Configuration comes only from environment variables, validated at startup. Fail fast with a clear message if one is missing.
- IDs are UUIDs. Timestamps are `timestamptz` in UTC.

**Local environment.** `deploy/docker-compose.dev.yml` starts everything the build needs on a laptop: the control-plane Postgres, Redis, a Postgres data host with PgBouncer, a Redis data host, and MinIO as the local stand-in for R2. `pnpm dev` runs the API and worker against them. `pnpm test` runs the full suite against the same containers.

**Environment variables.** `DATABASE_URL`, `REDIS_URL`, `ENCRYPTION_KEY`, `PUBLIC_URL`, `MODE` (`cloud` or `selfhost`), `SMTP_URL` (in development, log emails instead of sending them), plus per-host settings in the `hosts` table. Provide a complete `.env.example`.

**Storage variables.** In production: `R2_ACCOUNT_ID`, plus the bucket names and the encrypted parent credentials, which live in the `hosts` table rather than in the environment. In local development: `S3_ENDPOINT`, `S3_ACCESS_KEY` and `S3_SECRET_KEY` for MinIO. List the development ones in `.env.example`.

## Stage 0: Foundations

Goal: a running control plane with accounts, API keys, projects, a job runner, a host registry and an isolation test harness. No tenant resources yet.

**0.1 Scaffold the monorepo**

- Create the pnpm workspace and the layout above, with TypeScript strict, ESLint, Prettier and Vitest.
- Add `deploy/docker-compose.dev.yml` with the control-plane Postgres, Redis and MinIO. Add CI that runs lint, type checks and tests.
- *Done when:* `pnpm install && pnpm lint && pnpm test` passes on a clean clone, and CI is green.

**0.2 Config and logging**

- Validate environment variables with Zod at startup. Add structured `pino` logging with request ids and a redaction list for secrets.
- *Done when:* starting the API with a missing variable fails with a clear message, and a test proves secrets are redacted from logs.

**0.3 Database package and first migration**

- Create the shared postgres.js client, the migration runner and `0001_init.sql` exactly as written in the spec's data model.
- Seed the `plans` table with the Free, Starter and Pro plans for Postgres, cache and object storage, using the limits in the spec's pricing tables.
- *Done when:* `pnpm db:migrate` builds the schema on an empty database, running it twice changes nothing, and a test checks that every table exists.

**0.4 Crypto helpers**

- Implement AES-256-GCM encrypt and decrypt for stored secrets, argon2id hashing for passwords, and API key generation (`sb_live_` plus a random secret, with an 8 character prefix stored for display and a SHA-256 hash stored for lookup).
- *Done when:* unit tests cover round trips, tampered ciphertext failing to decrypt, wrong key failing, and a generated key verifying only against its own hash.

**0.5 Accounts and sessions**

- Build `POST /auth/signup`, `/auth/login`, `/auth/logout`, `/auth/verify-email`, `/auth/password/reset`, `GET /me` and `PATCH /me`. Sessions use httpOnly, secure, same-site cookies. Email goes through an interface, with a development implementation that logs the message.
- Lock an account after repeated failed logins. Never reveal whether an email is registered.
- *Done when:* integration tests cover signup, verification, login, logout, password reset, lockout, and identical responses for known and unknown emails.

**0.6 API keys, auth middleware and rate limits**

- Build `GET/POST/DELETE /api-keys`. The secret is returned once, at creation. Add middleware that accepts a Bearer key or a session cookie and attaches the user and key scope to the request.
- Add a Redis sliding-window rate limiter keyed by API key or IP, with `X-RateLimit-*` headers.
- *Done when:* tests cover a valid key, a revoked key, an expired key, a key scoped to another project (403), and the 429 response with its headers.

**0.7 Projects**

- Build the project endpoints from the spec, including slug generation, and `GET /plans`. Deleting a project queues a job and sets its status to `deleting`.
- *Done when:* a user can create, list, rename and delete projects, and cannot see or change another user's project.

**0.8 Job queue and worker**

- Implement the `jobs` table flow: claim with `FOR UPDATE SKIP LOCKED`, retries with exponential backoff up to `max_attempts`, a stuck-job requeue after a timeout, and a worker loop with graceful shutdown.
- *Done when:* tests prove two workers never take the same job, a failing job retries then ends in `failed` with its error saved, and a crashed worker's job is picked up again.

**0.9 Host registry and scheduler**

- Add admin commands (`sparkbase-admin host add|list|drain`) that write to the `hosts` table with an encrypted admin secret. Implement the scheduler that picks the least-loaded active host of a kind, weighted by `hosts.weight`.
- *Done when:* tests show new resources spread across hosts by weight, and a draining or offline host is never chosen.

**0.10 Isolation test harness**

- Build the test helper that provisions two tenants (A and B) of any resource kind and runs a list of checks as each one. This harness is how every later driver proves isolation.
- *Done when:* the harness runs in CI with a placeholder driver, and a deliberately broken driver makes it fail.

**Gate 0.** All steps above are done, CI is green, and the isolation harness exists and can fail. Write the gate report and wait for approval.

## Stage 1: Postgres resource

Goal: a developer can create a Postgres resource, get a native connection string and connect, with isolation and quotas enforced. This stage is the hardest and the most important. Take your time with the isolation tests.

**1.1 Dev data host with a pooler**

- Add a Postgres data host and PgBouncer to the dev compose file. PgBouncer runs in transaction mode and authenticates tenants through `auth_query` against a function on the host, so new roles work without editing config files. Expose a pooled port and a direct port.
- *Done when:* a role created by hand on the host can connect through both ports, and a role that does not exist cannot.

**1.2 Postgres driver**

- Implement the `ResourceDriver` interface from the spec for Postgres: `provision`, `deprovision`, `pause`, `resume`, `rotateCredentials`, `applyLimits`, `measureUsage` and `setWriteLock`.
- Provisioning follows the seven steps in the spec. Each step is idempotent and its progress is recorded on the resource, so a retry resumes instead of repeating. Remember that `CREATE DATABASE` cannot run inside a transaction.
- Generate role names and passwords randomly. Store the password encrypted in `resource_credentials`.
- *Done when:* a provision job takes a resource from `provisioning` to `active`, running it twice creates nothing extra, and deprovision removes the database and role completely.

**1.3 Hostnames**

- Put DNS behind a `DnsProvider` interface. The development implementation maps `<slug>.db.localhost` to the local host. Store the stable per-project hostname on the resource.
- *Done when:* the connection string uses the hostname, and changing the target host behind the hostname requires no change to the connection string.

**1.4 Resource endpoints**

- Build the resource endpoints from the spec: create (returns `202` with status `provisioning`), list, get, update, delete, `GET /resources/{id}/connection`, and credential rotation with a grace period during which old and new credentials both work.
- Enforce the free-tier rules at creation: at most 3 free resources per account, and a verified email.
- *Done when:* tests cover the full create, poll, connect, rotate and delete flow, the `plan_limit_reached` error, and `resource_not_ready` while provisioning.

**1.5 Quotas and usage**

- Apply per-role limits at provisioning: `CONNECTION LIMIT`, `statement_timeout`, `idle_in_transaction_session_timeout` and `work_mem`.
- Build the `sample_usage` job (database size and connections, every minute) and `enforce_quotas`: warn at 80% and 90%, flip `default_transaction_read_only` at 100%, and lift it when usage drops or the plan changes. Add `GET /resources/{id}/usage`.
- *Done when:* a test fills a database past its limit and sees writes blocked, then deletes data and sees writes allowed again, with an audit row and a warning email at each step.

**1.6 Pause and resume, plan changes**

- Implement pause (`NOLOGIN`, existing connections terminated) and resume. Build the `pause_inactive` job for free resources idle for 7 days, with the day 5 warning email and deletion after 60 days paused.
- Implement plan changes: upgrades apply at once, downgrades are refused with `downgrade_blocked` when usage exceeds the target plan.
- *Done when:* a paused resource refuses logins and resumes in seconds, an idle free resource is paused on schedule in a test with a faked clock, and a blocked downgrade returns the right error.

**1.7 Postgres isolation tests (the most important tests in the repository)**

Run each of these through the harness as tenant A against tenant B. Every one must be a failing attempt.

| Check | Expected result |
| --- | --- |
| Connect to B's database with A's credentials | Refused |
| Connect to a database that does not exist, and to `postgres` or the template databases | Refused |
| List other databases, roles and their settings | Nothing useful from other tenants |
| Create a role or a database | Permission denied |
| Read a server file, write a server file, run a server program | Permission denied |
| Create a forbidden extension (`dblink`, `file_fdw`, untrusted languages) | Refused |
| Open more connections than the limit | Extra connections refused |
| Run a query longer than the statement timeout | Cancelled |
| Write after the quota flip | Refused, reads still work |
| Connect while paused | Refused |
| Use rotated-out credentials after the grace period | Refused |

- *Done when:* all checks pass in CI, and temporarily weakening one setting (for example not revoking `PUBLIC` connect) makes the matching check fail.

**1.8 Minimal CLI**

- Build `sparkbase login`, `init`, `resources add`, `resources list` and `env pull`. `env pull` writes the connection strings into `.env` without touching other lines in the file.
- *Done when:* from an empty folder, a new user can run four commands and have a working `DATABASE_URL`.

**1.9 Dashboard basics**

- In `apps/dashboard`, build sign up and log in, the project list, a project page with its resources and their status, a create-resource form, and a connection string panel with copy buttons. Use only the public API.
- *Done when:* a new user can sign up, create a project, add a Postgres resource and copy a working connection string, all in the browser.

**Gate 1.** The isolation tests are green, a fresh account goes from sign up to a native connection in under two minutes, and quotas, pause and rotation all work. Write the gate report and wait for approval.

## Stage 2: Cache, storage, data API and SDK

Goal: the other two launch resources, the HTTP data API over all three, usage metering, a first SDK and a complete CLI. Build each new driver behind the same interface used in Stage 1, so the API and worker need no changes.

**2.1 Redis driver**

- Add a Redis data host to the dev compose file. Implement the driver using the shared-instance default from the spec: one ACL user per resource, restricted to a key prefix, with dangerous commands denied (`KEYS`, `FLUSHALL`, `FLUSHDB`, `CONFIG`, `DEBUG`, `SCRIPT` and Lua at first).
- Measure memory by sampling the resource's keys with `MEMORY USAGE`. At the limit, switch the ACL user to read and delete only until usage drops. Count clients per user and kill the excess.
- Keep the driver swappable. If the open question about a Redis process per project is decided that way, only this driver should change.
- *Done when:* provision, pause, resume, rotate and deprovision all work, TLS is required, and a test proves a tenant can only touch keys under its own prefix.

**2.2 Object storage: shared buckets, one folder per resource**

- Build a `StorageBackend` interface with two implementations: MinIO for local development and CI, and Cloudflare R2 for production. Both speak the S3 API through the AWS SDK v3 client, so everything above the backend is identical in development and production. MinIO is only a local stand-in for R2.
- **Isolation model:** tenants share buckets, and each resource owns one folder (a key prefix) inside a bucket. Build the prefix from the immutable resource id, for example `r/<resource_id>/`, never from a name the user chose, and always end it with `/`. Store it in `resources.external_name`.
- **Buckets are storage hosts.** Register each bucket as a `hosts` row of kind `storage` (endpoint, bucket name, encrypted parent credentials) and let the scheduler pick the bucket with the most headroom. Start with one bucket. Adding another is registering another row. Moving a resource to a different bucket is a server-side copy of its prefix followed by a switch of `host_id`.
- Keep a separate private bucket for backups that no tenant prefix can ever reach.
- **Key hygiene.** The platform adds the prefix and the tenant only ever sees keys relative to it. Normalize every key: reject empty keys, a leading `/`, `..` segments, control characters and anything over 1,024 bytes.
- **Two ways for tenants to reach their folder.** (1) The data API and presigned URLs: Sparkbase signs requests with the parent credentials, which never leave the server, and always stays inside the resource's prefix. (2) Optional native S3 access for tools such as `aws s3` and `rclone`: issue temporary credentials scoped to the bucket and the prefix. R2 supports this through its temporary credentials API (bucket, prefixes, permission and a lifetime of up to 7 days, signed from a bucket-scoped parent token). The MinIO backend issues the equivalent through STS with a session policy. Expose it as `POST /resources/{id}/storage/credentials` and `sparkbase storage credentials`, with a default lifetime of 1 hour. Treat issued credentials as impossible to revoke before they expire, which is why the lifetime stays short.
- **Do not build** long-lived per-tenant S3 keys or an S3 gateway in front of R2. R2 has no long-lived credentials scoped to a prefix. If the owner wants them later, record the question in `DECISIONS.md` and wait for a decision.
- **Limits.** R2 gives no per-prefix usage figures, so keep a running size counter updated by data API writes and reconcile it with a periodic paginated listing of the prefix. At the limit, refuse data API writes and stop issuing write-capable temporary credentials. Credentials issued earlier stay valid until they expire, and the short lifetime keeps that overshoot small.
- *Done when:* a tenant can upload, list, download and delete inside its own folder through the data API, presigned URLs and temporary credentials, and cannot reach any other folder or the backups bucket. The same tests pass against MinIO, and a staging run passes against a real R2 test bucket.

**2.3 Isolation tests for cache and storage**

Add these to the harness and run them in CI.

| Resource | Check | Expected result |
| --- | --- | --- |
| Cache | Read or write a key outside the tenant's prefix | Denied |
| Cache | Run `KEYS *`, `FLUSHALL`, `CONFIG GET *`, any Lua script | Denied |
| Cache | Exceed the client limit | Extra clients dropped |
| Cache | Write past the memory limit | Writes blocked, reads and deletes allowed |
| Cache | Connect while paused | Refused |
| Storage | Data API keys such as `../other`, a leading `/`, or another resource's prefix | Rejected |
| Storage | Prefixes of two resources whose ids share leading characters | No overlap |
| Storage | Temporary credentials used on another folder, another bucket or the backups bucket | Denied |
| Storage | Temporary credentials used after they expire | Refused |
| Storage | Presigned URL used after it expires | Refused |
| Storage | Write past the storage limit through the data API | Refused |

- *Done when:* all checks pass, and each fails when its matching protection is removed.

**2.4 Data API**

- Build the data endpoints from the spec: Postgres `query`, `transaction` and `schema`; cache `get`, `put`, `delete` and `batch`; storage `list`, `presign-upload`, `presign-download` and `delete`.
- Run every data API call with the resource's own credentials, never with an admin role, so the same limits and isolation apply as for a native connection. Cap request size, response size and execution time.
- Return `resource_not_ready` for paused or provisioning resources and `quota_exceeded` at a limit.
- *Done when:* each endpoint has tests for success, bad input, a missing resource, a resource owned by someone else, and a paused resource. SQL over HTTP must be unable to escape its own database.

**2.5 Request and bandwidth metering**

- Count data API requests in Redis counters per resource, flush them into `usage_samples`, and enforce the monthly request limits with `quota_exceeded` and a clear message.
- Traffic that goes straight to R2 through presigned URLs or temporary credentials does not pass through Sparkbase, so it cannot be metered or capped. Do not build an egress limit for storage. Record this in `DECISIONS.md` and ask the owner whether the plan tables should drop the storage egress limit. R2 does not charge egress fees, but check current Cloudflare pricing before relying on that.
- *Done when:* a test exceeds the free data API request limit and is blocked, and the usage endpoint reports the same numbers.

**2.6 SDK v0.1**

- Build `@sparkbase/sdk` in TypeScript with zero required dependencies, using `fetch` so it runs in Node, Bun, Deno and edge runtimes. Every method maps to one documented endpoint. Errors are typed and carry the API error `code`. Include `postgres()`, `cache()`, `storage()`, `resources` and `connectionString()` as shown in the spec.
- *Done when:* the SDK's tests run against a live local API, and the examples in the spec run unchanged.

**2.7 Complete the CLI**

- Add `usage`, `resources delete`, `resources pause` and `resume`, and `host add` for self-hosters.
- *Done when:* every CLI command has a test, and `--help` explains each one.

**2.8 Documentation skeleton**

- Create the docs site structure with a quickstart for each resource, the connection string formats, a page on pooled versus direct Postgres connections (transaction mode breaks session features, and that must be on the first page a Postgres user reads), the API reference generated from the Zod schemas, and an error code page.
- *Done when:* a developer who has never seen the project can follow the quickstart and connect to each resource.

**Gate 2.** Isolation tests pass for all three resources, quotas are enforced and measured on all three, and the SDK and CLI work end to end. Write the gate report and wait for approval.

## Stage 3: Beta readiness

Goal: everything needed to let 50 to 100 outside developers use Sparkbase safely: billing, tested backups, a replica, monitoring, abuse controls and a status page.

**3.1 Billing behind an interface**

- Define the `billing` interface in `packages/billing`: create a subscription for a resource, change its plan, cancel it, and report a resource's billing state. Ship a no-op implementation (used when `MODE=selfhost`) and a cloud implementation for the chosen payment provider. The provider is an open decision, so ask which one before writing the cloud implementation. Until then, build and test against a fake provider.
- Billing is per resource. Failed payment suspends the resource (status `suspended`, data kept) after a grace period, and a successful payment restores it.
- *Done when:* the core works with the no-op implementation, the cloud implementation passes the same contract tests against the fake provider, and the plan change and suspension flows have tests.

**3.2 Backups and restore tests**

- Add the `backup_resource` job: a nightly logical dump per Postgres database, encrypted and stored in the separate private backups bucket, kept for the number of days the plan allows. Build the backup list, create and restore endpoints.
- Add a weekly job that restores a random backup into a scratch database and verifies it, and alerts if it fails.
- *Done when:* a restore test passes in CI, and a deliberately corrupted backup makes the weekly check alert.

**3.3 Host level protection**

- Set up continuous WAL archiving (pgBackRest or WAL-G) for the Postgres host, plus a streaming replica with a written manual failover procedure. Provide scripts in `deploy/` and a runbook.
- *Done when:* a replica stays in sync in the dev setup, and following the runbook promotes it and restores service.

**3.4 Observability**

- Expose Prometheus metrics: per-host CPU, IO wait, disk and connections, per-resource usage against limit, API latency and error rate, and job queue depth and failures. Ship structured logs to one place.
- Add alerts: host disk above 80%, pooler saturation, replication lag, provisioning failures, missing backups and stuck jobs. Build `GET /status` and a public status page.
- *Done when:* each alert has a test or a documented way to trigger it, and the status page shows host health and queue depth.

**3.5 Abuse controls**

- Add signup rate limits per IP, disposable email blocking, a review flag for accounts that hit limits unusually fast, anomaly alerts (egress spikes, connection floods), and an admin action that suspends an account or resource.
- *Done when:* tests cover each control, and a suspended resource refuses connections and API calls.

**3.6 Audit log and notifications**

- Make sure every create, delete, rotate, plan change, pause, suspend and quota event writes an audit row. Send the owner emails for quota warnings, pauses, suspensions and receipts.
- *Done when:* a test walks through each lifecycle event and finds its audit row and its email.

**3.7 Dashboard completion**

- Add usage graphs against plan limits, banners with upgrade buttons at 80% and 100%, plan change and billing screens, credential rotation, and backup management.
- *Done when:* a beta user can manage everything from the dashboard without the CLI.

**3.8 Security review**

- Work through the security baseline in the spec: TLS everywhere, secret handling, role privileges, firewall rules, dependency scanning in CI, a security contact and a disclosure policy. Then arrange an external review of the isolation design and tests.
- *Done when:* every item on the baseline is checked off in `SECURITY_CHECKLIST.md` with evidence, and external findings are fixed or consciously accepted.

**3.9 Runbooks**

- Write runbooks for adding a host, draining a host, moving a resource, restoring a database, rotating the master encryption key, responding to a suspected cross-tenant incident, and handling an abuse report. Implement the `move_resource` job from the spec.
- *Done when:* each runbook has been followed once on a dev setup and corrected where it failed.

**Gate 3.** A restored backup works, the replica failover has been exercised, billing and suspension work, and the system has run for the beta period with no cross-tenant incident. Write the gate report and wait for approval.

## Stage 4: Launch readiness

Goal: a public release that outsiders can self-host, contribute to and learn from.

**4.1 Self-hosting package**

- Build `deploy/docker-compose.yml` for a single host: API, worker, dashboard, control-plane Postgres, Redis, a Postgres data host with PgBouncer, a Redis data host and MinIO. Load the no-op billing implementation when `MODE=selfhost`, with admin-defined or unlimited plans. Run migrations automatically at startup.
- Document adding more hosts with `sparkbase host add`, and how TLS certificates and hostnames are set up.
- *Done when:* on a clean machine, `docker compose up` followed by the documented first-run steps produces a working Postgres resource, and a second host can be added by following the guide.

**4.2 Open source release**

- Add the license files (the recommended split is AGPL-3.0 for the core and MIT or Apache-2.0 for the SDK and CLI, but confirm the license with the owner before publishing), `CONTRIBUTING.md`, a `SECURITY.md` with a private reporting address, issue and pull request templates, a code of conduct, and a contribution policy (DCO or CLA, as decided).
- *Done when:* the repository passes a checklist review for a first-time contributor, and no secret or private file is in the history.

**4.3 Documentation content**

- Complete the docs: quickstarts, guides for Hono, Express, NestJS, Next.js, FastAPI and Go, a migration guide from a plain Postgres, the pricing and limits page, a limits and errors reference, and the self-hosting guide. Every code sample is run in CI.
- *Done when:* every sample in the docs executes without error against a local stack.

**4.4 Starter templates**

- Publish starter repositories with the `.env` wiring already done, each using Postgres, cache and storage in a minimal working app.
- *Done when:* each template runs after `sparkbase env pull` and one start command.

**4.5 Release process**

- Use semantic versions, publish Docker images and npm packages from CI, keep a changelog, and write upgrade notes for every release. Migrations stay forward-only.
- *Done when:* tagging a version builds and publishes everything, and a test upgrades a previous version's data to the new one.

**4.6 Launch checks**

- Confirm the status page, backups, alerts and runbooks are live. Load test the data API and the provisioning path at a multiple of the expected beta traffic. Review the free tier limits against measured cost per tenant.
- *Done when:* the load test passes without breaking isolation or quota enforcement, and the results are written up.

**Gate 4.** Self-hosting works from a clean machine, the docs are verified, the release pipeline works, and a launch checklist is signed off by the owner.

## What not to build

Do not build any of the following unless the owner asks for it in writing after Gate 4.

- MongoDB, vector, MySQL or MariaDB resources.
- Auth, realtime or queues as Sparkbase services.
- Dedicated instances per project, multi-region support or automatic failover.
- Overage billing, project bundles, organizations or teams.
- SDKs other than TypeScript.
- Email sending for the newsletter. The marketing landing page and its waitlist are a separate project and are not part of this repository.

## When you are unsure

Prefer the smaller, safer option. Prefer the spec over your own idea. Prefer a test that proves isolation over a feature that adds scope. If a decision is hard to reverse and the spec does not settle it, write the question in `DECISIONS.md`, stop at the end of the current step, and ask.
