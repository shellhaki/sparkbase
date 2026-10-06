# Sparkbase: Agent Build Guide

Oct 6, 2026 · @Excel

A step by step guide for an AI coding agent to build Sparkbase in order, with a check at the end of every step.

## Rules for the agent

The companion document, **Sparkbase: Product & Technical Spec** (`spec.md`), is the source of truth for the data model, the API, the driver contract, the services and their limits. This guide says in what order to build and how to know each step is done. When the two disagree, the spec wins; record the conflict in `DECISIONS.md`.

Sparkbase is open source and self-hosted only. No billing, no pricing, no hosted edition, no feature gating.

**How to work**

1. **Build in order.** Stages 0 to 3 now; Stage 4 (Docker, Traefik install, release) after. Do not build Stage 5 items.
2. **One step at a time.** Finish a step, run its checks, commit, move on. A step is done when its **Done when** line is true and tests pass.
3. **Keep going.** Do not stop at stage checks to ask for approval. Record choices in `DECISIONS.md` and continue. Stop only for something destructive outside the repository.
4. **Small commits.** One logical change per commit. Plain commit messages with no co-author or tool attribution lines.
5. **Tests are part of the step.** Integration tests run against real Postgres and Redis started by `scripts/dev-services.sh`, never mocks of those engines.
6. **The dashboard grows with the backend.** When a stage adds an API, add its screen.

**Hard rules**

- No ORM and no query builder. Hand-written, parameterized SQL in small query functions.
- Never log passwords, keys, tokens or connection strings (the first-run setup token is the one allowed exception).
- Credentials encrypted with AES-256-GCM. Passwords argon2id; account keys, session tokens and one-time tokens stored only as SHA-256 hashes.
- Email and password sign-in for dashboard users and the project auth service (argon2id). Nothing may depend on SMTP being configured.
- Every service has its own role, credentials and namespace. The project API runs as the service's own credentials.
- Every long operation is a job, and every job step is idempotent.
- Traefik is networking only. Nothing in the API or worker depends on it being present.
- No vendor lock: no cloud, DNS or S3 provider is required.

## Setup and conventions

| Concern | Choice |
| --- | --- |
| Backend | Go, one module in `server/` |
| HTTP | `net/http` with method and path patterns |
| Postgres | `jackc/pgx/v5` |
| Redis | `redis/go-redis/v9` |
| Logging | `log/slog` JSON with a redaction helper |
| Tests | `go test`, integration tests tagged by environment (`SPARKBASE_TEST_ENV`) |
| Dashboard | Next.js App Router on Bun, Tailwind, same palette as `waitlist/` |
| SDK | TypeScript in `sdk/js`, built and tested with Bun |
| Package manager | Bun for every JavaScript package. No pnpm, no npm workspaces |

**Local environment.** `scripts/dev-services.sh start` runs private Postgres clusters (control on 54329, data host on 54330) and a private Redis (63799, ACLs on) from local binaries under `.dev/`. No Docker. `scripts/dev.sh` runs the server with the dev environment, and `cd web/dashboard && bun dev` runs the UI.

## Stage 0: Foundations

**0.1 Scaffold.** Go module, `Makefile` (`make test`, `make run`, `make lint` with `go vet`), dev services script, `.env.example`, `DECISIONS.md`. *Done when:* `make test` passes on a clean clone with dev services running.

**0.2 Config and logging.** Env validation with a clear message for each missing variable; slog JSON; request ids; redaction. *Done when:* tests prove missing variables fail and secrets are redacted.

**0.3 Database and migrations.** pgx pool, embedded migrations, `schema_migrations`, seeds for plans (every kind) and settings. *Done when:* migrating twice changes nothing and a test checks every table.

**0.4 Crypto.** AES-GCM, argon2id, tokens, account keys `sb_`, project keys `sbp_`/`sbs_`. *Done when:* round trips, tamper and wrong-key tests pass.

**0.5 First-run setup.** Setup token printed once, `GET/POST /v1/setup`. *Done when:* setup works once, wrong token refused, `setup_closed` after.

**0.6 Accounts, sessions, invites.** Email and password: setup, login with lockout, invites with password, open signup mode, password reset by email and by admin-made link, password change, logout, `/v1/me`, admin users and settings, logging mailer. *Done when:* integration tests cover setup to admin to invite to member sign-in, lockout, resets being single use, a disabled user refused, and identical responses for known and unknown emails.

**0.7 Account keys, middleware, rate limits.** *Done when:* valid, revoked, expired and wrong-project keys and 429 with headers are tested.

**0.8 Projects, project URL and keys, plans.** Project creation issues a publishable and a secret key; keys endpoint and rotation. *Done when:* ownership is enforced and rotated keys stop working.

**0.9 Jobs and worker.** `SKIP LOCKED` claim, backoff, stuck requeue, graceful shutdown, periodic schedules. *Done when:* two workers never take one job, failures retry then fail, stuck jobs return.

**0.10 Hosts and scheduler.** Admin host endpoints, bootstrap hosts from env, weighted least-loaded choice, host bootstrap that revokes `PUBLIC` connect. *Done when:* draining hosts are skipped and bootstrap is idempotent.

**0.11 Dashboard shell.** Next.js on Bun: setup, sign-in, password reset, invite acceptance, project list, project page with URL and keys, admin users and invites.

## Stage 1: Database service

**1.1 Postgres driver.** Provision, deprovision, pause, resume, rotate, limits, usage, write lock, with `provision_state`.

**1.2 Service endpoints.** Create (202), list, get, rename, plan change, delete, pause, resume, retry, connection, rotate, usage; per-user cap.

**1.3 Project API for Postgres.** Query, transaction, schema, running as the service role with timeouts, row caps and size caps.

**1.4 Isolation tests.** Tenant A cannot connect to B's database, `postgres`, `template1` or the control DB; cannot create roles or databases; cannot read server files; forbidden extensions refused; connection limit and statement timeout enforced; write lock blocks writes; paused services refuse logins; rotated-out credentials refused after grace. *Done when:* all pass and weakening one protection fails its check.

**1.5 Dashboard.** Add service, service status, connection panel, SQL runner, table browser.

## Stage 2: More services, SDK and CLI

**2.1 Cache.** ACL user per service with prefix, denied commands, memory and client limits, project API kv and batch, isolation tests.

**2.2 Storage.** Local backend (directory per service, HMAC signed URLs, quota) tested end to end; Garage backend (admin API, SigV4 presign) behind the same interface. Isolation tests: path traversal, other service's objects, quota.

**2.3 Vector.** Collections, upsert, query (cosine, l2, inner product), delete, metadata filter; pgvector when present, `real[]` fallback otherwise.

**2.4 Queues.** Create queues, send with delay, receive with visibility timeout, ack, nack, dead letters, stats.

**2.5 Auth.** Email and password sign-up and sign-in, recovery by link or code, optional passwordless link or code, refresh with rotation and reuse detection, logout, user get and update, admin users and generate-link, settings; HS256 JWTs; one auth service per project; user limit.

**2.6 Request metering.** Redis counters per service, monthly limit, flush to `usage_samples`.

**2.7 SDK.** `createClient(url, key)` with every service, typed errors, auth session handling. Tests run against a live local server.

**2.8 CLI.** Go binary: login, projects, link, services, env pull, usage, admin commands.

**2.9 Dashboard.** A page per service kind: cache browser, file browser with upload, vector collections, queue stats, auth users.

## Stage 3: Operations

**3.1 Usage and limits.** `sample_usage`, `enforce_limits`, warnings at 80% and 90%, write lock at 100%, notifications.

**3.2 Backups.** `pg_dump` per Postgres-family service into backup storage, list, take now, restore into the same service.

**3.3 Admin and audit.** Plans, hosts, suspend and unsuspend, audit viewer, jobs viewer, `/v1/status`, metrics.

**3.4 Dashboard.** Usage bars, notifications, admin health page.

## Stage 4: Release (after Stage 3)

Docker images, `deploy/docker-compose.yml` with Traefik, PgBouncer, pgvector and Garage, bring-your-own-Traefik profile, end-to-end install test, docs site, license files (AGPL-3.0 core, MIT SDK and CLI), release pipeline.

## What not to build

Billing or paid tiers, a hosted edition, MongoDB, MySQL, realtime, row-level end-user policies, organizations, Kubernetes, default-on telemetry, and anything in `waitlist/`.

## When you are unsure

Prefer the smaller, safer option. Prefer the spec over your own idea. Prefer a test that proves isolation over a feature that adds scope. Write the choice in `DECISIONS.md` and keep going.
