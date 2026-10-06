// Uses every service in a project once. Needs a project with services named
// main (postgres), sessions (cache), uploads (storage), docs (vector) and
// jobs (queue). Run `sparkbase link <ref> && sparkbase env pull` first.
import { createClient } from '@sparkbase/sdk'

const sb = createClient(process.env.SPARKBASE_URL, process.env.SPARKBASE_SECRET_KEY)

await sb.postgres('main').query('create table if not exists visits (id serial primary key, at timestamptz default now())')
await sb.postgres('main').query('insert into visits default values')
const { rows } = await sb.postgres('main').query('select count(*)::int as n from visits')
console.log('visits:', rows[0].n)

console.log('hits:', await sb.cache('sessions').incr('hits'))

await sb.storage('uploads').upload('hello.txt', 'hello from the quickstart', { contentType: 'text/plain' })
console.log('signed url:', await sb.storage('uploads').signedUrl('hello.txt', { expiresIn: 60 }))

const docs = sb.vector('docs')
if (!(await docs.listCollections()).some((c) => c.name === 'notes')) {
  await docs.createCollection({ name: 'notes', dimension: 3 })
}
await docs.collection('notes').upsert([{ id: 'a', vector: [1, 0, 0], metadata: { title: 'first' } }])
console.log('nearest:', (await docs.collection('notes').query([0.9, 0.1, 0], { topK: 1 }))[0]?.id)

const jobs = sb.queue('jobs')
if (!(await jobs.listQueues()).some((q) => q.name === 'emails')) {
  await jobs.createQueue({ name: 'emails' })
}
await jobs.queue('emails').send({ to: 'ada@example.com' })
for (const msg of await jobs.queue('emails').receive({ max: 10 })) {
  console.log('job:', msg.body)
  await jobs.queue('emails').ack(msg.id)
}
