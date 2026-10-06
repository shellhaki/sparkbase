---
title: Queues
description: Durable queues with visibility timeouts, retries and dead letters.
---

```ts
const jobs = sb.queue('jobs')
await jobs.createQueue({ name: 'emails', visibilityTimeout: 60, maxReceives: 5 })
await jobs.queue('emails').send({ to: 'ada@example.com' }, { delay: 30 })

for (const msg of await jobs.queue('emails').receive({ max: 10 })) {
  try {
    await sendEmail(msg.body)
    await jobs.queue('emails').ack(msg.id)
  } catch {
    await jobs.queue('emails').nack(msg.id, { delay: 60 })
  }
}
```

`receive` hides messages for the visibility timeout. Unacknowledged messages come back after it. A message received `maxReceives` times moves to the **dead letters**; `redrive` puts them back. Combine with [cron](cron) to enqueue work on a schedule.
