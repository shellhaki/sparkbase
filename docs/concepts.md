---
title: Concepts
description: Projects, services, keys, plans and how they relate.
---

## Projects

A project is a container for services. It has a short **ref** (for example `a1b2c3d4e5`) and a **project URL**: `https://api.example.com/p/a1b2c3d4e5`.

## Services

A service is one capability inside a project, with a name you choose (`main`, `sessions`, `uploads`). There are two families:

- **Data services** hold your data: `postgres`, `cache`, `storage`, `vector`, `queue`, `auth`.
- **Platform services** do work for you: `realtime`, `cron`, `backup`, `monitor`.

Each service has a status: `provisioning`, `active`, `paused`, `suspended`, `failed` or `deleting`. Database and cache services also report whether their container is **awake** or **asleep**.

## Keys

| Key | Prefix | Use it in | Can call |
| --- | --- | --- | --- |
| Publishable | `sbp_` | Browsers, mobile apps | Auth routes and `public:` realtime channels |
| Secret | `sbs_` | Your servers | Everything in the project |
| Account key | `sb_` | CLI, CI | The management API for your projects |

Send project keys in the `apikey` header. Never ship the secret key to a browser.

## Plans

A plan is a named set of limits for one kind of service (memory, storage, connections, requests per month). Admins define plans; there are no prices. Moving to a smaller plan is refused while usage is above it.

## Users and roles

The first account on an install is the **admin**. Everyone else joins with an invite link. Admins manage users, plans, hosts and install settings, and can see every project.
