# Sparkbase

Open-source, self-hosted backend services. Create a project, add only the services your app needs, and use them from one SDK with a project URL and key, or natively with a connection string.

| Service | What you get |
| --- | --- |
| Postgres | A database with its own role. Native `postgres://` URL plus SQL over HTTP |
| Cache | A Redis namespace with its own ACL user. Native `redis://` plus a key/value API |
| Storage | A private bucket. Upload, download, list and signed URLs |
| Vector | Collections of embeddings with similarity search (pgvector, with a fallback) |
| Queues | Durable queues with visibility timeouts, retries and dead letters |
| Auth | Email and password sign-in for your app's users, with JWT sessions |
| Realtime | Broadcast channels and live table changes over Server-Sent Events |
| Cron | Scheduled jobs that call a URL, run SQL or send a queue message |
| Backups | Scheduled database dumps to this server or any S3 bucket |
| Monitor | Uptime checks with alerts and a webhook |

Databases and caches run in their own containers with a memory limit and **scale to zero** when idle; the next connection wakes them in under a second. Every service has live logs and metrics, and databases and caches have a terminal.

```ts
import { createClient } from '@sparkbase/sdk'

const sb = createClient(process.env.SPARKBASE_URL, process.env.SPARKBASE_SECRET_KEY)
const { rows } = await sb.postgres('main').query('select * from todos')
await sb.queue('jobs').queue('emails').send({ to: 'ada@example.com' })
```

No paid tier, no hosted edition, no feature gating.

## Install on a server

```bash
git clone https://github.com/shellhaki/sparkbase && cd sparkbase/deploy
cp .env.example .env && ./generate-secrets.sh   # set SPARKBASE_DOMAIN, TLS_MODE, ACME_EMAIL
docker compose up -d
```

Needs Docker: each database and cache runs as its own container. Open `https://app.<your domain>/setup`: the first account is the admin. Full guide: [docs/self-hosting.md](docs/self-hosting.md), and all docs at `/docs` in the dashboard.

## Run it locally

Needs Go, Bun, Postgres 17+ and Redis installed (for example `brew install go bun postgresql@18 redis`). No Docker: local development uses shared hosts instead of containers.

```bash
make dev        # backend on :8787 + dashboard on :3000, Ctrl+C stops both
```

Or separately, in two terminals:

```bash
make backend    # API + worker, starts private Postgres and Redis
make frontend   # dashboard
```

`make stop` stops the private Postgres and Redis.

First run: open http://localhost:3000/setup and create your account. The first account is the admin; everyone else joins by invite.

## Repository

| Path | What |
| --- | --- |
| `server/` | Go: API, worker, container engine and drivers (`cmd/sparkbase-server`) and the CLI (`cmd/sparkbase`) |
| `apps/dashboard/` | Next.js dashboard on Bun, including the `/docs` site |
| `packages/sdk-js/` | `@sparkbase/sdk` |
| `scripts/` | Dev services, dev runner, test helpers |
| `deploy/` | Docker install: compose, Traefik, Postgres host, Garage |
| `docs/` | Documentation (Markdown plus `nav.json`) |
| `examples/` | Example apps |
| `spec.md`, `agent.md`, `DECISIONS.md` | Product spec, build guide, decision log |
| `waitlist/` | Landing page (separate app) |

## Tests

```bash
scripts/dev-services.sh start
cd server && make test                                   # Go unit + integration, incl. isolation suites
cd packages/sdk-js && ../../scripts/with-server.sh bun test       # SDK against a live server
scripts/with-server.sh scripts/e2e-dashboard.sh          # dashboard smoke test
scripts/e2e-compose.sh                                   # full Docker install through Traefik
```

## License

Server, drivers and dashboard: AGPL-3.0. SDK and CLI: MIT. See [CONTRIBUTING.md](CONTRIBUTING.md).
