# Decisions

One line per decision: date, choice, reason.

- 2026-10-06: Backend in Go (one module in `server/`), frontend Next.js on Bun, SDK built with Bun. No pnpm. Owner's choice.
- 2026-10-06: Email-only, magic-link authentication everywhere (dashboard users and the project `auth` service). No passwords. Owner's choice.
- 2026-10-06: Without SMTP, magic links and invite links are logged by the server and invite links are returned to the admin; the auth service exposes `generate-link` so apps can send their own email.
- 2026-10-06: Development and tests run without Docker. `scripts/dev-services.sh` starts private Postgres clusters (control and data host) and Redis from local binaries.
- 2026-10-06: Project URL is path based, `{PUBLIC_API_URL}/p/{ref}`, so no extra DNS is needed. Project keys: publishable `sbp_` (auth routes only), secret `sbs_` (everything).
- 2026-10-06: `vector`, `queue` and `auth` services each get their own database and role on a Postgres host, the same isolation unit as `postgres`.
- 2026-10-06: Vector falls back to `real[]` with exact search when the host has no pgvector, so development Postgres works.
- 2026-10-06: Storage has two backends: `local` (directory per service, API-served, HMAC signed URLs) for development and simple installs, and `garage` for native S3.
- 2026-10-06: `members_change_plans` defaults to true (small trusted installs); admins can turn it off.
- 2026-10-06: Native hostnames are `<name>-<ref>.db.<domain>` so a project can have several databases.
