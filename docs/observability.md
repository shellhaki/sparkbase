---
title: Observability
description: Live logs, metrics and terminals for every service, and server logs for admins.
---

## Live logs

Every service has a **Logs** tab that streams as it happens:

- **API calls**: every project API request to the service, with status, duration and error code. Platform services log their events here too: cron runs, check results, backup runs.
- **Container**: the database or cache process's own output (connections, slow statements over one second, errors). Logs survive sleep; the tail reconnects when the container wakes.

Admins also get **Admin → Server logs**, a live view of the server itself. Secrets (passwords, keys, tokens, connection strings) are removed from every log.

The API: `GET /v1/resources/{id}/logs?follow=1&source=api|container` (Server-Sent Events), `GET /v1/admin/logs?follow=1`.

## Metrics

The **Metrics** tab shows the last hour: requests and errors per minute, average latency, and for container services memory and CPU, plus storage use. `GET /v1/resources/{id}/metrics?minutes=60` returns the same data. Prometheus metrics for the whole install are at `/metrics` on the internal network.

## Terminals

Postgres and cache services have a **Terminal** tab: a small psql (`\dt`, `\d TABLE`, `\dx`) and a Redis console. Both run as the service's own user, so they can do what your app can do and no more.

## Health

Admin → Health shows every data host, queued and failed jobs. Usage warnings at 80% and 90% and limit breaches arrive as notifications.
