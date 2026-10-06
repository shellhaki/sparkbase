---
title: CLI
description: The sparkbase command line.
---

Download `sparkbase` from the release page, or build it: `cd server && go build ./cmd/sparkbase`.

| Command | Does |
| --- | --- |
| `sparkbase login --url URL [--email E] [--key sb_...]` | Sign in and store an account key (password from the prompt or `SPARKBASE_PASSWORD`) |
| `sparkbase logout` | Forget the stored key |
| `sparkbase projects list` / `create NAME` | Manage projects |
| `sparkbase link REF` | Link the current folder to a project (`.sparkbase/project.json`) |
| `sparkbase services list` | Services and their status |
| `sparkbase services add KIND NAME [--plan PLAN]` | Add a service and wait until it is ready |
| `sparkbase services pause\|resume\|delete NAME` | Lifecycle |
| `sparkbase env pull [--file .env]` | Write `SPARKBASE_URL`, both keys, `DATABASE_URL`, `REDIS_URL` and per-service variables. Other lines stay as they are |
| `sparkbase usage` | Usage against limits |
| `sparkbase admin invite EMAIL [--role admin]` | Create an invite link |
| `sparkbase admin users` / `admin hosts` | Admin views |

The config lives in your user config folder (`sparkbase/config.json`).
