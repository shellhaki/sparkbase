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

```ts
import { createClient } from '@sparkbase/sdk'

const sb = createClient(process.env.SPARKBASE_URL, process.env.SPARKBASE_SECRET_KEY)
const { rows } = await sb.postgres('main').query('select * from todos')
await sb.queue('jobs').queue('emails').send({ to: 'ada@example.com' })
```

No paid tier, no hosted edition, no feature gating.

## Run it locally

Needs Go, Bun, Postgres 17+ and Redis installed (for example `brew install go bun postgresql@18 redis`). No Docker.

```bash
make dev        # backend on :8787 + dashboard on :3000, Ctrl+C stops both
```

Or separately, in two terminals:

```bash
make backend    # API + worker, starts private Postgres and Redis
make frontend   # dashboard
```

`make stop` stops the private Postgres and Redis.

First run: the backend prints a setup token. Open http://localhost:3000/setup, paste it and create the admin account.

## Repository

| Path | What |
| --- | --- |
| `server/` | Go: API, worker and drivers (`cmd/sparkbase-server`) and the CLI (`cmd/sparkbase`) |
| `web/dashboard/` | Next.js dashboard on Bun |
| `sdk/js/` | `@sparkbase/sdk` |
| `scripts/` | Dev services, dev runner, test helpers |
| `spec.md`, `agent.md`, `DECISIONS.md` | Product spec, build guide, decision log |
| `waitlist/` | Landing page (separate app) |

## Tests

```bash
scripts/dev-services.sh start
cd server && make test                                   # Go unit + integration, incl. isolation suites
cd sdk/js && ../../scripts/with-server.sh bun test       # SDK against a live server
scripts/with-server.sh scripts/e2e-dashboard.sh          # dashboard smoke test
```
