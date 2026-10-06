---
title: Postgres
description: A Postgres database with its own role, a native connection string and SQL over HTTP.
---

Each Postgres service is a database owned by its own role. With the container engine (the default) it runs in its own container with a memory limit and sleeps when idle.

## Connect

| How | Where |
| --- | --- |
| Native, direct | `postgres://ROLE:PASSWORD@main-REF.db.example.com:5432/DB?sslmode=require` |
| Native, pooled | same host, port `6432` (PgBouncer on shared hosts; the same as direct with containers) |
| HTTP | `POST {projectUrl}/postgres/main/query` with the secret key |
| SDK | `sb.postgres('main').query(sql, params)` |

Copy the connection string from the service's **Overview** tab or run `sparkbase env pull`.

## SQL over HTTP

```ts
await sb.postgres('main').query('insert into todos (title) values ($1)', ['Ship it'])
const { rows } = await sb.postgres('main').query('select * from todos order by id')
await sb.postgres('main').transaction([
  { sql: 'update accounts set balance = balance - $1 where id = $2', params: [10, 1] },
  { sql: 'update accounts set balance = balance + $1 where id = $2', params: [10, 2] },
])
```

One statement per query; transactions are all or nothing. Statements run as the service's own role, with its statement timeout. At most 10,000 rows come back.

## Add-ons

Allowed extensions: `pgcrypto`, `uuid-ossp`, `pg_trgm`, `vector` (created for you), plus any of these you enable with `create extension`: `citext`, `hstore`, `ltree`, `btree_gin`, `btree_gist`, `unaccent`, `fuzzystrmatch`, `intarray`, `tablefunc`. The project's **Architecture** view shows which ones each database uses.

## Terminal

The **Terminal** tab is a small psql: type SQL, or `\dt`, `\d TABLE`, `\dx`, `\?`.

## Limits

| Limit | What happens |
| --- | --- |
| Storage | Warnings at 80% and 90%; at 100% the database turns read-only until you delete data or change plan |
| Direct connections | New connections are refused |
| Statement timeout | The statement is cancelled |
| Memory (containers) | The container's hard limit; Postgres is tuned to fit it |

> **Pooled or direct?** The pooled port runs PgBouncer in transaction mode on shared hosts: advisory locks, `LISTEN/NOTIFY` and session `SET` need the direct port. Run migrations on the direct port.
