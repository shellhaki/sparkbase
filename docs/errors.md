---
title: Errors
description: Error codes and what to do about them.
---

| Code | HTTP | Meaning | What to do |
| --- | --- | --- | --- |
| `unauthorized` | 401 | Missing or wrong key, session or token | Check the `apikey` header; refresh the user's session |
| `forbidden` | 403 | The key or role cannot do this | Use the secret key on servers; ask an admin |
| `not_found` | 404 | No such project, service, object or key | Check names; services are addressed by name |
| `resource_limit_reached` | 403 | The per-user service cap is reached | Delete a service or ask an admin to raise the cap |
| `quota_exceeded` | 429 | A plan limit is hit (storage, memory, users, monthly requests) | Delete data or move to a bigger plan |
| `rate_limited` | 429 | Too many requests | Wait for `Retry-After` seconds |
| `account_locked` | 429 | Too many wrong passwords | Wait 15 minutes or reset the password |
| `resource_not_ready` | 409 | The service is provisioning, paused, suspended or failed | Wait, resume it, or ask an admin |
| `plan_change_blocked` | 409 | Usage is above the smaller plan | Free space first |
| `conflict` | 409 | Name taken, or a second auth service | Pick another name |
| `setup_closed` | 409 | The install already has an admin | Sign in or ask for an invite |
| `validation_failed` | 422 | Bad request body | Read `message` |
| `query_failed` | 400 | Postgres rejected the SQL | `details.pgCode` has the Postgres error code |
| `payload_too_large` | 413 | Upload above the plan's size | Use a signed PUT URL or a bigger plan |
