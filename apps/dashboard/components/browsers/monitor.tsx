"use client";

import { Play, Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "../toast";
import { api, timeAgo, useApi } from "@/lib/api";
import { servicePath, type Resource } from "@/lib/kinds";
import { Button, Card, CardHeader, ErrorText, Field, Input, Modal, Select, cn } from "../ui";

interface Result {
  checkedAt: string;
  ok: boolean;
  status: number | null;
  latencyMs: number | null;
  error: string | null;
}

interface Check {
  check: {
    id: string;
    name: string;
    url: string;
    method: string;
    intervalSeconds: number;
    enabled: boolean;
    state: string;
  };
  uptime24h: number;
  uptime7d: number;
  recent: Result[];
}

function pct(n: number) {
  return n < 0 ? "—" : `${(n * 100).toFixed(n === 1 ? 0 : 2)}%`;
}

/** The last checks as a strip of bars, newest on the right. */
function UptimeStrip({ results }: { results: Result[] }) {
  const slots = 60;
  const list = [...results].reverse().slice(-slots);
  const pad = Array.from({ length: slots - list.length });
  return (
    <div className="flex h-7 items-end gap-[3px]" aria-label="Recent checks">
      {pad.map((_, i) => (
        <span key={"p" + i} className="h-full flex-1 rounded-sm bg-muted" />
      ))}
      {list.map((r, i) => (
        <span
          key={i}
          title={`${new Date(r.checkedAt).toLocaleString()} · ${r.ok ? "up" : "down"}${r.latencyMs !== null ? ` · ${r.latencyMs} ms` : ""}${r.error ? ` · ${r.error}` : ""}`}
          className={cn("h-full flex-1 rounded-sm", r.ok ? "bg-brand" : "bg-destructive")}
        />
      ))}
    </div>
  );
}

export function MonitorBrowser({ r }: { r: Resource }) {
  const base = servicePath(r);
  const { data, reload } = useApi<{
    checks: Check[];
    settings: { alertWebhook?: string };
  }>(`${base}/checks`, { poll: 15_000 });
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState({
    name: "",
    url: "",
    method: "GET",
    intervalSeconds: "60",
    expectStatus: "200",
  });
  const [webhook, setWebhook] = useState("");
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    if (data) setWebhook(data.settings.alertWebhook ?? "");
  }, [data]);

  return (
    <div className="space-y-6">
      <div className="flex justify-end">
        <Button onClick={() => setCreating(true)}>
          <Plus className="size-4" /> New check
        </Button>
      </div>
      {data?.checks.length === 0 && (
        <Card>
          <p className="p-10 text-center text-sm text-muted-foreground">Add a check to watch your app&apos;s health endpoint.</p>
        </Card>
      )}
      {data?.checks.map((c) => {
        const last = c.recent[0];
        return (
          <Card key={c.check.id} className="p-5">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span
                    className={cn("led !size-2", c.check.state === "up" ? "text-success" : c.check.state === "down" ? "text-destructive" : "text-muted-foreground")}
                    data-live={c.check.state === "up"}
                  />
                  <span className="font-medium">{c.check.name}</span>
                  {!c.check.enabled && <span className="text-xs text-muted-foreground">paused</span>}
                </div>
                <div className="mt-1 truncate font-mono text-xs text-muted-foreground">
                  {c.check.method} {c.check.url} · every {c.check.intervalSeconds}s
                </div>
              </div>
              <div className="flex gap-6 text-right">
                <div>
                  <div className="font-mono text-[10px] tracking-[0.14em] text-muted-foreground uppercase">24 h</div>
                  <div className="tabular font-display text-lg font-semibold">{pct(c.uptime24h)}</div>
                </div>
                <div>
                  <div className="font-mono text-[10px] tracking-[0.14em] text-muted-foreground uppercase">7 d</div>
                  <div className="tabular font-display text-lg font-semibold">{pct(c.uptime7d)}</div>
                </div>
                <div>
                  <div className="font-mono text-[10px] tracking-[0.14em] text-muted-foreground uppercase">Latency</div>
                  <div className="tabular font-display text-lg font-semibold">{last?.latencyMs != null ? `${last.latencyMs} ms` : "—"}</div>
                </div>
              </div>
            </div>
            <div className="mt-4">
              <UptimeStrip results={c.recent} />
              <div className="mt-2 flex items-center justify-between text-xs text-muted-foreground">
                <span>{last ? `last check ${timeAgo(last.checkedAt)}${last.error ? ` · ${last.error}` : ""}` : "not checked yet"}</span>
                <span className="flex gap-1">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={async () => {
                      const res = await api<{ result: Result }>("POST", `${base}/checks/${c.check.id}/run`);
                      toast(res.result.ok ? `${c.check.name} is up` : `${c.check.name} is down`, res.result.ok ? "ok" : "error");
                      reload();
                    }}
                  >
                    <Play className="size-3.5" /> Check now
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={async () => (
                      await api("PATCH", `${base}/checks/${c.check.id}`, {
                        enabled: !c.check.enabled,
                      }),
                      reload()
                    )}
                  >
                    {c.check.enabled ? "Pause" : "Resume"}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-label={`Delete ${c.check.name}`}
                    onClick={async () => {
                      if (!confirm(`Delete ${c.check.name}?`)) return;
                      await api("DELETE", `${base}/checks/${c.check.id}`);
                      reload();
                    }}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </span>
              </div>
            </div>
          </Card>
        );
      })}

      <Card>
        <CardHeader title="Alerts" />
        <div className="p-5">
          <label htmlFor="alert-webhook" className="mb-1.5 block text-sm font-medium">
            Webhook
          </label>
          <div className="flex gap-2">
            <Input id="alert-webhook" type="url" value={webhook} onChange={(e) => setWebhook(e.target.value)} placeholder="https://hooks.example.com/sparkbase" />
            <Button
              variant="secondary"
              onClick={async () => {
                try {
                  await api("PATCH", `${base}/settings`, {
                    alertWebhook: webhook,
                  });
                  toast("Saved");
                } catch (e) {
                  toast(e instanceof Error ? e.message : "Failed", "error");
                }
              }}
            >
              Save
            </Button>
          </div>
          <p className="mt-1.5 text-xs text-muted-foreground">We POST JSON here when a check goes down or comes back. Owners also get a notification.</p>
        </div>
      </Card>

      <Modal open={creating} onClose={() => setCreating(false)} title="New check">
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setError(null);
            try {
              await api("POST", `${base}/checks`, {
                name: form.name,
                url: form.url,
                method: form.method,
                intervalSeconds: Number(form.intervalSeconds),
                expectStatus: Number(form.expectStatus),
              });
              setCreating(false);
              reload();
            } catch (err) {
              setError(err);
            }
          }}
        >
          <Field label="Name">
            <Input required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} autoFocus placeholder="API health" />
          </Field>
          <div className="grid grid-cols-[100px_1fr] gap-2">
            <Select value={form.method} onChange={(e) => setForm({ ...form, method: e.target.value })}>
              <option>GET</option>
              <option>HEAD</option>
              <option>POST</option>
            </Select>
            <Input required type="url" value={form.url} onChange={(e) => setForm({ ...form, url: e.target.value })} placeholder="https://myapp.com/health" />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Field label="Every (seconds)">
              <Input type="number" min={10} value={form.intervalSeconds} onChange={(e) => setForm({ ...form, intervalSeconds: e.target.value })} />
            </Field>
            <Field label="Expected status">
              <Input type="number" value={form.expectStatus} onChange={(e) => setForm({ ...form, expectStatus: e.target.value })} />
            </Field>
          </div>
          <ErrorText error={error} />
          <div className="flex justify-end">
            <Button type="submit">Create check</Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
