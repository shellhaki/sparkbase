import { db } from "./db";
import type { List } from "./lists";

export const RATE_LIMIT_PER_MINUTE = 5;

export async function addSubscriber(input: {
  email: string;
  list: List;
  source: string | null;
  ipHash: string;
}): Promise<void> {
  await db()`
    insert into subscribers (email, list, source, ip_hash)
    values (${input.email}, ${input.list}, ${input.source}, ${input.ipHash})
    on conflict (email, list) do nothing
  `;
}

/** Counts this request in the current one-minute window and returns the new total. */
export async function hitRateLimit(ipHash: string): Promise<number> {
  const [row] = await db()<{ count: number }[]>`
    insert into rate_limits (ip_hash, window_start)
    values (${ipHash}, date_trunc('minute', now()))
    on conflict (ip_hash, window_start)
      do update set count = rate_limits.count + 1
    returning count
  `;
  return row.count;
}

export async function deleteOldRateLimits(): Promise<void> {
  await db()`
    delete from rate_limits
    where window_start < now() - interval '5 minutes'
  `;
}

export type SubscriberRow = {
  email: string;
  source: string | null;
  created_at: Date;
};

/**
 * Active subscribers of one list, oldest first. Never selects unsubscribe_token or ip_hash:
 * this feeds the public /metrics page and API, and a leaked token lets anyone unsubscribe that person.
 */
export async function listActiveSubscribers(list: List): Promise<SubscriberRow[]> {
  return db()<SubscriberRow[]>`
    select email, source, created_at
    from subscribers
    where list = ${list}
      and unsubscribed_at is null
    order by created_at
  `;
}

export type TokenStatus = "unknown" | "active" | "unsubscribed";

export async function getTokenStatus(token: string): Promise<TokenStatus> {
  const [row] = await db()<{ unsubscribed_at: Date | null }[]>`
    select unsubscribed_at
    from subscribers
    where unsubscribe_token = ${token}
  `;
  if (!row) return "unknown";
  return row.unsubscribed_at ? "unsubscribed" : "active";
}

/** Returns true when the token matches a row. Already unsubscribed rows keep their original timestamp. */
export async function unsubscribe(token: string): Promise<boolean> {
  const rows = await db()`
    update subscribers
    set unsubscribed_at = coalesce(unsubscribed_at, now())
    where unsubscribe_token = ${token}
    returning id
  `;
  return rows.length > 0;
}
