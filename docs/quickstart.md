---
title: Quickstart
description: Create a project, add services and call them from your app.
---

1. Sign in to the dashboard and create a project. It gets a **project URL** and two keys:
   - **Publishable key** (`sbp_`): safe in browsers. It can only call the auth routes.
   - **Secret key** (`sbs_`): for servers. It can call everything in the project.
2. Add the services you need. Each one is ready in a few seconds.
3. Call them from your app.

```bash
npm install @sparkbase/sdk
```

```ts
import { createClient } from '@sparkbase/sdk'

const sb = createClient(process.env.SPARKBASE_URL!, process.env.SPARKBASE_SECRET_KEY!)
```

Or pull every URL and key into `.env` with the CLI:

```bash
sparkbase login --url https://api.example.com
sparkbase link <project-ref>
sparkbase env pull
```

## Postgres

```ts
await sb.postgres('main').query('create table todos (id serial primary key, title text)')
const { rows } = await sb.postgres('main').query('select * from todos where id = $1', [1])
```

Or connect any driver with `DATABASE_URL` from `env pull`.

## Cache

```ts
await sb.cache('sessions').set('user:42', { name: 'Ada' }, { ttl: 3600 })
const user = await sb.cache('sessions').getJSON('user:42')
```

## Storage

```ts
await sb.storage('uploads').upload('avatars/42.png', file)
const url = await sb.storage('uploads').signedUrl('avatars/42.png', { expiresIn: 600 })
```

Give browsers a signed `PUT` URL to upload straight to storage without your secret key:

```ts
const uploadUrl = await sb.storage('uploads').signedUrl('avatars/42.png', { method: 'PUT' })
```

## Vector

```ts
const docs = sb.vector('docs')
await docs.createCollection({ name: 'pages', dimension: 1536 })
await docs.collection('pages').upsert([{ id: 'home', vector: embedding, metadata: { lang: 'en' } }])
const matches = await docs.collection('pages').query(queryEmbedding, { topK: 5, filter: { lang: 'en' } })
```

## Queues

```ts
const jobs = sb.queue('jobs')
await jobs.createQueue({ name: 'emails', visibilityTimeout: 60, maxReceives: 5 })
await jobs.queue('emails').send({ to: 'ada@example.com' })

for (const msg of await jobs.queue('emails').receive({ max: 10 })) {
  await sendEmail(msg.body)
  await jobs.queue('emails').ack(msg.id)
}
```

## Auth

In the browser, with the publishable key:

```ts
const client = createClient(projectUrl, 'sbp_...', { storage: localStorage })
await client.auth.signUp({ email, password })
await client.auth.signInWithPassword({ email, password })
const token = await client.auth.getAccessToken() // send it to your server
```

On your server, verify the access token with the service's JWT secret (dashboard → auth service → Overview). Tokens are HS256 JWTs with `sub` (user id), `email` and `exp`.
