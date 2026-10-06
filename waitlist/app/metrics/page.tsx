import type { Metadata } from "next";
import { connection } from "next/server";
import { PageShell } from "@/components/page-shell";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { LISTS, type List } from "@/lib/lists";
import { listActiveSubscribers, type SubscriberRow } from "@/lib/subscribers";

export const metadata: Metadata = {
  title: "Metrics",
  robots: { index: false, follow: false },
};

const TITLES: Record<List, string> = { waitlist: "Waitlist", newsletter: "Newsletter" };

const formatDate = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" });

function ListSection({ list, rows }: { list: List; rows: SubscriberRow[] }) {
  return (
    <section className="border-t border-border pt-8">
      <div className="flex items-baseline justify-between gap-4">
        <h2 className="text-xl font-semibold tracking-tight">{TITLES[list]}</h2>
        <p className="font-mono text-3xl font-semibold tabular-nums">{rows.length}</p>
      </div>
      {rows.length === 0 ? (
        <p className="mt-3 text-muted-foreground">No subscribers yet.</p>
      ) : (
        <Table className="mt-4">
          <TableHeader>
            <TableRow>
              <TableHead>Email</TableHead>
              <TableHead>Source</TableHead>
              <TableHead className="text-right">Joined (UTC)</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => (
              <TableRow key={row.email}>
                <TableCell className="font-mono text-xs">{row.email}</TableCell>
                <TableCell className="text-muted-foreground">{row.source ?? "-"}</TableCell>
                <TableCell className="text-right text-muted-foreground tabular-nums">
                  {formatDate.format(row.created_at)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  );
}

// Public on purpose: the owner chose no auth for this page.
export default async function MetricsPage() {
  // Render per request; without this the build would prerender a stale snapshot of the lists.
  await connection();
  const results = await Promise.all(LISTS.map((list) => listActiveSubscribers(list)));

  return (
    <PageShell>
      <h1 className="text-5xl font-semibold tracking-[-0.04em]">Metrics</h1>
      <p className="mt-5 text-lg leading-relaxed text-muted-foreground">
        Active subscribers per list. Unsubscribed people are not shown. JSON at{" "}
        <a href="/api/metrics" className="text-foreground underline underline-offset-4 transition-colors hover:text-brand">
          /api/metrics
        </a>
        .
      </p>
      <div className="mt-14 space-y-10">
        {LISTS.map((list, i) => (
          <ListSection key={list} list={list} rows={results[i]} />
        ))}
      </div>
    </PageShell>
  );
}
