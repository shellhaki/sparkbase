"use client";

import { api, formatBytes, useApi } from "@/lib/api";
import { servicePath, type Resource } from "@/lib/kinds";
import { TimeChart, type Point } from "./charts";
import { LiveLogs } from "./live-logs";
import { Terminal, psqlTable, splitArgs } from "./terminal";
import { Card, SkeletonRows } from "./ui";

export function LogsPanel({ r }: { r: Resource }) {
  const sources = [{ id: "api", label: r.runtime === "control" ? "Events" : "API calls" }];
  if (r.runtime === "container") sources.push({ id: "container", label: "Container" });
  return <LiveLogs path={`/v1/resources/${r.id}/logs`} sources={sources} />;
}

interface Metrics {
  requests: Array<{ minute: string; requests: number; errors: number; avgMs: number }>;
  samples: Record<string, Array<{ t: string; v: number }>>;
  awake?: boolean;
  live?: { memoryBytes: number; memoryLimit: number; cpuPercent: number };
}

export function MetricsPanel({ r }: { r: Resource }) {
  const { data } = useApi<Metrics>(`/v1/resources/${r.id}/metrics?minutes=60`, { poll: 15_000 });
  if (!data) return <SkeletonRows rows={6} />;
  const req: Point[] = data.requests.map((p) => ({ t: p.minute, v: p.requests }));
  const err: Point[] = data.requests.map((p) => ({ t: p.minute, v: p.errors }));
  const lat: Point[] = data.requests.map((p) => ({ t: p.minute, v: p.avgMs }));
  const s = (k: string): Point[] => (data.samples[k] ?? []).map((p) => ({ t: p.t, v: p.v }));
  const mem = s("container_memory_bytes");
  const cpu = s("cpu_permille").map((p) => ({ ...p, v: p.v / 10 }));
  const storage = s("storage_bytes").length ? s("storage_bytes") : s("memory_bytes");
  const total = data.requests.reduce((a, p) => a + p.requests, 0);
  const errors = data.requests.reduce((a, p) => a + p.errors, 0);

  return (
    <div className="space-y-6">
      <div className="grid gap-3 sm:grid-cols-4">
        {[
          { label: "Requests · 1 h", value: total.toLocaleString() },
          { label: "Error rate", value: total ? `${((errors / total) * 100).toFixed(1)}%` : "0%" },
          {
            label: r.runtime === "container" ? "State" : "Runtime",
            value: r.runtime === "container" ? (data.awake ? "Awake" : "Asleep") : r.runtime === "control" ? "Platform" : "Shared",
          },
          {
            label: "Memory now",
            value: data.live ? `${formatBytes(data.live.memoryBytes)} / ${formatBytes(data.live.memoryLimit)}` : r.runtime === "container" ? "0 B" : "—",
          },
        ].map((t) => (
          <Card key={t.label} className="p-4">
            <div className="font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase">{t.label}</div>
            <div className="tabular mt-1.5 font-display text-xl font-semibold">{t.value}</div>
          </Card>
        ))}
      </div>
      <div className="grid gap-6 lg:grid-cols-2">
        <Card className="p-5">
          <TimeChart label="Requests / min" points={req} kind="bars" />
        </Card>
        <Card className="p-5">
          <TimeChart label="Errors / min" points={err} kind="bars" />
        </Card>
        <Card className="p-5">
          <TimeChart label="Avg latency" points={lat} format={(n) => `${n} ms`} />
        </Card>
        {r.runtime === "container" && (
          <Card className="p-5">
            <TimeChart label="Container memory" points={mem} format={formatBytes} />
          </Card>
        )}
        {r.runtime === "container" && (
          <Card className="p-5">
            <TimeChart label="CPU" points={cpu} format={(n) => `${n.toFixed(1)}%`} />
          </Card>
        )}
        {storage.length > 0 && (
          <Card className="p-5">
            <TimeChart label={r.kind === "cache" ? "Data in memory" : "Storage used"} points={storage} format={formatBytes} />
          </Card>
        )}
      </div>
    </div>
  );
}

const PSQL_HELP = `Commands:
  any SQL       run one statement (no trailing ; needed)
  \\dt           list tables
  \\d TABLE      describe a table
  \\dx           list extensions
  clear         clear the screen`;

export function PostgresTerminal({ r }: { r: Resource }) {
  const base = servicePath(r);
  return (
    <Terminal
      prompt={`${r.name}=>`}
      banner={`Connected to ${r.name} as the service's own role.\nType \\? for help.`}
      run={async (input) => {
        if (input === "\\?" || input === "help") return { output: PSQL_HELP };
        let sql = input.replace(/;\s*$/, "");
        if (input === "\\dt") sql = `select table_schema as schema, table_name as name from information_schema.tables where table_schema not in ('pg_catalog','information_schema') order by 1, 2`;
        else if (input === "\\dx") sql = `select extname as name, extversion as version from pg_extension order by 1`;
        else if (input.startsWith("\\d ")) {
          const t = input.slice(3).trim().replace(/'/g, "''");
          sql = `select column_name as column, data_type as type, is_nullable as nullable, column_default as default from information_schema.columns where table_name = '${t}' order by ordinal_position`;
        } else if (input.startsWith("\\")) return { output: `unknown command ${input}; try \\?`, error: true };
        const res = await api<{ command: string; rowCount: number; fields: Array<{ name: string }>; rows: Array<Record<string, unknown>> }>("POST", `${base}/query`, { sql, rowLimit: 500 });
        if (res.fields.length === 0) return { output: `${res.command} ${res.rowCount}` };
        return { output: psqlTable(res.fields, res.rows) };
      }}
    />
  );
}

export function CacheTerminal({ r }: { r: Resource }) {
  const base = servicePath(r);
  return (
    <Terminal
      prompt={`${r.name}>`}
      banner={"Redis commands run as this service's own user. Keys get the service prefix for you.\nTry: SET greeting hello · GET greeting · TTL greeting"}
      run={async (input) => {
        const args = splitArgs(input);
        const res = await api<{ output: string }>("POST", `${base}/command`, { args });
        return { output: res.output, error: res.output.startsWith("(error)") };
      }}
    />
  );
}
