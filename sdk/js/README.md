# @sparkbase/sdk

One client for every service in a Sparkbase project.

```ts
import { createClient } from '@sparkbase/sdk'

const sb = createClient(process.env.SPARKBASE_URL!, process.env.SPARKBASE_SECRET_KEY!)

const { rows } = await sb.postgres('main').query('select * from todos where id = $1', [1])
await sb.cache('sessions').set('user:42', { name: 'Ada' }, { ttl: 60 })
await sb.storage('uploads').upload('avatars/42.png', file)
const url = await sb.storage('uploads').signedUrl('avatars/42.png', { expiresIn: 600 })
await sb.vector('docs').collection('pages').upsert([{ id: 'p1', vector: embedding }])
await sb.queue('jobs').queue('emails').send({ to: 'ada@example.com' })
```

In a browser, use the publishable key. It can only call the auth routes:

```ts
const client = createClient(projectUrl, 'sbp_...', { storage: localStorage })
await client.auth.signUp({ email, password })
await client.auth.signInWithPassword({ email, password })
const user = await client.auth.getUser()
```

Never ship the secret key (`sbs_`) to a browser.

Tests run against a live server: `../../scripts/with-server.sh bun test`.

License: MIT.
