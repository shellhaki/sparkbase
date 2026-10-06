"use client";

import { Plus, RotateCcw, Send, Trash2 } from "lucide-react";
import { useState } from "react";
import { api, timeAgo, useApi } from "@/lib/api";
import { servicePath, type Resource } from "@/lib/kinds";
import { Button, Card, CardHeader, ErrorText, Field, Input, Modal, Table, Td, Textarea } from "../ui";

interface QueueRow {
  name: string;
  visibilityTimeout: number;
  maxReceives: number;
  createdAt: string;
  ready: number;
  inFlight: number;
  dead: number;
}

export function QueueBrowser({ r }: { r: Resource }) {
  const base = `${servicePath(r)}/queues`;
  const { data, reload, error } = useApi<{ queues: QueueRow[] }>(base, { poll: 5000 });
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState({ name: "", visibilityTimeout: "30", maxReceives: "5" });
  const [sending, setSending] = useState<string | null>(null);
  const [body, setBody] = useState('{"hello": "world"}');
  const [actionError, setActionError] = useState<unknown>(null);

  const act = async (fn: () => Promise<unknown>) => {
    setActionError(null);
    try {
      await fn();
      reload();
    } catch (e) {
      setActionError(e);
    }
  };

  return (
    <Card>
      <CardHeader
        title="Queues"
        description="Messages become visible again after the visibility timeout unless acked. After max receives they move to dead letters."
        action={
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="size-3.5" /> New queue
          </Button>
        }
      />
      <div className="p-2">
        <ErrorText error={error ?? actionError} />
        <Table head={["Name", "Ready", "In flight", "Dead", "Timeout", "Max receives", ""]}>
          {data?.queues.map((q) => (
            <tr key={q.name}>
              <Td mono>{q.name}</Td>
              <Td>{q.ready}</Td>
              <Td>{q.inFlight}</Td>
              <Td className={q.dead > 0 ? "text-destructive" : undefined}>{q.dead}</Td>
              <Td>{q.visibilityTimeout} s</Td>
              <Td>{q.maxReceives}</Td>
              <Td className="text-right whitespace-nowrap">
                <Button size="sm" variant="ghost" onClick={() => setSending(q.name)}>
                  <Send className="size-3.5" /> Send
                </Button>
                {q.dead > 0 && (
                  <Button size="sm" variant="ghost" onClick={() => act(() => api("POST", `${base}/${q.name}/redrive`))}>
                    <RotateCcw className="size-3.5" /> Redrive
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={`Delete queue ${q.name}`}
                  onClick={() => {
                    if (confirm(`Delete queue ${q.name} and its messages?`)) act(() => api("DELETE", `${base}/${q.name}`));
                  }}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </Td>
            </tr>
          ))}
        </Table>
        {data?.queues.length === 0 && <p className="p-8 text-center text-sm text-muted-foreground">No queues yet.</p>}
        {data && data.queues.length > 0 && <p className="px-4 py-2 text-xs text-muted-foreground">Updated {timeAgo(new Date().toISOString())}, refreshes every 5 s.</p>}
      </div>

      <Modal open={creating} onClose={() => setCreating(false)} title="New queue">
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            await act(async () => {
              await api("POST", base, { name: form.name, visibilityTimeout: Number(form.visibilityTimeout), maxReceives: Number(form.maxReceives) });
              setCreating(false);
            });
          }}
        >
          <Field label="Name">
            <Input required pattern="[a-z][a-z0-9_-]*" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} autoFocus />
          </Field>
          <div className="grid grid-cols-2 gap-4">
            <Field label="Visibility timeout (s)">
              <Input type="number" min={1} max={43200} value={form.visibilityTimeout} onChange={(e) => setForm({ ...form, visibilityTimeout: e.target.value })} />
            </Field>
            <Field label="Max receives">
              <Input type="number" min={1} max={1000} value={form.maxReceives} onChange={(e) => setForm({ ...form, maxReceives: e.target.value })} />
            </Field>
          </div>
          <ErrorText error={actionError} />
          <div className="flex justify-end">
            <Button type="submit">Create</Button>
          </div>
        </form>
      </Modal>

      <Modal open={!!sending} onClose={() => setSending(null)} title={`Send to ${sending ?? ""}`}>
        <div className="space-y-4">
          <Field label="Message body (JSON)">
            <Textarea rows={5} value={body} onChange={(e) => setBody(e.target.value)} />
          </Field>
          <ErrorText error={actionError} />
          <div className="flex justify-end">
            <Button
              onClick={() =>
                act(async () => {
                  await api("POST", `${base}/${sending}/send`, { messages: [{ body: JSON.parse(body) }] });
                  setSending(null);
                })
              }
            >
              Send
            </Button>
          </div>
        </div>
      </Modal>
    </Card>
  );
}
