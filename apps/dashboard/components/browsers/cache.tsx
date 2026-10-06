"use client";

import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { api, useApi } from "@/lib/api";
import { servicePath, type Resource } from "@/lib/kinds";
import { Button, Card, CardHeader, ErrorText, Field, Input, Modal, Textarea, cn } from "../ui";

const encKey = (k: string) => k.split("/").map(encodeURIComponent).join("/");

export function CacheBrowser({ r }: { r: Resource }) {
  const base = servicePath(r);
  const [prefix, setPrefix] = useState("");
  const { data, reload, error } = useApi<{ keys: string[]; truncated: boolean }>(`${base}/keys?limit=500&prefix=${encodeURIComponent(prefix)}`);
  const [selected, setSelected] = useState<string | null>(null);
  const [value, setValue] = useState<{ value: string; ttl: number | null } | null>(null);
  const [adding, setAdding] = useState(false);
  const [form, setForm] = useState({ key: "", value: "", ttl: "" });
  const [actionError, setActionError] = useState<unknown>(null);

  const open = async (key: string) => {
    setSelected(key);
    setValue(null);
    setActionError(null);
    try {
      setValue(await api("GET", `${base}/keys/${encKey(key)}`));
    } catch (e) {
      setActionError(e);
    }
  };

  return (
    <div className="grid gap-6 lg:grid-cols-5">
      <Card className="lg:col-span-2">
        <CardHeader
          title="Keys"
          action={
            <Button size="sm" onClick={() => setAdding(true)}>
              <Plus className="size-3.5" /> Set key
            </Button>
          }
        />
        <div className="p-3">
          <Input placeholder="Filter by prefix" value={prefix} onChange={(e) => setPrefix(e.target.value)} />
        </div>
        <div className="max-h-[480px] overflow-y-auto px-2 pb-2">
          <ErrorText error={error} />
          {data?.keys.length === 0 && <p className="p-3 text-sm text-muted-foreground">No keys.</p>}
          {data?.keys.map((k) => (
            <button key={k} onClick={() => open(k)} className={cn("block w-full truncate rounded-lg px-3 py-1.5 text-left font-mono text-sm hover:bg-accent", selected === k && "bg-accent")}>
              {k}
            </button>
          ))}
          {data?.truncated && <p className="p-3 text-xs text-muted-foreground">Showing the first 500 keys. Narrow with a prefix.</p>}
        </div>
      </Card>
      <Card className="lg:col-span-3">
        <CardHeader
          title={selected ? <span className="font-mono">{selected}</span> : "Value"}
          description={value ? (value.ttl ? `Expires in ${value.ttl} s` : "No expiry") : undefined}
          action={
            selected && (
              <Button
                size="sm"
                variant="danger"
                onClick={async () => {
                  await api("DELETE", `${base}/keys/${encKey(selected)}`);
                  setSelected(null);
                  setValue(null);
                  reload();
                }}
              >
                <Trash2 className="size-3.5" /> Delete
              </Button>
            )
          }
        />
        <div className="p-5">
          <ErrorText error={actionError} />
          {value ? (
            <pre className="max-h-[420px] overflow-auto rounded-lg bg-code p-4 font-mono text-sm whitespace-pre-wrap">
              {(() => {
                try {
                  return JSON.stringify(JSON.parse(value.value), null, 2);
                } catch {
                  return value.value;
                }
              })()}
            </pre>
          ) : (
            <p className="text-sm text-muted-foreground">Select a key to see its value.</p>
          )}
        </div>
      </Card>
      <Modal open={adding} onClose={() => setAdding(false)} title="Set a key">
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setActionError(null);
            try {
              await api("PUT", `${base}/keys/${encKey(form.key)}`, { value: form.value, ttl: form.ttl ? Number(form.ttl) : undefined });
              setAdding(false);
              reload();
              open(form.key);
            } catch (err) {
              setActionError(err);
            }
          }}
        >
          <Field label="Key">
            <Input required value={form.key} onChange={(e) => setForm({ ...form, key: e.target.value })} className="font-mono" autoFocus />
          </Field>
          <Field label="Value">
            <Textarea rows={4} value={form.value} onChange={(e) => setForm({ ...form, value: e.target.value })} />
          </Field>
          <Field label="TTL in seconds (optional)">
            <Input type="number" min={1} value={form.ttl} onChange={(e) => setForm({ ...form, ttl: e.target.value })} />
          </Field>
          <ErrorText error={actionError} />
          <div className="flex justify-end">
            <Button type="submit">Save</Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
