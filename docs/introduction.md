---
title: Introduction
description: Sparkbase is an open-source, self-hosted backend. Add the services your app needs to a project and use them from one SDK.
---

Sparkbase runs on your own server. You create a **project**, add only the **services** your app needs, and call them with the project URL and a key, or connect natively with a regular driver.

| Data services | Platform services |
| --- | --- |
| Postgres, cache (Redis), storage (S3), vector, queues, auth | Realtime, cron, backups, monitoring |

```ts
import { createClient } from '@sparkbase/sdk'

const sb = createClient(process.env.SPARKBASE_URL!, process.env.SPARKBASE_SECRET_KEY!)
const { rows } = await sb.postgres('main').query('select * from todos')
await sb.realtime('live').broadcast('public:todos', { refresh: true })
```

## Why Sparkbase

- **Pick your services.** A project starts empty. Each service you add gets its own credentials, limits, logs and metrics.
- **Memory only while in use.** Databases and caches run in their own containers and stop when idle. The next connection or API call starts them again in well under a second.
- **One URL, one key.** Every service is reachable over HTTPS from the project URL. Postgres, Redis and S3 also give you native connection strings.
- **Yours.** Open source (AGPL-3.0 server, MIT SDK and CLI), no paid tier, no telemetry.

## Next steps

- [Quickstart](quickstart): a project, a database and a query in five minutes.
- [Self-hosting](self-hosting): install Sparkbase on a server.
- [Concepts](concepts): projects, services, keys and plans.
