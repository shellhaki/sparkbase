# SDK

`npm install @sparkbase/sdk`. No dependencies; uses `fetch`, so it runs in Node 18+, Bun, Deno, browsers and edge runtimes.

```ts
import { createClient } from '@sparkbase/sdk'
const sb = createClient(projectUrl, key, { storage?, fetch?, headers?, autoRefresh? })
```

| Area | Methods |
| --- | --- |
| `sb.postgres(name)` | `query(sql, params?, { rowLimit? })`, `transaction(statements)`, `schema()` |
| `sb.cache(name)` | `get`, `getJSON`, `set(key, value, { ttl?, nx? })`, `del`, `incr`, `batch`, `keys` |
| `sb.storage(name)` | `list`, `upload(key, data, { contentType? })`, `download(key)` (a `Response`), `remove`, `signedUrl(key, { method?, expiresIn? })` |
| `sb.vector(name)` | `createCollection`, `listCollections`, `collection(c).upsert / query / get / delete / drop` |
| `sb.queue(name)` | `createQueue`, `listQueues`, `queue(q).send / sendBatch / receive / ack / nack / stats / dead / redrive / purge / drop` |
| `sb.auth` | `signUp`, `signInWithPassword`, `signInWithOtp`, `verifyOtp`, `recover`, `refreshSession`, `getSession`, `getAccessToken`, `getUser`, `updateUser`, `signOut`, `onAuthStateChange` |
| `sb.auth.admin` | `listUsers`, `createUser`, `getUser`, `updateUser`, `deleteUser`, `generateLink`, `getSettings`, `updateSettings` |

Errors are typed and carry the API `code`: `UnauthorizedError`, `ForbiddenError`, `NotFoundError`, `QuotaExceededError`, `RateLimitedError`, `ResourceNotReadyError`, `ValidationError`, `QueryError`, all extending `SparkbaseError`.

The auth client keeps the session in memory by default. Pass `{ storage: localStorage }` in browsers to keep users signed in across reloads. Access tokens refresh shortly before they expire.
