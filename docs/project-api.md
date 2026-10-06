---
title: Project API
description: HTTP reference for everything under the project URL.
---

Base URL: the project URL, `https://api.example.com/p/<ref>`.

Send the project key in the `apikey` header. For signed-in end users, also send `Authorization: Bearer <access token>`. Errors look like `{ "error": { "code", "message", "details" } }` (see [Errors](errors.md)).

| Key | Can call |
| --- | --- |
| Publishable `sbp_` | Auth public routes only |
| Secret `sbs_` | Everything below |

## Postgres

| Method | Path | Body |
| --- | --- | --- |
| POST | `/postgres/{name}/query` | `{ sql, params?, rowLimit? }` → `{ command, rowCount, fields, rows }` |
| POST | `/postgres/{name}/transaction` | `{ statements: [{ sql, params }] }` → `{ results }`, all or nothing |
| GET | `/postgres/{name}/schema` | → `{ tables: [{ schema, name, columns, indexes }] }` |

One statement per query. Statements run as the service's own role with its timeout. At most 10,000 rows come back (`rowLimit`, default 1,000).

## Cache

| Method | Path | Body |
| --- | --- | --- |
| GET | `/cache/{name}/keys/{key}` | → `{ key, value, ttl }` |
| PUT | `/cache/{name}/keys/{key}` | `{ value, ttl?, nx? }` (value is a string) |
| DELETE | `/cache/{name}/keys/{key}` | → `{ deleted }` |
| POST | `/cache/{name}/incr/{key}` | `{ by?, ttl? }` → `{ value }` |
| POST | `/cache/{name}/batch` | `{ ops: [{ op: get\|set\|del, key, value?, ttl? }] }`, up to 100 |
| GET | `/cache/{name}/keys?prefix=&limit=` | → `{ keys, truncated }` |

## Storage

| Method | Path | Body |
| --- | --- | --- |
| GET | `/storage/{name}/objects?prefix=&after=&limit=` | → `{ objects, truncated, next }` |
| PUT | `/storage/{name}/objects/{key}` | raw bytes; `Content-Type` is kept |
| GET | `/storage/{name}/objects/{key}` | raw bytes; supports `Range` |
| DELETE | `/storage/{name}/objects/{key}` | → `{ deleted }` |
| POST | `/storage/{name}/sign` | `{ key, method?: GET\|PUT, expiresIn? }` → `{ url, expiresAt }` |

Keys are relative paths: no leading `/`, no `.` or `..` segments.

## Vector

| Method | Path | Body |
| --- | --- | --- |
| GET/POST | `/vector/{name}/collections` | create: `{ name, dimension, metric?: cosine\|l2\|inner_product }` |
| DELETE | `/vector/{name}/collections/{c}` | |
| POST | `/vector/{name}/collections/{c}/upsert` | `{ items: [{ id, vector, metadata? }] }`, up to 1,000 |
| POST | `/vector/{name}/collections/{c}/query` | `{ vector, topK?, filter?, includeVectors? }` → `{ matches: [{ id, score, distance, metadata }] }` |
| GET | `/vector/{name}/collections/{c}/items/{id}` | |
| POST | `/vector/{name}/collections/{c}/delete` | `{ ids }` |

## Queues

| Method | Path | Body |
| --- | --- | --- |
| GET/POST | `/queue/{name}/queues` | create: `{ name, visibilityTimeout?, maxReceives? }` |
| DELETE | `/queue/{name}/queues/{q}` | |
| POST | `/queue/{name}/queues/{q}/send` | `{ messages: [{ body, delay? }] }` → `{ ids }` |
| POST | `/queue/{name}/queues/{q}/receive` | `{ max?, visibilityTimeout? }` → `{ messages }` |
| POST | `/queue/{name}/queues/{q}/ack` | `{ ids }` |
| POST | `/queue/{name}/queues/{q}/nack` | `{ ids, delay? }` |
| GET | `/queue/{name}/queues/{q}/stats` | → `{ ready, inFlight, dead, oldestEnqueuedAt }` |
| GET | `/queue/{name}/queues/{q}/dead` | dead letters |
| POST | `/queue/{name}/queues/{q}/redrive`, `/purge` | |

## Auth

| Method | Path | Key | Body |
| --- | --- | --- | --- |
| POST | `/auth/signup` | publishable | `{ email, password, data? }` → `{ session, user }` |
| POST | `/auth/token?grant_type=password` | publishable | `{ email, password }` |
| POST | `/auth/token?grant_type=refresh_token` | publishable | `{ refresh_token }` |
| POST | `/auth/recover` | publishable | `{ email, redirectTo? }` |
| POST | `/auth/otp` | publishable | `{ email, redirectTo?, data? }` |
| POST | `/auth/verify` | publishable | `{ token }` or `{ email, code }` |
| POST | `/auth/logout?scope=global` | publishable + user | |
| GET/PUT | `/auth/user` | publishable + user | update `{ data?, password? }` |
| GET/POST | `/auth/admin/users` | secret | create `{ email, password?, data?, appData? }` |
| GET/PATCH/DELETE | `/auth/admin/users/{id}` | secret | patch `{ data?, appData?, banned? }` |
| POST | `/auth/admin/generate-link` | secret | `{ email, redirectTo? }` → `{ token, code, link }` |
| GET/PATCH | `/auth/admin/settings` | secret | `{ allowSignups, siteUrl, redirectUrls, accessTokenTtl, refreshTokenTtl }` |

A session is `{ access_token, token_type, expires_in, expires_at, refresh_token, user }`.

## Realtime

| Method | Path | Key | Body |
| --- | --- | --- | --- |
| POST | `/realtime/{name}/broadcast` | secret | `{ channel, event?, payload }` |
| GET | `/realtime/{name}/subscribe?channel=…&apikey=…` | publishable (`public:` only) or secret | Server-Sent Events: `ready`, `message` |
| GET/POST | `/realtime/{name}/watches` | secret | `{ postgres, table }` → `{ channel: "db:…:…" }` |
| DELETE | `/realtime/{name}/watches/{postgres}/{table}` | secret | |

## Cron

| Method | Path | Body |
| --- | --- | --- |
| GET/POST | `/cron/{name}/jobs` | `{ name, schedule, action }` |
| PATCH/DELETE | `/cron/{name}/jobs/{id}` | `{ schedule?, action?, enabled? }` |
| POST | `/cron/{name}/jobs/{id}/run` | → `{ status, output }` |
| GET | `/cron/{name}/jobs/{id}/runs` | |

## Monitor

| Method | Path | Body |
| --- | --- | --- |
| GET/POST | `/monitor/{name}/checks` | `{ name, url, method?, intervalSeconds?, timeoutSeconds?, expectStatus? }` |
| PATCH/DELETE | `/monitor/{name}/checks/{id}` | `{ enabled?, intervalSeconds?, url? }` |
| POST | `/monitor/{name}/checks/{id}/run` | |
| GET | `/monitor/{name}/checks/{id}/results?hours=24` | |
| PATCH | `/monitor/{name}/settings` | `{ alertWebhook }` |

## Backup

| Method | Path | Body |
| --- | --- | --- |
| GET/PATCH | `/backup/{name}/settings` | `{ schedule?, retentionDays?, targets?, destination? }` |
| POST | `/backup/{name}/run` | |
| GET | `/backup/{name}/runs` | |

## Consoles

| Method | Path | Body |
| --- | --- | --- |
| POST | `/cache/{name}/command` | `{ args: ["GET", "key"] }` → `{ output }` |
| GET | `/postgres/{name}/extensions` | → `{ installed, available }` |

## Logs and metrics (management API)

| Method | Path | |
| --- | --- | --- |
| GET | `/v1/resources/{id}/logs?follow=1&source=api\|container` | Server-Sent Events: `log`, `line`, `state` |
| GET | `/v1/resources/{id}/metrics?minutes=60` | Requests, errors, latency per minute; memory, CPU and storage samples |
| GET | `/v1/admin/logs?follow=1` | Server logs (admins) |
