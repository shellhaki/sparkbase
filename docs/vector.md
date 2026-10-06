---
title: Vector
description: Collections of embeddings with similarity search on pgvector.
---

```ts
const docs = sb.vector('docs')
await docs.createCollection({ name: 'pages', dimension: 1536, metric: 'cosine' })
await docs.collection('pages').upsert([{ id: 'home', vector: embedding, metadata: { lang: 'en' } }])
const hits = await docs.collection('pages').query(queryEmbedding, { topK: 5, filter: { lang: 'en' } })
```

Metrics: `cosine` (default), `l2`, `inner_product`. Collections up to 2000 dimensions get an HNSW index. `filter` matches metadata with JSON containment. The data lives in a Postgres database with pgvector, so the native connection string works with any pgvector client too.
