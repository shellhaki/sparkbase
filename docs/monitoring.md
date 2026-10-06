---
title: Monitoring
description: Uptime checks for your endpoints, with alerts when they go down.
---

A monitor service checks URLs on an interval and records status code and latency.

```ts
await sb.monitor('uptime').create({ name: 'api', url: 'https://myapp.com/health', intervalSeconds: 60, expectStatus: 200 })
```

The dashboard shows the state, uptime over 24 hours and 7 days, latency and the last 60 checks. When a check goes down or comes back, the project owner gets a notification (and an email with SMTP), and an optional **webhook** receives:

```json
{ "check": "api", "url": "https://myapp.com/health", "state": "down", "at": "2026-10-06T12:00:00Z", "error": "expected 200, got 503" }
```

For the health of Sparkbase's own services, see [Observability](observability).
