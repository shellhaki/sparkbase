/**
 * @sparkbase/sdk: one client for every service in a Sparkbase project.
 *
 *   const sb = createClient('https://api.example.com/p/a1b2c3', 'sbs_...')
 *   const { rows } = await sb.postgres('main').query('select 1 as x')
 *
 * Zero dependencies; uses fetch, so it runs in Node, Bun, Deno, browsers and
 * edge runtimes. Every method maps to one documented endpoint.
 */

export type Fetch = typeof fetch

/** Storage for the auth session, e.g. `localStorage` in a browser. */
export interface SessionStorage {
  getItem(key: string): string | null | Promise<string | null>
  setItem(key: string, value: string): void | Promise<void>
  removeItem(key: string): void | Promise<void>
}

export interface ClientOptions {
  fetch?: Fetch
  headers?: Record<string, string>
  /** Where the auth session is kept. Defaults to memory. */
  storage?: SessionStorage
  /** Storage key for the session. Defaults to `sparkbase.session.<ref>`. */
  storageKey?: string
  /** Refresh the access token shortly before it expires. Default true. */
  autoRefresh?: boolean
}

// Errors

export class SparkbaseError extends Error {
  readonly code: string
  readonly status: number
  readonly details: Record<string, unknown>
  constructor(status: number, code: string, message: string, details: Record<string, unknown> = {}) {
    super(message)
    this.name = 'SparkbaseError'
    this.status = status
    this.code = code
    this.details = details
  }
}
export class UnauthorizedError extends SparkbaseError {}
export class ForbiddenError extends SparkbaseError {}
export class NotFoundError extends SparkbaseError {}
export class QuotaExceededError extends SparkbaseError {}
export class RateLimitedError extends SparkbaseError {}
export class ResourceNotReadyError extends SparkbaseError {}
export class ValidationError extends SparkbaseError {}
export class QueryError extends SparkbaseError {}

const errorClasses: Record<string, typeof SparkbaseError> = {
  unauthorized: UnauthorizedError,
  forbidden: ForbiddenError,
  not_found: NotFoundError,
  quota_exceeded: QuotaExceededError,
  rate_limited: RateLimitedError,
  resource_not_ready: ResourceNotReadyError,
  validation_failed: ValidationError,
  query_failed: QueryError,
}

function toError(status: number, body: unknown): SparkbaseError {
  const e = (body as { error?: { code?: string; message?: string; details?: Record<string, unknown> } })?.error
  const code = e?.code ?? `http_${status}`
  const Cls = errorClasses[code] ?? SparkbaseError
  const err = new Cls(status, code, e?.message ?? `request failed with status ${status}`, e?.details ?? {})
  err.name = Cls.name
  return err
}

// Transport

type Body = BodyInit | Record<string, unknown> | unknown[] | undefined

interface RequestOptions {
  body?: Body
  raw?: boolean // send body as is (uploads)
  headers?: Record<string, string>
  user?: boolean // send the signed-in user's access token
  response?: 'json' | 'raw'
}

class Transport {
  constructor(
    readonly url: string,
    readonly key: string,
    readonly fetchFn: Fetch,
    readonly headers: Record<string, string>,
    readonly userToken: () => Promise<string | null>,
  ) {}

  async request<T = any>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
    const headers: Record<string, string> = { apikey: this.key, 'X-Sparkbase-Client': 'sdk-js', ...this.headers, ...opts.headers }
    let body: BodyInit | undefined
    if (opts.body !== undefined) {
      if (opts.raw) {
        body = opts.body as BodyInit
      } else {
        body = JSON.stringify(opts.body)
        headers['Content-Type'] = 'application/json'
      }
    }
    if (opts.user) {
      const tok = await this.userToken()
      if (!tok) throw new UnauthorizedError(401, 'unauthorized', 'no signed-in user')
      headers.Authorization = `Bearer ${tok}`
    }
    const res = await this.fetchFn(this.url + path, { method, headers, body })
    if (opts.response === 'raw') {
      if (!res.ok) throw toError(res.status, await res.json().catch(() => ({})))
      return res as unknown as T
    }
    const text = await res.text()
    const data = text ? JSON.parse(text) : undefined
    if (!res.ok) throw toError(res.status, data)
    return data as T
  }
}

const enc = encodeURIComponent
function encPath(key: string): string {
  return key.split('/').map(enc).join('/')
}
function qs(params: Record<string, string | number | undefined>): string {
  const p = Object.entries(params).filter(([, v]) => v !== undefined && v !== '')
  return p.length ? '?' + p.map(([k, v]) => `${enc(k)}=${enc(String(v))}`).join('&') : ''
}

// Postgres

export interface Field {
  name: string
  type: string
}
export interface QueryResult<R = Record<string, unknown>> {
  command: string
  rowCount: number
  fields: Field[]
  rows: R[]
  truncated?: boolean
}
export interface Statement {
  sql: string
  params?: unknown[]
}

export class PostgresClient {
  constructor(private t: Transport, readonly name: string) {}
  private get base() {
    return `/postgres/${enc(this.name)}`
  }
  /** Run one parameterized statement ($1, $2, ...). */
  query<R = Record<string, unknown>>(sql: string, params: unknown[] = [], opts: { rowLimit?: number } = {}): Promise<QueryResult<R>> {
    return this.t.request('POST', `${this.base}/query`, { body: { sql, params, ...opts } })
  }
  /** Run several statements atomically. */
  async transaction(statements: Statement[], opts: { rowLimit?: number } = {}): Promise<QueryResult[]> {
    const out = await this.t.request<{ results: QueryResult[] }>('POST', `${this.base}/transaction`, { body: { statements, ...opts } })
    return out.results
  }
  async schema(): Promise<unknown[]> {
    const out = await this.t.request<{ tables: unknown[] }>('GET', `${this.base}/schema`)
    return out.tables
  }
}

// Cache

export type CacheOp = { op: 'get'; key: string } | { op: 'set'; key: string; value: string; ttl?: number } | { op: 'del'; key: string }

export class CacheClient {
  constructor(private t: Transport, readonly name: string) {}
  private get base() {
    return `/cache/${enc(this.name)}`
  }
  /** Read a string value; null when the key does not exist. */
  async get(key: string): Promise<string | null> {
    try {
      const out = await this.t.request<{ value: string }>('GET', `${this.base}/keys/${encPath(key)}`)
      return out.value
    } catch (e) {
      if (e instanceof NotFoundError) return null
      throw e
    }
  }
  /** Read and JSON-parse a value written with set(). */
  async getJSON<T = unknown>(key: string): Promise<T | null> {
    const v = await this.get(key)
    return v === null ? null : (JSON.parse(v) as T)
  }
  /** Write a value. Non-strings are stored as JSON. */
  async set(key: string, value: unknown, opts: { ttl?: number; nx?: boolean } = {}): Promise<boolean> {
    const v = typeof value === 'string' ? value : JSON.stringify(value)
    const out = await this.t.request<{ set: boolean }>('PUT', `${this.base}/keys/${encPath(key)}`, { body: { value: v, ...opts } })
    return out.set
  }
  async del(key: string): Promise<boolean> {
    const out = await this.t.request<{ deleted: boolean }>('DELETE', `${this.base}/keys/${encPath(key)}`)
    return out.deleted
  }
  async incr(key: string, opts: { by?: number; ttl?: number } = {}): Promise<number> {
    const out = await this.t.request<{ value: number }>('POST', `${this.base}/incr/${encPath(key)}`, { body: opts })
    return out.value
  }
  async batch(ops: CacheOp[]): Promise<Array<Record<string, unknown>>> {
    const out = await this.t.request<{ results: Array<Record<string, unknown>> }>('POST', `${this.base}/batch`, { body: { ops } })
    return out.results
  }
  async keys(opts: { prefix?: string; limit?: number } = {}): Promise<string[]> {
    const out = await this.t.request<{ keys: string[] }>('GET', `${this.base}/keys${qs(opts)}`)
    return out.keys
  }
}

// Storage

export interface StorageObject {
  key: string
  size: number
  lastModified: string
}

export class StorageClient {
  constructor(private t: Transport, readonly name: string) {}
  private get base() {
    return `/storage/${enc(this.name)}`
  }
  async list(opts: { prefix?: string; after?: string; limit?: number } = {}): Promise<{ objects: StorageObject[]; truncated: boolean; next?: string }> {
    return this.t.request('GET', `${this.base}/objects${qs(opts)}`)
  }
  /** Upload bytes, a string, a Blob or a stream. */
  async upload(key: string, data: BodyInit, opts: { contentType?: string } = {}): Promise<{ key: string; size: number }> {
    const headers: Record<string, string> = {}
    if (opts.contentType) headers['Content-Type'] = opts.contentType
    return this.t.request('PUT', `${this.base}/objects/${encPath(key)}`, { body: data, raw: true, headers })
  }
  /** Download an object as a fetch Response (use .arrayBuffer(), .text(), .body). */
  download(key: string): Promise<Response> {
    return this.t.request('GET', `${this.base}/objects/${encPath(key)}`, { response: 'raw' })
  }
  async remove(key: string): Promise<boolean> {
    const out = await this.t.request<{ deleted: boolean }>('DELETE', `${this.base}/objects/${encPath(key)}`)
    return out.deleted
  }
  /** A URL anyone can use to GET (or PUT) one object until it expires. */
  async signedUrl(key: string, opts: { expiresIn?: number; method?: 'GET' | 'PUT' } = {}): Promise<string> {
    const out = await this.t.request<{ url: string }>('POST', `${this.base}/sign`, { body: { key, ...opts } })
    return out.url
  }
}

// Vector

export type Metric = 'cosine' | 'l2' | 'inner_product'
export interface Collection {
  name: string
  dimension: number
  metric: Metric
  createdAt: string
  count?: number
}
export interface VectorItem {
  id: string
  vector: number[]
  metadata?: Record<string, unknown>
}
export interface Match {
  id: string
  score: number
  distance: number
  metadata: Record<string, unknown>
  vector?: number[]
}

export class VectorCollection {
  constructor(private t: Transport, private base: string) {}
  async upsert(items: VectorItem[]): Promise<number> {
    const out = await this.t.request<{ upserted: number }>('POST', `${this.base}/upsert`, { body: { items } })
    return out.upserted
  }
  async query(vector: number[], opts: { topK?: number; filter?: Record<string, unknown>; includeVectors?: boolean } = {}): Promise<Match[]> {
    const out = await this.t.request<{ matches: Match[] }>('POST', `${this.base}/query`, { body: { vector, ...opts } })
    return out.matches
  }
  async get(id: string): Promise<VectorItem | null> {
    try {
      return await this.t.request('GET', `${this.base}/items/${enc(id)}`)
    } catch (e) {
      if (e instanceof NotFoundError) return null
      throw e
    }
  }
  async delete(ids: string[]): Promise<number> {
    const out = await this.t.request<{ deleted: number }>('POST', `${this.base}/delete`, { body: { ids } })
    return out.deleted
  }
  async drop(): Promise<void> {
    await this.t.request('DELETE', this.base)
  }
}

export class VectorClient {
  constructor(private t: Transport, readonly name: string) {}
  private get base() {
    return `/vector/${enc(this.name)}/collections`
  }
  async createCollection(c: { name: string; dimension: number; metric?: Metric }): Promise<Collection> {
    const out = await this.t.request<{ collection: Collection }>('POST', this.base, { body: c })
    return out.collection
  }
  async listCollections(): Promise<Collection[]> {
    const out = await this.t.request<{ collections: Collection[] }>('GET', this.base)
    return out.collections
  }
  collection(name: string): VectorCollection {
    return new VectorCollection(this.t, `${this.base}/${enc(name)}`)
  }
}

// Queue

export interface Message<T = unknown> {
  id: number
  body: T
  receives: number
  enqueuedAt: string
}

export class Queue<T = unknown> {
  constructor(private t: Transport, private base: string) {}
  /** Send one message. */
  async send(body: T, opts: { delay?: number } = {}): Promise<number> {
    const ids = await this.sendBatch([{ body, ...opts }])
    return ids[0]
  }
  async sendBatch(messages: Array<{ body: T; delay?: number }>): Promise<number[]> {
    const out = await this.t.request<{ ids: number[] }>('POST', `${this.base}/send`, { body: { messages } })
    return out.ids
  }
  /** Take up to max messages; they stay hidden for the visibility timeout. */
  async receive(opts: { max?: number; visibilityTimeout?: number } = {}): Promise<Message<T>[]> {
    const out = await this.t.request<{ messages: Message<T>[] }>('POST', `${this.base}/receive`, { body: opts })
    return out.messages
  }
  /** Delete processed messages. */
  async ack(ids: number | number[]): Promise<number> {
    const out = await this.t.request<{ acked: number }>('POST', `${this.base}/ack`, { body: { ids: ([] as number[]).concat(ids) } })
    return out.acked
  }
  /** Return messages to the queue, optionally after a delay. */
  async nack(ids: number | number[], opts: { delay?: number } = {}): Promise<number> {
    const out = await this.t.request<{ released: number }>('POST', `${this.base}/nack`, { body: { ids: ([] as number[]).concat(ids), ...opts } })
    return out.released
  }
  stats(): Promise<{ ready: number; inFlight: number; dead: number; oldestEnqueuedAt: string | null }> {
    return this.t.request('GET', `${this.base}/stats`)
  }
  async dead(opts: { limit?: number } = {}): Promise<Message<T>[]> {
    const out = await this.t.request<{ messages: Message<T>[] }>('GET', `${this.base}/dead${qs(opts)}`)
    return out.messages
  }
  async redrive(): Promise<number> {
    const out = await this.t.request<{ redriven: number }>('POST', `${this.base}/redrive`)
    return out.redriven
  }
  async purge(): Promise<number> {
    const out = await this.t.request<{ purged: number }>('POST', `${this.base}/purge`)
    return out.purged
  }
  async drop(): Promise<void> {
    await this.t.request('DELETE', this.base)
  }
}

export class QueueClient {
  constructor(private t: Transport, readonly name: string) {}
  private get base() {
    return `/queue/${enc(this.name)}/queues`
  }
  async createQueue(q: { name: string; visibilityTimeout?: number; maxReceives?: number }): Promise<unknown> {
    const out = await this.t.request<{ queue: unknown }>('POST', this.base, { body: q })
    return out.queue
  }
  async listQueues(): Promise<Array<Record<string, unknown>>> {
    const out = await this.t.request<{ queues: Array<Record<string, unknown>> }>('GET', this.base)
    return out.queues
  }
  queue<T = unknown>(name: string): Queue<T> {
    return new Queue<T>(this.t, `${this.base}/${enc(name)}`)
  }
}

// Auth

export interface User {
  id: string
  email: string
  data: Record<string, unknown>
  appData: Record<string, unknown>
  emailConfirmedAt: string | null
  lastSignInAt: string | null
  bannedAt: string | null
  createdAt: string
  updatedAt: string
}
export interface Session {
  access_token: string
  token_type: 'bearer'
  expires_in: number
  expires_at: number
  refresh_token: string
  user: User
}
export type AuthEvent = 'SIGNED_IN' | 'SIGNED_OUT' | 'TOKEN_REFRESHED' | 'USER_UPDATED'

class MemoryStorage implements SessionStorage {
  private m = new Map<string, string>()
  getItem(k: string) {
    return this.m.get(k) ?? null
  }
  setItem(k: string, v: string) {
    this.m.set(k, v)
  }
  removeItem(k: string) {
    this.m.delete(k)
  }
}

export class AuthAdmin {
  constructor(private t: Transport) {}
  listUsers(opts: { limit?: number; offset?: number; email?: string } = {}): Promise<{ users: User[]; total: number }> {
    return this.t.request('GET', `/auth/admin/users${qs(opts)}`)
  }
  async createUser(u: { email: string; password?: string; data?: Record<string, unknown>; appData?: Record<string, unknown> }): Promise<User> {
    return (await this.t.request<{ user: User }>('POST', '/auth/admin/users', { body: u })).user
  }
  async getUser(id: string): Promise<User> {
    return (await this.t.request<{ user: User }>('GET', `/auth/admin/users/${enc(id)}`)).user
  }
  async updateUser(id: string, patch: { data?: Record<string, unknown>; appData?: Record<string, unknown>; banned?: boolean }): Promise<User> {
    return (await this.t.request<{ user: User }>('PATCH', `/auth/admin/users/${enc(id)}`, { body: patch })).user
  }
  async deleteUser(id: string): Promise<void> {
    await this.t.request('DELETE', `/auth/admin/users/${enc(id)}`)
  }
  /** A sign-in link and code for apps that send their own email. */
  generateLink(opts: { email: string; redirectTo?: string }): Promise<{ token: string; code: string; link: string; expiresIn: number }> {
    return this.t.request('POST', '/auth/admin/generate-link', { body: opts })
  }
  async getSettings(): Promise<Record<string, unknown>> {
    return (await this.t.request<{ settings: Record<string, unknown> }>('GET', '/auth/admin/settings')).settings
  }
  async updateSettings(s: {
    allowSignups?: boolean
    siteUrl?: string
    redirectUrls?: string[]
    accessTokenTtl?: number
    refreshTokenTtl?: number
  }): Promise<Record<string, unknown>> {
    return (await this.t.request<{ settings: Record<string, unknown> }>('PATCH', '/auth/admin/settings', { body: s })).settings
  }
}

export class AuthClient {
  readonly admin: AuthAdmin
  private listeners = new Set<(event: AuthEvent, session: Session | null) => void>()
  private refreshing: Promise<Session | null> | null = null

  constructor(
    private t: Transport,
    private storage: SessionStorage,
    private storageKey: string,
    private autoRefresh: boolean,
  ) {
    this.admin = new AuthAdmin(t)
  }

  private async save(session: Session | null, event: AuthEvent) {
    if (session) await this.storage.setItem(this.storageKey, JSON.stringify(session))
    else await this.storage.removeItem(this.storageKey)
    for (const fn of this.listeners) fn(event, session)
  }

  /** The stored session, refreshed first when it is about to expire. */
  async getSession(): Promise<Session | null> {
    const raw = await this.storage.getItem(this.storageKey)
    if (!raw) return null
    const session = JSON.parse(raw) as Session
    if (this.autoRefresh && session.expires_at - 60 < Date.now() / 1000) {
      return this.refreshSession().catch(() => null)
    }
    return session
  }

  /** The current access token, for your own server calls. */
  async getAccessToken(): Promise<string | null> {
    return (await this.getSession())?.access_token ?? null
  }

  onAuthStateChange(fn: (event: AuthEvent, session: Session | null) => void): () => void {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  async signUp(opts: { email: string; password: string; data?: Record<string, unknown> }): Promise<{ session: Session; user: User }> {
    const out = await this.t.request<{ session: Session; user: User }>('POST', '/auth/signup', { body: opts })
    await this.save(out.session, 'SIGNED_IN')
    return out
  }

  async signInWithPassword(opts: { email: string; password: string }): Promise<{ session: Session; user: User }> {
    const out = await this.t.request<{ session: Session; user: User }>('POST', '/auth/token?grant_type=password', { body: opts })
    await this.save(out.session, 'SIGNED_IN')
    return out
  }

  /** Email a one-time sign-in link and 6-digit code. */
  async signInWithOtp(opts: { email: string; redirectTo?: string; data?: Record<string, unknown>; createUser?: boolean }): Promise<void> {
    await this.t.request('POST', '/auth/otp', { body: opts })
  }

  /** Email a recovery link and code; verify it, then call updateUser({ password }). */
  async recover(opts: { email: string; redirectTo?: string }): Promise<void> {
    await this.t.request('POST', '/auth/recover', { body: opts })
  }

  /** Exchange a link token, or an email and code, for a session. */
  async verifyOtp(opts: { token: string } | { email: string; code: string }): Promise<{ session: Session; user: User }> {
    const out = await this.t.request<{ session: Session; user: User }>('POST', '/auth/verify', { body: opts })
    await this.save(out.session, 'SIGNED_IN')
    return out
  }

  async refreshSession(): Promise<Session | null> {
    if (this.refreshing) return this.refreshing
    this.refreshing = (async () => {
      const raw = await this.storage.getItem(this.storageKey)
      if (!raw) return null
      const old = JSON.parse(raw) as Session
      try {
        const out = await this.t.request<{ session: Session }>('POST', '/auth/token?grant_type=refresh_token', {
          body: { refresh_token: old.refresh_token },
        })
        await this.save(out.session, 'TOKEN_REFRESHED')
        return out.session
      } catch (e) {
        if (e instanceof UnauthorizedError || e instanceof ForbiddenError) await this.save(null, 'SIGNED_OUT')
        throw e
      }
    })().finally(() => {
      this.refreshing = null
    })
    return this.refreshing
  }

  async getUser(): Promise<User> {
    return (await this.t.request<{ user: User }>('GET', '/auth/user', { user: true })).user
  }

  async updateUser(patch: { data?: Record<string, unknown>; password?: string }): Promise<User> {
    const user = (await this.t.request<{ user: User }>('PUT', '/auth/user', { body: patch, user: true })).user
    const session = await this.getSession()
    if (session) await this.save({ ...session, user }, 'USER_UPDATED')
    return user
  }

  async signOut(opts: { scope?: 'local' | 'global' } = {}): Promise<void> {
    try {
      if (await this.getAccessToken()) {
        await this.t.request('POST', `/auth/logout${opts.scope === 'global' ? '?scope=global' : ''}`, { user: true })
      }
    } finally {
      await this.save(null, 'SIGNED_OUT')
    }
  }
}

// Client

export class SparkbaseClient {
  readonly auth: AuthClient
  private t: Transport

  constructor(readonly url: string, key: string, opts: ClientOptions = {}) {
    if (!/^https?:\/\/.+\/p\/[a-z0-9]+\/?$/.test(url)) {
      throw new Error('createClient: url must be a project URL like https://api.example.com/p/a1b2c3')
    }
    if (!/^sb[ps]_/.test(key)) {
      throw new Error('createClient: key must be a project key (sbp_ publishable or sbs_ secret)')
    }
    this.url = url.replace(/\/$/, '')
    const ref = this.url.split('/p/')[1]
    const fetchFn = opts.fetch ?? globalThis.fetch.bind(globalThis)
    let auth: AuthClient | undefined
    this.t = new Transport(this.url, key, fetchFn, opts.headers ?? {}, async () => (auth ? auth.getAccessToken() : null))
    auth = new AuthClient(this.t, opts.storage ?? new MemoryStorage(), opts.storageKey ?? `sparkbase.session.${ref}`, opts.autoRefresh ?? true)
    this.auth = auth
  }

  /** Project name and, with the secret key, its services. */
  info(): Promise<{ ref: string; name: string; services?: Array<{ kind: string; name: string; status: string; apiUrl: string }> }> {
    return this.t.request('GET', '')
  }
  postgres(name: string) {
    return new PostgresClient(this.t, name)
  }
  cache(name: string) {
    return new CacheClient(this.t, name)
  }
  storage(name: string) {
    return new StorageClient(this.t, name)
  }
  vector(name: string) {
    return new VectorClient(this.t, name)
  }
  queue(name: string) {
    return new QueueClient(this.t, name)
  }
}

/** Create a client for a project URL and a project key. */
export function createClient(url: string, key: string, opts?: ClientOptions): SparkbaseClient {
  return new SparkbaseClient(url, key, opts)
}
