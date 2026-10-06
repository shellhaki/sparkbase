---
title: Services overview
description: What every service kind does and how to connect to it.
---

Every service gets its own credentials, limits, live logs and metrics. Services never share a login.

| Data services | Platform services |
| --- | --- |
| [Postgres](postgres), [cache](cache), [storage](storage), [vector](vector), [queues](queues), [auth](auth) | [Realtime](realtime), [cron](cron), [backups](backups), [monitoring](monitoring) |

## Postgres

A database and its own role on a Postgres host.

| | |
| --- | --- |
| Pooled URL (default) | `postgres://ROLE:PASSWORD@main-REF.db.example.com:6432/DB?sslmode=require` |
| Direct URL | same host, port `5432` |
| Project API | `POST /postgres/{name}/query`, `/transaction`, `GET /schema` |

**Pooled or direct?** The pooled port goes through PgBouncer in transaction mode. It scales to many clients but does not keep session state between transactions: advisory locks, `LISTEN/NOTIFY`, `SET` without `LOCAL`, and server-side prepared statements across transactions need the **direct** port. Run migrations on the direct port. Most drivers work on the pooled port; with `pg` or `postgres.js`, turn off prepared statements if you see errors about them.

Allowed extensions: `pgcrypto`, `uuid-ossp`, `pg_trgm`, `vector`. At the storage limit the database turns read-only until data is deleted or the plan changes.

## Cache

A Redis ACL user that can only touch keys under its own prefix (shown in the dashboard, also `REDIS_KEY_PREFIX` from `env pull`).

| | |
| --- | --- |
| URL | `rediss://USER:PASSWORD@sessions-REF.cache.example.com:6380` |
| Project API | `GET/PUT/DELETE /cache/{name}/keys/{key}`, `POST /incr/{key}`, `POST /batch`, `GET /keys?prefix=` |

The project API adds the prefix for you. Native clients must add it themselves. `KEYS`, `SCAN`, `FLUSHALL`, `CONFIG`, scripts and admin commands are denied. Over the memory limit, writes are refused until keys are deleted.

## Storage

A private bucket. On the Docker install it is a Garage bucket with its own S3 key; on a local-disk install it is a directory served by the API.

| | |
| --- | --- |
| S3 (Garage) | endpoint `https://s3.example.com`, region `garage`, path-style, the service's bucket, access key and secret |
| Project API | `GET /storage/{name}/objects`, `PUT/GET/DELETE /objects/{key}`, `POST /sign` |

Signed URLs let anyone `GET` or `PUT` one object until they expire (at most 7 days).

## Vector

Collections of embeddings with similarity search (cosine, L2 or inner product) and JSON metadata filters. Uses pgvector with an HNSW index (up to 2000 dimensions indexed). The data lives in its own Postgres database, so the native URL works with any pgvector client.

## Queues

Durable queues on Postgres. `receive` hides messages for the visibility timeout; `ack` deletes them; `nack` returns them, optionally after a delay. A message received `maxReceives` times moves to the dead letters; `redrive` puts them back.

## Auth

Users for your app: email and password, refresh tokens that rotate on every use (reusing an old one signs that session out everywhere), recovery by email link or 6-digit code, and optional passwordless sign-in. Links only go to the site URL or an allowlisted redirect (Auth → Settings). One auth service per project.
