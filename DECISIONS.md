# Decisions

One line per decision: date, choice, reason.

- 2026-10-06: Backend in Go (one module in `server/`), frontend Next.js on Bun, SDK built with Bun. No pnpm. Owner's choice.
- 2026-10-06: Email and password authentication (argon2id) for dashboard users and the project `auth` service. Owner's choice, replacing an earlier magic-link-only plan: self-hosted installs often have no SMTP.
- 2026-10-06: Nothing needs SMTP. Invite links are returned to the admin, admins can create password reset links, and the auth service exposes `generate-link`. With SMTP, the same links are also emailed.
- 2026-10-06: The auth service keeps a passwordless link-and-code flow (`/auth/otp`, `/auth/verify`) as an option; it also powers password recovery.
- 2026-10-06: Development and tests run without Docker. `scripts/dev-services.sh` starts private Postgres clusters (control and data host) and Redis from local binaries.
- 2026-10-06: Project URL is path based, `{PUBLIC_API_URL}/p/{ref}`, so no extra DNS is needed. Project keys: publishable `sbp_` (auth routes only), secret `sbs_` (everything).
- 2026-10-06: `vector`, `queue` and `auth` services each get their own database and role on a Postgres host, the same isolation unit as `postgres`.
- 2026-10-06: Vector falls back to `real[]` with exact search when the host has no pgvector, so development Postgres works.
- 2026-10-06: Storage has two backends: `local` (directory per service, API-served, HMAC signed URLs) for development and simple installs, and `garage` for native S3.
- 2026-10-06: `members_change_plans` defaults to true (small trusted installs); admins can turn it off.
- 2026-10-06: Native hostnames are `<name>-<ref>.db.<domain>` so a project can have several databases.
- 2026-10-06: No setup token. The first account on a fresh install becomes the admin and sign-up closes; everyone else joins by invite. Open sign-up mode removed. Owner's choice; the server prints a reminder to finish setup right after the first start.
- 2026-10-06: Docker install pins Garage v2.4.1 (admin API v2), PgBouncer 1.26 (auth_query through a security-definer function that only returns `sbu_*` tenant logins), pgvector/pgvector:pg18, Traefik v3.7. The server image is based on postgres:18-alpine so backups get a matching pg_dump.
- 2026-10-06: Traefik static config is built by `deploy/traefik/start.sh` from TLS_MODE; HTTPS routes come from a templated file provider, native TCP routes from the API's config endpoint. `/internal` and `/metrics` are not routed publicly.
- 2026-10-06: TLS_MODE gains `selfsigned` (Traefik's default certificate) for testing; native ports are configurable (NATIVE_PG_PORT etc.) so shifted-port installs print correct connection strings.
- 2026-10-06: Encryption key rotation is a one-shot command, `sparkbase-server rotate-encryption-key`, re-encrypting all secrets in one transaction.
- 2026-10-06: Service logos: Postgres and Redis use their simple-icons marks (CC0); storage, vector, queue and auth use drawn marks.
