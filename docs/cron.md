---
title: Cron
description: Scheduled jobs that call a URL, run SQL or enqueue a message.
---

Schedules use the standard five fields in UTC: `minute hour day month weekday`, plus `@hourly`, `@daily`, `@weekly`, `@monthly`.

| Action | Example |
| --- | --- |
| `http` | `{ "type": "http", "method": "POST", "url": "https://myapp.com/api/digest", "body": {"kind": "daily"} }` |
| `sql` | `{ "type": "sql", "postgres": "main", "sql": "delete from sessions where expires_at < now()" }` |
| `queue` | `{ "type": "queue", "queue": "jobs", "queueName": "emails", "body": {"to": "team"} }` |

```ts
await sb.cron('tasks').create({ name: 'cleanup', schedule: '0 3 * * *', action: { type: 'sql', postgres: 'main', sql: 'vacuum analyze' } })
```

Every run is recorded (status and the first part of the output) and appears in the service's live log. **Run** in the dashboard runs a job immediately.

> **Private addresses** HTTP jobs cannot call private network addresses (localhost, 10.x, 192.168.x…) so tenants cannot reach the install's internal services. An admin can allow them in Admin → Settings.
