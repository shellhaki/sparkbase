"use client";

import { Plus, Radio, Send, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, useApi } from "@/lib/api";
import { servicePath, type Resource } from "@/lib/kinds";
import { Button, Card, CardHeader, ErrorText, Field, Input, Select, Table, Td, Textarea } from "../ui";

interface Msg {
  channel: string;
  event: string;
  payload: unknown;
  at: string;
}

/** Send and watch channel messages, and turn on table change feeds. */
export function RealtimeBrowser({ r }: { r: Resource }) {
  const base = servicePath(r);
  const { data, reload } = useApi<{ watches: Array<{ postgres: string; table: string }>; subscribers: number; messagesThisMonth: number }>(`${base}/watches`, { poll: 10_000 });
  const { data: project } = useApi<{ resources: Resource[] }>(`/v1/projects/${r.projectId}`);
  const pgs = (project?.resources ?? []).filter((x) => x.kind === "postgres");
  const [channel, setChannel] = useState("public:room1");
  const [payload, setPayload] = useState('{"text": "hello"}');
  const [listening, setListening] = useState<string | null>(null);
  const [messages, setMessages] = useState<Msg[]>([]);
  const [watch, setWatch] = useState({ postgres: "", table: "" });
  const [error, setError] = useState<unknown>(null);
  const es = useRef<EventSource | null>(null);

  useEffect(() => () => es.current?.close(), []);
  useEffect(() => {
    if (!watch.postgres && pgs[0]) setWatch((w) => ({ ...w, postgres: pgs[0].name }));
  }, [pgs, watch.postgres]);

  const listen = (ch: string) => {
    es.current?.close();
    setMessages([]);
    // The owner's session authorizes the stream through the dashboard proxy.
    const s = new EventSource(`/api${base}/subscribe?channel=${encodeURIComponent(ch)}`, { withCredentials: true });
    s.addEventListener("message", (e) => setMessages((m) => [JSON.parse((e as MessageEvent).data) as Msg, ...m].slice(0, 200)));
    es.current = s;
    setListening(ch);
  };

  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <Card>
        <CardHeader title="Try a channel" />
        <div className="space-y-4 p-5">
          <Field label="Channel" hint="Channels starting with public: are open to browsers with the publishable key.">
            <div className="flex gap-2">
              <Input value={channel} onChange={(e) => setChannel(e.target.value)} className="font-mono" />
              <Button variant="secondary" onClick={() => listen(channel)}>
                <Radio className="size-4" /> Listen
              </Button>
            </div>
          </Field>
          <Field label="Payload (JSON)">
            <Textarea rows={3} value={payload} onChange={(e) => setPayload(e.target.value)} />
          </Field>
          <ErrorText error={error} />
          <Button
            onClick={async () => {
              setError(null);
              try {
                await api("POST", `${base}/broadcast`, { channel, event: "message", payload: JSON.parse(payload) });
              } catch (e) {
                setError(e);
              }
            }}
          >
            <Send className="size-4" /> Broadcast
          </Button>
          <div className="grid grid-cols-2 gap-3 pt-2">
            <div className="rounded-xl border border-border p-3">
              <div className="font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase">Subscribers</div>
              <div className="tabular mt-1 font-display text-2xl font-semibold">{data?.subscribers ?? 0}</div>
            </div>
            <div className="rounded-xl border border-border p-3">
              <div className="font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase">Messages · month</div>
              <div className="tabular mt-1 font-display text-2xl font-semibold">{(data?.messagesThisMonth ?? 0).toLocaleString()}</div>
            </div>
          </div>
        </div>
      </Card>

      <Card className="flex flex-col">
        <CardHeader title={listening ? `Listening · ${listening}` : "Messages"} />
        <div className="h-[380px] overflow-y-auto bg-code p-3 font-mono text-[12.5px]">
          {!listening && <p className="p-6 text-center text-muted-foreground">Pick a channel and press Listen.</p>}
          {messages.map((m, i) => (
            <div key={i} className="border-b border-border/60 py-2">
              <span className="text-muted-foreground">{new Date(m.at).toLocaleTimeString([], { hour12: false })}</span> <span className="text-brand">{m.event}</span>{" "}
              <span className="break-all">{JSON.stringify(m.payload)}</span>
            </div>
          ))}
        </div>
      </Card>

      <Card className="lg:col-span-2">
        <CardHeader title="Table changes" />
        <div className="p-5">
          <p className="mb-4 text-sm text-muted-foreground">
            Watch a table to stream its inserts, updates and deletes on the channel <code className="font-mono">db:SERVICE:TABLE</code>.
          </p>
          <div className="flex flex-wrap items-end gap-2">
            <Field label="Postgres service">
              <Select value={watch.postgres} onChange={(e) => setWatch({ ...watch, postgres: e.target.value })} className="w-44">
                {pgs.length === 0 && <option value="">No postgres service</option>}
                {pgs.map((p) => (
                  <option key={p.id} value={p.name}>
                    {p.name}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Table">
              <Input value={watch.table} onChange={(e) => setWatch({ ...watch, table: e.target.value })} className="w-44 font-mono" placeholder="todos" />
            </Field>
            <Button
              disabled={!watch.postgres || !watch.table}
              onClick={async () => {
                setError(null);
                try {
                  await api("POST", `${base}/watches`, watch);
                  reload();
                } catch (e) {
                  setError(e);
                }
              }}
            >
              <Plus className="size-4" /> Watch
            </Button>
          </div>
          <div className="mt-4">
            <Table head={["Channel", ""]}>
              {data?.watches.map((w) => (
                <tr key={w.postgres + w.table}>
                  <Td mono>
                    db:{w.postgres}:{w.table}
                  </Td>
                  <Td className="text-right whitespace-nowrap">
                    <Button size="sm" variant="ghost" onClick={() => listen(`db:${w.postgres}:${w.table}`)}>
                      <Radio className="size-3.5" /> Listen
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      aria-label="Stop watching"
                      onClick={async () => {
                        await api("DELETE", `${base}/watches/${w.postgres}/${w.table}`);
                        reload();
                      }}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </Td>
                </tr>
              ))}
            </Table>
            {data && data.watches.length === 0 && <p className="p-6 text-center text-sm text-muted-foreground">No tables watched yet.</p>}
          </div>
        </div>
      </Card>
    </div>
  );
}
