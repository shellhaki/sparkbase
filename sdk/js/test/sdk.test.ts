// Runs against a live server: ../../scripts/with-server.sh bun test
import { afterAll, beforeAll, describe, expect, test } from 'bun:test'
import {
  createClient,
  NotFoundError,
  ForbiddenError,
  QueryError,
  UnauthorizedError,
  type SparkbaseClient,
} from '../src/index'

const API = process.env.SB_API
const SETUP = process.env.SB_SETUP_TOKEN
const live = !!API && !!SETUP

let cookie = ''
async function mgmt(method: string, path: string, body?: unknown): Promise<any> {
  const res = await fetch(API + path, {
    method,
    headers: { 'Content-Type': 'application/json', Cookie: cookie },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const set = res.headers.get('set-cookie')
  if (set) cookie = set.split(';')[0]
  const text = await res.text()
  if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${text}`)
  return text ? JSON.parse(text) : undefined
}

let project: { id: string; ref: string; url: string }
let secret: SparkbaseClient
let pub: SparkbaseClient

describe.skipIf(!live)('sdk against a live server', () => {
  beforeAll(async () => {
    await mgmt('POST', '/v1/setup', { token: SETUP, email: 'sdk@example.com', password: 'sdk password 123' })
    project = (await mgmt('POST', '/v1/projects', { name: 'sdk' })).project
    const keys = await mgmt('GET', `/v1/projects/${project.id}/keys`)
    const ids: string[] = []
    for (const [kind, name] of [
      ['postgres', 'main'],
      ['cache', 'kv'],
      ['storage', 'files'],
      ['vector', 'docs'],
      ['queue', 'jobs'],
      ['auth', 'auth'],
    ]) {
      ids.push((await mgmt('POST', `/v1/projects/${project.id}/resources`, { kind, name })).resource.id)
    }
    for (const id of ids) {
      for (let i = 0; i < 300; i++) {
        const r = (await mgmt('GET', `/v1/resources/${id}`)).resource
        if (r.status === 'active') break
        if (r.status === 'failed') throw new Error(`provisioning failed: ${r.lastError}`)
        await Bun.sleep(100)
      }
    }
    secret = createClient(keys.url, keys.secretKey)
    pub = createClient(keys.url, keys.publishableKey)
  }, 60_000)

  afterAll(async () => {
    if (!project) return
    await mgmt('DELETE', `/v1/projects/${project.id}`)
    for (let i = 0; i < 200; i++) {
      const list = await mgmt('GET', '/v1/projects')
      if (!list.projects.some((p: { id: string }) => p.id === project.id)) return
      await Bun.sleep(100)
    }
  }, 30_000)

  test('validates url and key', () => {
    expect(() => createClient('https://example.com', 'sbs_x')).toThrow()
    expect(() => createClient(project.url, 'nope')).toThrow()
  })

  test('info lists services with the secret key', async () => {
    const info = await secret.info()
    expect(info.services?.map((s) => s.kind).sort()).toEqual(['auth', 'cache', 'postgres', 'queue', 'storage', 'vector'])
  })

  test('postgres', async () => {
    const db = secret.postgres('main')
    await db.query('create table todos (id serial primary key, title text not null)')
    await db.query('insert into todos (title) values ($1), ($2)', ['a', 'b'])
    const { rows } = await db.query<{ id: number; title: string }>('select * from todos order by id')
    expect(rows.map((r) => r.title)).toEqual(['a', 'b'])
    await expect(db.transaction([{ sql: "insert into todos (title) values ('c')" }, { sql: 'insert into todos (title) values (null)' }])).rejects.toBeInstanceOf(QueryError)
    expect((await db.query('select count(*)::int as n from todos')).rows[0].n).toBe(2)
    const tables = (await db.schema()) as Array<{ name: string }>
    expect(tables[0].name).toBe('todos')
    await expect(pub.postgres('main').query('select 1')).rejects.toBeInstanceOf(ForbiddenError)
  })

  test('cache', async () => {
    const kv = secret.cache('kv')
    await kv.set('user:1', { name: 'Ada' }, { ttl: 60 })
    expect(await kv.getJSON<{ name: string }>('user:1')).toEqual({ name: 'Ada' })
    expect(await kv.get('missing')).toBeNull()
    expect(await kv.incr('hits', { by: 2 })).toBe(2)
    expect(await kv.keys({ prefix: 'user' })).toEqual(['user:1'])
    expect(await kv.del('user:1')).toBe(true)
  })

  test('storage', async () => {
    const files = secret.storage('files')
    await files.upload('notes/hello.txt', 'hello world', { contentType: 'text/plain' })
    expect(await (await files.download('notes/hello.txt')).text()).toBe('hello world')
    const { objects } = await files.list({ prefix: 'notes/' })
    expect(objects.map((o) => o.key)).toEqual(['notes/hello.txt'])
    const url = await files.signedUrl('notes/hello.txt', { expiresIn: 60 })
    expect(await (await fetch(url)).text()).toBe('hello world')
    const putUrl = await files.signedUrl('direct.bin', { method: 'PUT' })
    expect((await fetch(putUrl, { method: 'PUT', body: new Uint8Array([1, 2, 3]) })).status).toBe(201)
    expect(await files.remove('notes/hello.txt')).toBe(true)
    await expect(files.download('notes/hello.txt')).rejects.toBeInstanceOf(NotFoundError)
  })

  test('vector', async () => {
    const v = secret.vector('docs')
    await v.createCollection({ name: 'pages', dimension: 3 })
    const pages = v.collection('pages')
    await pages.upsert([
      { id: 'a', vector: [1, 0, 0], metadata: { lang: 'en' } },
      { id: 'b', vector: [0, 1, 0], metadata: { lang: 'fr' } },
    ])
    const hits = await pages.query([0.9, 0.1, 0], { topK: 1 })
    expect(hits[0].id).toBe('a')
    expect((await pages.query([1, 0, 0], { filter: { lang: 'fr' } }))[0].id).toBe('b')
    expect(await pages.get('zzz')).toBeNull()
    expect(await pages.delete(['a'])).toBe(1)
    expect((await v.listCollections())[0].count).toBe(1)
  })

  test('queue', async () => {
    const q = secret.queue('jobs')
    await q.createQueue({ name: 'emails', visibilityTimeout: 30 })
    const emails = q.queue<{ to: string }>('emails')
    await emails.send({ to: 'a@example.com' })
    const [msg] = await emails.receive()
    expect(msg.body.to).toBe('a@example.com')
    expect(await emails.receive()).toEqual([])
    expect(await emails.ack(msg.id)).toBe(1)
    expect((await emails.stats()).ready).toBe(0)
  })

  test('auth with email and password', async () => {
    const events: string[] = []
    pub.auth.onAuthStateChange((e) => events.push(e))
    const { user } = await pub.auth.signUp({ email: 'end@example.com', password: 'end user pass', data: { plan: 'free' } })
    expect(user.data.plan).toBe('free')
    expect((await pub.auth.getUser()).email).toBe('end@example.com')
    await pub.auth.updateUser({ data: { name: 'End' } })
    await pub.auth.refreshSession()
    await pub.auth.signOut()
    await expect(pub.auth.getUser()).rejects.toBeInstanceOf(UnauthorizedError)
    await expect(pub.auth.signInWithPassword({ email: 'end@example.com', password: 'wrong password' })).rejects.toBeInstanceOf(UnauthorizedError)
    const { session } = await pub.auth.signInWithPassword({ email: 'end@example.com', password: 'end user pass' })
    expect(session.user.data.name).toBe('End')
    expect(events).toEqual(['SIGNED_IN', 'USER_UPDATED', 'TOKEN_REFRESHED', 'SIGNED_OUT', 'SIGNED_IN'])

    // Admin from the server side.
    const { users, total } = await secret.auth.admin.listUsers()
    expect(total).toBe(1)
    const link = await secret.auth.admin.generateLink({ email: 'otp@example.com' })
    const other = createClient(project.url, (await mgmt('GET', `/v1/projects/${project.id}/keys`)).publishableKey)
    const res = await other.auth.verifyOtp({ token: link.token })
    expect(res.user.email).toBe('otp@example.com')
    await secret.auth.admin.deleteUser(users[0].id)
  })
})
