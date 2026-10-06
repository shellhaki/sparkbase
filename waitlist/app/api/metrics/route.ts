import { LISTS } from "@/lib/lists";
import { listActiveSubscribers } from "@/lib/subscribers";

// Public on purpose: the owner chose no auth for this endpoint.
export async function GET() {
  try {
    const results = await Promise.all(LISTS.map((list) => listActiveSubscribers(list)));
    const body = Object.fromEntries(
      LISTS.map((list, i) => [
        list,
        {
          count: results[i].length,
          subscribers: results[i].map((row) => ({
            email: row.email,
            source: row.source,
            createdAt: row.created_at.toISOString(),
          })),
        },
      ]),
    );
    return Response.json(body, { headers: { "Cache-Control": "no-store" } });
  } catch (error) {
    // Never log the error object itself: postgres.js errors can carry query parameters.
    const code = (error as { code?: string }).code ?? "unknown";
    console.error(`metrics failed: ${code}`);
    return Response.json({ ok: false, error: "server" }, { status: 500, headers: { "Cache-Control": "no-store" } });
  }
}
