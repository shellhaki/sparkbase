---
title: Storage
description: A private bucket for files, with uploads, downloads and signed URLs.
---

On the Docker install each storage service is a Garage bucket with its own S3 key.

## Use it

```ts
const files = sb.storage('uploads')
await files.upload('avatars/42.png', file, { contentType: 'image/png' })
const url = await files.signedUrl('avatars/42.png', { expiresIn: 600 })            // anyone can GET it
const put = await files.signedUrl('avatars/43.png', { method: 'PUT' })              // a browser uploads straight to storage
const { objects } = await files.list({ prefix: 'avatars/' })
```

Native S3 clients use the endpoint `https://s3.example.com`, region `garage`, path-style addressing and the bucket, access key and secret from the Overview tab.

Keys are relative paths: no leading `/`, no `.` or `..` segments. Signed URLs last at most 7 days.
