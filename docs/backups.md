---
title: Backups
description: Scheduled database backups to your own Cloudflare R2 or S3 bucket.
---

Every Postgres-family service (postgres, vector, queues, auth) can be backed up from its **Backups** tab and restored from any backup there.

A **backup service** adds a policy for several databases:

- **Schedule**: a cron expression (at most hourly), for example `0 3 * * *`.
- **Retention**: days to keep each backup, up to the plan's maximum.
- **Destination**: your own bucket. Cloudflare R2 is built in; any other S3-compatible bucket (AWS S3, B2, MinIO, Garage) works too. Backups can also stay on this server, but they are lost if the server is.

## Bring your own R2 bucket

1. In Cloudflare, create an R2 bucket, for example `my-backups`.
2. Create an R2 API token with **Object Read & Write** on that bucket. Note the access key ID and secret access key.
3. Copy your **account ID** from the R2 overview page.
4. In the backup service's **Policy** tab, pick **Cloudflare R2** and fill in the account ID, bucket and keys. Tick **EU jurisdiction** if the bucket was created in the EU jurisdiction.

When you save, Sparkbase writes and deletes a small test file, so wrong keys or a missing bucket show up straight away rather than at 3 a.m.

```bash
curl -X PATCH "$SPARKBASE_URL/backup/nightly/settings" \
  -H "apikey: $SPARKBASE_SECRET_KEY" \
  -d '{
    "targets": ["main", "jobs"],
    "schedule": "0 3 * * *",
    "retentionDays": 14,
    "destination": {
      "type": "r2",
      "accountId": "0123456789abcdef0123456789abcdef",
      "bucket": "my-backups",
      "prefix": "sparkbase",
      "accessKey": "…",
      "secretKey": "…"
    }
  }'
```

## Any other S3 bucket

Use `"type": "s3"` with an `endpoint` and `region` instead of `accountId`:

```json
{ "type": "s3", "endpoint": "https://s3.eu-west-1.amazonaws.com", "region": "eu-west-1", "bucket": "my-backups", "accessKey": "AKIA…", "secretKey": "…" }
```

## Security

Dumps are encrypted with the install's key before they leave the server, so the bucket only ever holds ciphertext. Keep `ENCRYPTION_KEY` safe: without it the backups cannot be restored. The bucket secret is stored encrypted and never returned by the API. Saving the settings again without `secretKey` keeps the stored one.
