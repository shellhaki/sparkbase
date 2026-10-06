"use client";

import { useEffect, useRef, useState } from "react";
import { Card, cn } from "./ui";

interface Entry {
  id: number;
  input?: string;
  output: string;
  error?: boolean;
}

/**
 * A small terminal: type a command, press Enter, see the output. History
 * with the up and down arrows; Ctrl+L or "clear" clears the screen.
 */
export function Terminal({
  prompt,
  banner,
  run,
}: {
  prompt: string;
  banner?: string;
  run: (input: string) => Promise<{ output: string; error?: boolean }>;
}) {
  const [entries, setEntries] = useState<Entry[]>(banner ? [{ id: 0, output: banner }] : []);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [history, setHistory] = useState<string[]>([]);
  const [cursor, setCursor] = useState(-1);
  const seq = useRef(1);
  const box = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLInputElement>(null);

  useEffect(() => {
    box.current?.scrollTo({ top: box.current.scrollHeight });
  }, [entries, busy]);

  const submit = async () => {
    const cmd = input.trim();
    if (!cmd || busy) return;
    setInput("");
    setCursor(-1);
    setHistory((h) => [cmd, ...h.filter((x) => x !== cmd)].slice(0, 100));
    if (cmd === "clear") {
      setEntries([]);
      return;
    }
    setBusy(true);
    try {
      const res = await run(cmd);
      setEntries((e) => [...e, { id: seq.current++, input: cmd, output: res.output, error: res.error }]);
    } catch (err) {
      setEntries((e) => [...e, { id: seq.current++, input: cmd, output: err instanceof Error ? err.message : String(err), error: true }]);
    } finally {
      setBusy(false);
      setTimeout(() => field.current?.focus(), 0);
    }
  };

  return (
    <Card className="overflow-hidden">
      <div ref={box} className="h-[460px] overflow-y-auto bg-code p-4 font-mono text-[13px] leading-relaxed" onClick={() => field.current?.focus()}>
        {entries.map((e) => (
          <div key={e.id} className="mb-2">
            {e.input !== undefined && (
              <div>
                <span className="text-brand">{prompt}</span> {e.input}
              </div>
            )}
            {e.output && <pre className={cn("whitespace-pre-wrap", e.error ? "text-destructive" : "text-foreground/90")}>{e.output}</pre>}
          </div>
        ))}
        <div className="flex items-center gap-2">
          <span className="shrink-0 text-brand">{prompt}</span>
          <input
            ref={field}
            value={input}
            disabled={busy}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
              else if (e.key === "ArrowUp") {
                e.preventDefault();
                const n = Math.min(cursor + 1, history.length - 1);
                if (n >= 0) {
                  setCursor(n);
                  setInput(history[n]);
                }
              } else if (e.key === "ArrowDown") {
                e.preventDefault();
                const n = cursor - 1;
                setCursor(n);
                setInput(n >= 0 ? history[n] : "");
              } else if (e.key === "l" && e.ctrlKey) {
                e.preventDefault();
                setEntries([]);
              }
            }}
            className="min-w-0 flex-1 bg-transparent outline-none disabled:opacity-60"
            spellCheck={false}
            autoComplete="off"
            aria-label="Command"
          />
          {busy && <span className="text-muted-foreground">…</span>}
        </div>
      </div>
    </Card>
  );
}

/** Formats query rows like psql's aligned output. */
export function psqlTable(fields: Array<{ name: string }>, rows: Array<Record<string, unknown>>): string {
  const cell = (v: unknown) => (v === null || v === undefined ? "" : typeof v === "object" ? JSON.stringify(v) : String(v));
  const cols = fields.map((f) => f.name);
  const widths = cols.map((c) => Math.max(c.length, ...rows.map((r) => Math.min(60, cell(r[c]).length))));
  const fit = (s: string, w: number) => (s.length > w ? s.slice(0, w - 1) + "…" : s.padEnd(w));
  const head = " " + cols.map((c, i) => fit(c, widths[i])).join(" | ");
  const line = cols.map((_, i) => "-".repeat(widths[i] + 2)).join("+");
  const body = rows.map((r) => " " + cols.map((c, i) => fit(cell(r[c]), widths[i])).join(" | "));
  return [head, line, ...body, `(${rows.length} row${rows.length === 1 ? "" : "s"})`].join("\n");
}

/** Splits a command line into arguments, honoring quotes. */
export function splitArgs(s: string): string[] {
  const out: string[] = [];
  const re = /"((?:[^"\\]|\\.)*)"|'([^']*)'|(\S+)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(s))) out.push(m[1] !== undefined ? m[1].replace(/\\(.)/g, "$1") : (m[2] ?? m[3]));
  return out;
}
