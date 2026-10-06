---
title: Backups
description: Scheduled database backups, kept on the server or in your own S3 bucket.
---

Every Postgres-family service (postgres, vector, queues, auth) can be backed up from its **Backups** tab and restored from any backup there.

A **backup service** adds a policy for several databases:

- **Schedule**: a cron expression (at most hourly), for example `0 3 * * *`.
- **Retention**: days to keep each backup, up to the plan's maximum.
- **Destination**: this server, or any S3-compatible bucket (AWS S3, R2, B2, MinIO, Garage). An off-site bucket survives losing the server.

```bash
curl -X PATCH "$SPARKBASE_URL/backup/nightly/settings" -H "apikey: $SPARKBASE_SECRET_KEY" \
  -d '{"targets":["main","jobs"],"schedule":"0 3 * * *","retentionDays":14,
       "destination":{"type":"s3","endpoint":"https://s3.eu-west-1.amazonaws.com","bucket":"my-backups","region":"eu-west-1","accessKey":"AKIA…","secretKey":"…"}}'
```

Dumps are encrypted with the install's key before they leave the server. The S3 secret is stored encrypted and never returned by the API.
