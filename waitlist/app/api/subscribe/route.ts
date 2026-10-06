import { createHmac } from "node:crypto";
import { isList, normalizeEmail, SOURCE_MAX_LENGTH } from "@/lib/lists";
import {
  addSubscriber,
  deleteOldRateLimits,
  hitRateLimit,
  RATE_LIMIT_PER_MINUTE,
} from "@/lib/subscribers";

const MAX_BODY_BYTES = 2048;

function json(status: number, body: object) {
  return Response.json(body, { status, headers: { "Cache-Control": "no-store" } });
}

/** Accepts only browser requests whose Origin matches the host serving this route. */
function isSameOrigin(request: Request): boolean {
  const fetchSite = request.headers.get("sec-fetch-site");
  if (fetchSite && fetchSite !== "same-origin") return false;

  const origin = request.headers.get("origin");
  if (!origin) return false;
  const host = request.headers.get("x-forwarded-host") ?? request.headers.get("host");
  try {
    return new URL(origin).host === host;
  } catch {
    return false;
  }
}

function clientIp(request: Request): string {
  const forwarded = request.headers.get("x-forwarded-for");
  if (forwarded) return forwarded.split(",")[0].trim();
  return request.headers.get("x-real-ip")?.trim() || "unknown";
}

function hashIp(ip: string): string {
  const salt = process.env.IP_HASH_SALT;
  if (!salt) throw new Error("IP_HASH_SALT is not set.");
  return createHmac("sha256", salt).update(ip).digest("hex");
}

export async function POST(request: Request) {
  if (!isSameOrigin(request)) return json(403, { ok: false, error: "forbidden" });

  let body: Record<string, unknown>;
  try {
    const text = await request.text();
    if (text.length > MAX_BODY_BYTES) return json(422, { ok: false, error: "invalid" });
    const parsed: unknown = JSON.parse(text);
    if (typeof parsed !== "object" || parsed === null) throw new Error();
    body = parsed as Record<string, unknown>;
  } catch {
    return json(422, { ok: false, error: "invalid" });
  }

  const email = normalizeEmail(body.email);
  if (!email || !isList(body.list)) return json(422, { ok: false, error: "invalid" });

  // Honeypot: bots get the normal response and nothing is stored.
  if (typeof body.website === "string" && body.website.trim() !== "") {
    return json(200, { ok: true });
  }

  const source =
    typeof body.source === "string" && body.source.trim() !== ""
      ? body.source.trim().slice(0, SOURCE_MAX_LENGTH)
      : null;

  try {
    const ipHash = hashIp(clientIp(request));
    const [count] = await Promise.all([hitRateLimit(ipHash), deleteOldRateLimits()]);
    if (count > RATE_LIMIT_PER_MINUTE) {
      return json(429, { ok: false, error: "rate_limited" });
    }

    // Same response for new and existing rows, so sign-ups can't be probed.
    await addSubscriber({ email, list: body.list, source, ipHash });
    return json(200, { ok: true });
  } catch (error) {
    // Never log the error object itself: postgres.js errors carry query parameters (the email).
    const code = (error as { code?: string }).code ?? "unknown";
    console.error(`subscribe failed: ${code}`);
    return json(500, { ok: false, error: "server" });
  }
}
