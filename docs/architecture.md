---
title: How Sparkbase works
description: The control plane, the container engine, scale to zero and isolation.
---

## Pieces

| Piece | What it does |
| --- | --- |
| **Server** | One Go binary: the management API (`/v1`), the project API (`/p/{ref}`), the worker (jobs, schedules) and the activator |
| **Control database and Redis** | Users, projects, services, plans, jobs, audit log; rate limits, live logs, metrics |
| **Service containers** | One container per Postgres-family or cache service, with a memory limit |
| **Garage** | Object storage, one bucket and key per storage service |
| **Traefik** | TLS and routing at the edge, nothing else |
| **Dashboard** | Next.js app over the same public API |

## Scale to zero

Databases and caches are the services that hold memory, so each runs in its own container:

1. The container gets the plan's memory limit; Postgres is tuned to fit it.
2. After **idle minutes** (Admin → Settings, default 10) with no connections and no API calls, Sparkbase stops the container. Data stays in its volume.
3. The next native connection reaches the **activator**, which starts the container, waits until it accepts connections (usually under half a second) and then pipes the connection through. API calls wake it the same way.

Paused and suspended services stay stopped and refuse connections. A service with an open connection or a live table subscription is never stopped.

## Isolation

- Every service has its own credentials: its own Postgres roles in its own database and container, its own Redis user limited to a key prefix, its own S3 key allowed on one bucket.
- The project API runs every statement as the service's own role, never as an admin.
- Tenant roles cannot reach other databases, read server files or create roles. The isolation suites in `server/internal/api` prove this in CI.
- Cron jobs and uptime checks cannot reach private networks unless an admin allows it.
