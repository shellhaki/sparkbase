---
title: Cache
description: A Redis namespace with its own user, native rediss:// access and a key/value API.
---

## Connect

| How | Where |
| --- | --- |
| Native | `rediss://USER:PASSWORD@sessions-REF.cache.example.com:6380` |
| HTTP | `GET/PUT/DELETE {projectUrl}/cache/sessions/keys/{key}` |
| SDK | `sb.cache('sessions')` |

Every key lives under the service's **prefix** (shown on the Overview tab, also `REDIS_KEY_PREFIX` from `env pull`). The API and SDK add it for you; native clients must include it. `KEYS`, `SCAN`, `FLUSHALL`, `CONFIG`, scripts and admin commands are denied.

```ts
const cache = sb.cache('sessions')
await cache.set('user:42', { name: 'Ada' }, { ttl: 3600 })
const user = await cache.getJSON('user:42')
await cache.incr('hits', { by: 1, ttl: 60 })
```

## Terminal

The **Terminal** tab runs Redis commands as the service's own user, and prefixes keys for the common commands: `SET greeting hello`, `GET greeting`, `TTL greeting`, `HGETALL user:1`.

## Limits

At the memory limit, writes are refused until keys are deleted; reads and deletes keep working. Extra clients above the connection limit are disconnected.
