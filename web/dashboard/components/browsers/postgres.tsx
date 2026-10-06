"use client";

import { Play, Table2 } from "lucide-react";
import { useState } from "react";
import { api, formatBytes, useApi } from "@/lib/api";
import { servicePath, type Resource } from "@/lib/kinds";
import { Button, Card, CardHeader, ErrorText, Table, Td, Textarea, cn } from "../ui";

interface QueryResult {
  command: string;
  rowCount: number;
  fields: Array<{ name: string; type: string }>;
  rows: Array<Record<string, unknown>>;
  truncated?: boolean;
}

interface TableInfo {
  schema: string;
  name: string;
  estimatedRows: number;
  bytes: number;
  columns: Array<{ name: string; type: string; nullable: boolean }>;
}

function cell(v: unknown): string {
  if (v === null || v === undefined) return "NULL";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

function quoteIdent(s: string) {
  return `"${s.replace(/"/g, '""')}"`;
}

export function PostgresBrowser({ r }: { r: Resource }) {
  const base = servicePath(r);
  const { data: schema, reload: reloadSchema } = useApi<{ tables: TableInfo[] }>(`${base}/schema`);
  const [sql, setSql] = useState("select now();");
  const [result, setResult] = useState<QueryResult | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [ms, setMs] = useState(0);
  const [active, setActive] = useState<string | null>(null);

  const run = async (text = sql) => {
    setBusy(true);
    setError(null);
    const t = performance.now();
    try {
      const res = await api<QueryResult>("POST", `${base}/query`, { sql: text.trim().replace(/;\s*$/, ""), rowLimit: 500 });
      setResult(res);
      if (/^(create|drop|alter)/i.test(text.trim())) reloadSchema();
    } catch (e) {
      setResult(null);
      setError(e);
    } finally {
      setMs(Math.round(performance.now() - t));
      setBusy(false);
    }
  };

  return (
    <div className="grid gap-6 lg:grid-cols-4">
      <Card className="lg:col-span-1">
        <CardHeader title="Tables" />
        <div className="max-h-[480px] overflow-y-auto p-2">
          {schema?.tables.length === 0 && <p className="p-3 text-sm text-muted-foreground">No tables yet. Create one with SQL.</p>}
          {schema?.tables.map((t) => {
            const id = `${t.schema}.${t.name}`;
            return (
              <button
                key={id}
                onClick={() => {
                  const q = `select * from ${t.schema === "public" ? "" : quoteIdent(t.schema) + "."}${quoteIdent(t.name)} limit 100;`;
                  setActive(id);
                  setSql(q);
                  run(q);
                }}
                className={cn("flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm hover:bg-accent", active === id && "bg-accent")}
              >
                <Table2 className="size-4 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate font-mono">{t.schema === "public" ? t.name : id}</span>
                <span className="text-xs text-muted-foreground">{formatBytes(t.bytes)}</span>
              </button>
            );
          })}
        </div>
      </Card>
      <div className="space-y-4 lg:col-span-3">
        <Card>
          <div className="p-3">
            <Textarea
              value={sql}
              onChange={(e) => setSql(e.target.value)}
              onKeyDown={(e) => {
                if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                  e.preventDefault();
                  run();
                }
              }}
              rows={6}
              spellCheck={false}
              aria-label="SQL"
            />
            <div className="mt-3 flex items-center justify-between gap-3">
              <span className="text-xs text-muted-foreground">One statement per run, as the service's own role. ⌘/Ctrl + Enter runs it.</span>
              <Button size="sm" onClick={() => run()} loading={busy}>
                <Play className="size-3.5" /> Run
              </Button>
            </div>
          </div>
        </Card>
        <ErrorText error={error} />
        {result && (
          <Card>
            <div className="flex items-center justify-between border-b border-border px-4 py-2 text-xs text-muted-foreground">
              <span>
                {result.command} · {result.fields.length > 0 ? `${result.rows.length} rows` : `${result.rowCount} affected`}
                {result.truncated && " (truncated)"}
              </span>
              <span>{ms} ms</span>
            </div>
            {result.fields.length > 0 && (
              <div className="max-h-[480px] overflow-auto">
                <Table
                  head={result.fields.map((f) => (
                    <span key={f.name}>
                      {f.name} <span className="font-normal lowercase opacity-60">{f.type}</span>
                    </span>
                  ))}
                >
                  {result.rows.map((row, i) => (
                    <tr key={i}>
                      {result.fields.map((f) => (
                        <Td key={f.name} mono className={cn("max-w-xs truncate whitespace-nowrap", row[f.name] === null && "text-muted-foreground")}>
                          {cell(row[f.name])}
                        </Td>
                      ))}
                    </tr>
                  ))}
                </Table>
              </div>
            )}
          </Card>
        )}
      </div>
    </div>
  );
}
