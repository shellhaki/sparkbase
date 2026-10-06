"use client";

import { Play, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { toast } from "../toast";
import { api, timeAgo, useApi } from "@/lib/api";
import { servicePath, type Resource } from "@/lib/kinds";
import { Badge, Button, Card, CardHeader, ErrorText, Field, Input, Modal, Select, Table, Td, Textarea } from "../ui";

interface Job {
  id: string;
  name: string;
  schedule: string;
  action: { type: string; url?: string; method?: string; postgres?: string; sql?: string; queue?: string; queueName?: string };
  enabled: boolean;
  nextRunAt: string;
  lastRunAt: string | null;
  lastStatus: string | null;
}

const PRESETS = [
  { label: "Every minute", value: "* * * * *" },
  { label: "Every 5 minutes", value: "*/5 * * * *" },
  { label: "Hourly", value: "0 * * * *" },
  { label: "Daily at 03:00 UTC", value: "0 3 * * *" },
  { label: "Weekdays at 09:00 UTC", value: "0 9 * * 1-5" },
];

function describe(a: Job["action"]) {
  if (a.type === "http") return `${a.method ?? "POST"} ${a.url}`;
  if (a.type === "sql") return `SQL on ${a.postgres}: ${a.sql}`;
  return `Message to ${a.queue}/${a.queueName}`;
}

export function CronBrowser({ r }: { r: Resource }) {
  const base = servicePath(r);
  const { data, reload } = useApi<{ jobs: Job[] }>(`${base}/jobs`, { poll: 15_000 });
  const [creating, setCreating] = useState(false);
  const [runsOf, setRunsOf] = useState<Job | null>(null);
  const [form, setForm] = useState({ name: "", schedule: "*/5 * * * *", type: "http", url: "", method: "POST", postgres: "main", sql: "", queue: "jobs", queueName: "", body: "" });
  const [error, setError] = useState<unknown>(null);
  const runs = useApi<{ runs: Array<{ id: number; startedAt: string; status: string; output: string | null }> }>(runsOf ? `${base}/jobs/${runsOf.id}/runs` : null);

  return (
    <Card>
      <CardHeader
        title="Jobs"
        action={
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="size-3.5" /> New job
          </Button>
        }
      />
      <div className="p-2">
        <Table head={["Job", "Schedule", "Does", "Next run", "Last", ""]}>
          {data?.jobs.map((j) => (
            <tr key={j.id}>
              <Td>
                <button className="font-medium hover:underline" onClick={() => setRunsOf(j)}>
                  {j.name}
                </button>
              </Td>
              <Td mono>{j.schedule}</Td>
              <Td className="max-w-xs truncate text-muted-foreground">{describe(j.action)}</Td>
              <Td className="whitespace-nowrap">{j.enabled ? new Date(j.nextRunAt).toLocaleString() : <Badge>paused</Badge>}</Td>
              <Td>{j.lastStatus ? <Badge tone={j.lastStatus === "ok" ? "active" : "failed"}>{j.lastStatus}</Badge> : <span className="text-muted-foreground">never</span>}</Td>
              <Td className="text-right whitespace-nowrap">
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={async () => {
                    const res = await api<{ status: string }>("POST", `${base}/jobs/${j.id}/run`);
                    toast(`${j.name}: ${res.status}`, res.status === "ok" ? "ok" : "error");
                    reload();
                  }}
                >
                  <Play className="size-3.5" /> Run
                </Button>
                <Button size="sm" variant="ghost" onClick={async () => (await api("PATCH", `${base}/jobs/${j.id}`, { enabled: !j.enabled }), reload())}>
                  {j.enabled ? "Pause" : "Enable"}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={`Delete ${j.name}`}
                  onClick={async () => {
                    if (!confirm(`Delete ${j.name}?`)) return;
                    await api("DELETE", `${base}/jobs/${j.id}`);
                    reload();
                  }}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </Td>
            </tr>
          ))}
        </Table>
        {data && data.jobs.length === 0 && <p className="p-8 text-center text-sm text-muted-foreground">No jobs yet.</p>}
      </div>

      <Modal open={creating} onClose={() => setCreating(false)} title="New job" wide>
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setError(null);
            const action =
              form.type === "http"
                ? { type: "http", url: form.url, method: form.method, body: form.body ? JSON.parse(form.body) : undefined }
                : form.type === "sql"
                  ? { type: "sql", postgres: form.postgres, sql: form.sql }
                  : { type: "queue", queue: form.queue, queueName: form.queueName, body: form.body ? JSON.parse(form.body) : {} };
            try {
              await api("POST", `${base}/jobs`, { name: form.name, schedule: form.schedule, action });
              setCreating(false);
              reload();
            } catch (err) {
              setError(err);
            }
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Name">
              <Input required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} autoFocus />
            </Field>
            <Field label="Schedule (UTC)" hint="Five fields: minute hour day month weekday.">
              <div className="flex gap-2">
                <Input required value={form.schedule} onChange={(e) => setForm({ ...form, schedule: e.target.value })} className="font-mono" />
                <Select className="w-36" value="" onChange={(e) => e.target.value && setForm({ ...form, schedule: e.target.value })}>
                  <option value="">Presets</option>
                  {PRESETS.map((p) => (
                    <option key={p.value} value={p.value}>
                      {p.label}
                    </option>
                  ))}
                </Select>
              </div>
            </Field>
          </div>
          <Field label="Action">
            <Select value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value })}>
              <option value="http">Call a URL</option>
              <option value="sql">Run SQL on a Postgres service</option>
              <option value="queue">Send a message to a queue</option>
            </Select>
          </Field>
          {form.type === "http" && (
            <div className="grid gap-4 sm:grid-cols-[120px_1fr]">
              <Select value={form.method} onChange={(e) => setForm({ ...form, method: e.target.value })}>
                {["POST", "GET", "PUT", "PATCH", "DELETE"].map((m) => (
                  <option key={m}>{m}</option>
                ))}
              </Select>
              <Input required type="url" placeholder="https://myapp.com/api/tick" value={form.url} onChange={(e) => setForm({ ...form, url: e.target.value })} />
            </div>
          )}
          {form.type === "sql" && (
            <>
              <Field label="Postgres service name">
                <Input value={form.postgres} onChange={(e) => setForm({ ...form, postgres: e.target.value })} />
              </Field>
              <Field label="SQL">
                <Textarea rows={3} value={form.sql} onChange={(e) => setForm({ ...form, sql: e.target.value })} placeholder="delete from sessions where expires_at < now()" />
              </Field>
            </>
          )}
          {form.type === "queue" && (
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Queue service">
                <Input value={form.queue} onChange={(e) => setForm({ ...form, queue: e.target.value })} />
              </Field>
              <Field label="Queue name">
                <Input value={form.queueName} onChange={(e) => setForm({ ...form, queueName: e.target.value })} />
              </Field>
            </div>
          )}
          {form.type !== "sql" && (
            <Field label="Body (JSON, optional)">
              <Textarea rows={3} value={form.body} onChange={(e) => setForm({ ...form, body: e.target.value })} />
            </Field>
          )}
          <ErrorText error={error} />
          <div className="flex justify-end">
            <Button type="submit">Create job</Button>
          </div>
        </form>
      </Modal>

      <Modal open={!!runsOf} onClose={() => setRunsOf(null)} title={`Runs · ${runsOf?.name ?? ""}`} wide>
        <div className="max-h-[60vh] space-y-2 overflow-y-auto">
          {runs.data?.runs.length === 0 && <p className="text-sm text-muted-foreground">No runs yet.</p>}
          {runs.data?.runs.map((x) => (
            <div key={x.id} className="rounded-lg border border-border p-3">
              <div className="flex items-center justify-between text-sm">
                <Badge tone={x.status === "ok" ? "active" : x.status === "running" ? "running" : "failed"}>{x.status}</Badge>
                <span className="font-mono text-xs text-muted-foreground">{timeAgo(x.startedAt)}</span>
              </div>
              {x.output && <pre className="mt-2 max-h-40 overflow-auto rounded bg-code p-2 font-mono text-xs whitespace-pre-wrap">{x.output}</pre>}
            </div>
          ))}
        </div>
      </Modal>
    </Card>
  );
}
