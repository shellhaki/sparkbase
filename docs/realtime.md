---
title: Realtime
description: Push messages to browsers and servers, and stream Postgres table changes.
---

A realtime service carries messages on **channels**. Servers broadcast with the secret key; browsers listen with the publishable key on channels that start with `public:`.

```ts
// Browser
const client = createClient(projectUrl, 'sbp_...')
const stop = client.realtime('live').subscribe(['public:room1'], (m) => console.log(m.event, m.payload))

// Server
await sb.realtime('live').broadcast('public:room1', { text: 'hi' }, { event: 'chat' })
```

Under the hood a subscription is a Server-Sent Events stream: `GET {projectUrl}/realtime/live/subscribe?channel=public:room1&apikey=sbp_...`. Any `EventSource` works.

## Table changes

Watch a table to stream its inserts, updates and deletes:

```ts
const channel = await sb.realtime('live').watchTable('main', 'todos') // "db:main:todos"
sb.realtime('live').subscribe([channel], (m) => console.log(m.event, m.payload)) // insert / update / delete
```

Watching adds a trigger to the table. Change channels need the secret key, so subscribe from your server and fan out to browsers on a `public:` channel when you want them to see it. While someone is subscribed, the database stays awake.

## Limits

Plans cap concurrent subscribers and messages per month.
