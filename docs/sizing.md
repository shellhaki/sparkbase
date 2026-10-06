---
title: Sizing
description: How much a server can hold.
---

These are starting points, not measurements. With the container engine, memory is only used by services that are awake: size for the services busy at the same time, not for every service you have. Watch Admin → Health and the usage meters, then adjust plans and add hosts.

| Server | Comfortable for |
| --- | --- |
| 2 vCPU, 4 GB RAM, 40 GB disk | 10 to 20 small services across a few projects |
| 4 vCPU, 8 GB RAM, 160 GB disk | 50 small services, or a handful of medium ones |
| 8 vCPU, 16 GB RAM, 400 GB disk | 100+ small services; move busy ones to their own host |

Rules of thumb:

- **Container services** each get their plan's memory as a hard limit (Small 256 MB, Medium 1 GB, Large 4 GB for Postgres-family services; cache memory plus 48 MB). An awake idle Postgres container uses about 40 to 80 MB.
- **Postgres** memory grows with connections. Each direct connection can use `work_mem` per sort; the pooled port keeps server connections low. Disk is the sum of the plans' storage limits you are willing to overcommit.
- **Redis** memory is shared: the sum of cache plans' `memory_mb` should stay under the Redis host's RAM, or a busy tenant will push others to their limits.
- **Garage** needs disk for objects plus about 1% for metadata.
- Keep 20% of disk free on every host. Admin → Health warns when a host stops answering.
