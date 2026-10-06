"use client";

import { Pause, Play, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Button, Card, Input, cn } from "./ui";

interface Line {
  key: number;
  time: string;
  level: string;
  source: string;
  text: string;
  fields?: Record<string, unknown>;
}

const MAX = 1000;

function fmtTime(t: string) {
  const d = new Date(t);
  return isNaN(d.getTime()) ? "" : d.toLocaleTimeString([], { hour12: false });
}

function fieldText(fields?: Record<string, unknown>) {
  if (!fields) return "";
  return Object.entries(fields)
    .filter(([, v]) => v !== null && v !== undefined && v !== "")
    .map(([k, v]) => `${k}=${typeof v === "object" ? JSON.stringify(v) : v}`)
    .join(" ");
}

/** A live tail over Server-Sent Events from an API path supporting ?follow=1. */
export function LiveLogs({ path, sources }: { path: string; sources?: Array<{ id: string; label: string }> }) {
  const [source, setSource] = useState(sources?.[0]?.id ?? "");
  const [lines, setLines] = useState<Line[]>([]);
  const [paused, setPaused] = useState(false);
  const [filter, setFilter] = useState("");
  const [state, setState] = useState<"connecting" | "live" | "sleeping" | "error">("connecting");
  const box = useRef<HTMLDivElement>(null);
  const pausedRef = useRef(false);
  const buffer = useRef<Line[]>([]);
  const seq = useRef(0);
  pausedRef.current = paused;

  useEffect(() => {
    setLines([]);
    setState("connecting");
    const q = new URLSearchParams({ follow: "1", tail: "200" });
    if (source) q.set("source", source);
    const es = new EventSource(`/api${path}?${q}`, { withCredentials: true });
    const push = (l: Omit<Line, "key">) => {
      const line = { ...l, key: ++seq.current };
      if (pausedRef.current) buffer.current.push(line);
      else setLines((prev) => [...prev, line].slice(-MAX));
    };
    es.addEventListener("ready", () => setState("live"));
    es.addEventListener("log", (e) => {
      const d = JSON.parse((e as MessageEvent).data);
      push({ time: d.time, level: d.level ?? "info", source: d.source ?? "", text: d.message, fields: d.fields });
    });
    es.addEventListener("line", (e) => {
      const d = JSON.parse((e as MessageEvent).data);
      push({ time: d.time, level: d.stream === "stderr" ? "warn" : "info", source: d.stream, text: d.text });
    });
    es.addEventListener("state", (e) => setState(JSON.parse((e as MessageEvent).data).running ? "live" : "sleeping"));
    es.onerror = () => setState("error");
    return () => es.close();
  }, [path, source]);

  useEffect(() => {
    const el = box.current;
    if (el && !paused && el.scrollHeight - el.scrollTop - el.clientHeight < 120) el.scrollTop = el.scrollHeight;
  }, [lines, paused]);

  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return f ? lines.filter((l) => (l.text + " " + fieldText(l.fields)).toLowerCase().includes(f)) : lines;
  }, [lines, filter]);

  const dot = state === "live" ? "text-success" : state === "sleeping" ? "text-muted-foreground" : state === "error" ? "text-destructive" : "text-warning";

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2.5">
        <span className="flex items-center gap-2 font-mono text-xs text-muted-foreground">
          <span className={cn("led !size-1.5", dot)} data-live={state === "live"} />
          {state === "sleeping" ? "asleep, waiting for it to wake" : state}
        </span>
        {sources && sources.length > 1 && (
          <div className="ml-2 inline-flex rounded-lg border border-border p-0.5">
            {sources.map((s) => (
              <button
                key={s.id}
                onClick={() => setSource(s.id)}
                className={cn("rounded-md px-2.5 py-1 text-xs font-medium transition-colors", source === s.id ? "bg-accent text-foreground" : "text-muted-foreground hover:text-foreground")}
              >
                {s.label}
              </button>
            ))}
          </div>
        )}
        <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" className="ml-auto h-8 w-48 text-xs" />
        <Button
          size="sm"
          variant="ghost"
          aria-label={paused ? "Resume" : "Pause"}
          onClick={() => {
            if (paused && buffer.current.length) {
              const b = buffer.current;
              buffer.current = [];
              setLines((prev) => [...prev, ...b].slice(-MAX));
            }
            setPaused(!paused);
          }}
        >
          {paused ? <Play className="size-3.5" /> : <Pause className="size-3.5" />}
        </Button>
        <Button size="sm" variant="ghost" onClick={() => setLines([])} aria-label="Clear">
          <Trash2 className="size-3.5" />
        </Button>
      </div>
      <div ref={box} className="h-[460px] overflow-y-auto bg-code py-2 font-mono text-[12.5px] leading-6">
        {shown.length === 0 && <div className="px-4 py-10 text-center text-muted-foreground">{state === "live" ? "Waiting for log lines…" : " "}</div>}
        {shown.map((l) => (
          <div key={l.key} className="flex gap-3 px-4 hover:bg-accent/40">
            <span className="tabular shrink-0 text-muted-foreground">{fmtTime(l.time)}</span>
            <span className={cn("w-11 shrink-0 uppercase", l.level === "error" ? "text-destructive" : l.level === "warn" ? "text-warning" : "text-muted-foreground")}>{l.level}</span>
            {l.source && <span className="w-16 shrink-0 truncate text-brand">{l.source}</span>}
            <span className="min-w-0 flex-1 break-all whitespace-pre-wrap">
              {l.text}
              {l.fields && <span className="text-muted-foreground">{"  " + fieldText(l.fields)}</span>}
            </span>
          </div>
        ))}
      </div>
    </Card>
  );
}
