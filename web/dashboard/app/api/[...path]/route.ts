// Forwards /api/* to the Sparkbase API so the session cookie stays on the
// dashboard's origin. The target is read at request time, so one build works
// for every install.
import type { NextRequest } from "next/server";

const HOP_BY_HOP = ["connection", "keep-alive", "transfer-encoding", "upgrade", "host", "content-length", "content-encoding"];

async function forward(req: NextRequest, ctx: { params: Promise<{ path: string[] }> }) {
  const base = (process.env.SPARKBASE_API_URL ?? "http://localhost:8787").replace(/\/$/, "");
  const { path } = await ctx.params;
  const url = `${base}/${path.map(encodeURIComponent).join("/")}${req.nextUrl.search}`;

  const headers = new Headers(req.headers);
  for (const h of HOP_BY_HOP) headers.delete(h);
  const ip = req.headers.get("x-forwarded-for") ?? "";
  if (ip) headers.set("x-forwarded-for", ip);

  const hasBody = req.method !== "GET" && req.method !== "HEAD";
  let res: Response;
  try {
    res = await fetch(url, {
      method: req.method,
      headers,
      body: hasBody ? req.body : undefined,
      redirect: "manual",
      // Required by fetch when streaming a request body.
      ...(hasBody ? { duplex: "half" } : {}),
    } as RequestInit);
  } catch {
    return Response.json(
      { error: { code: "api_unreachable", message: `The dashboard cannot reach the Sparkbase API at ${base}.`, details: {} } },
      { status: 502 },
    );
  }

  const out = new Headers(res.headers);
  for (const h of HOP_BY_HOP) out.delete(h);
  return new Response(res.body, { status: res.status, headers: out });
}

export const GET = forward;
export const POST = forward;
export const PUT = forward;
export const PATCH = forward;
export const DELETE = forward;
export const dynamic = "force-dynamic";
