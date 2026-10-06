import postgres from "postgres";

type Sql = ReturnType<typeof postgres>;

// One shared client per server process. Reused across hot reloads in dev.
const globalForDb = globalThis as unknown as { sparkbaseSql?: Sql };

export function db(): Sql {
  if (!globalForDb.sparkbaseSql) {
    const url = process.env.DATABASE_URL;
    if (!url) throw new Error("DATABASE_URL is not set.");
    globalForDb.sparkbaseSql = postgres(url, {
      max: 5,
      idle_timeout: 20,
      // Works behind transaction-mode poolers such as PgBouncer.
      prepare: false,
    });
  }
  return globalForDb.sparkbaseSql;
}
